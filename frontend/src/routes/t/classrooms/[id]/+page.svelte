<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import type { Classroom, Module, Overrides, Topic } from '$lib/types';

	type Enrolment = { id: string; student_name: string; student_email: string; student_number: string | null; status: string; created_at: string };
	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let c = $state<Classroom | null>(null);
	let modules = $state<(Module & { topics: Topic[] })[]>([]);
	let enrolments = $state<Enrolment[]>([]);
	let tab = $state<'content' | 'students' | 'settings' | 'join'>('content');
	let newModule = $state('');
	let newTopic = $state<Record<string, string>>({});
	let errors = $state<Record<string, string>>({});
	let notice = $state('');

	async function load() {
		c = await api.get<Classroom>('/api/teacher/classrooms/' + id);
		const mods = (await api.get<{ modules: Module[] }>('/api/teacher/classrooms/' + id + '/modules')).modules ?? [];
		modules = await Promise.all(mods.map(async (m) => ({ ...m, topics: (await api.get<{ topics: Topic[] }>('/api/teacher/modules/' + m.id + '/topics')).topics ?? [] })));
		enrolments = (await api.get<{ enrolments: Enrolment[] }>('/api/teacher/classrooms/' + id + '/enrolments')).enrolments ?? [];
	}
	$effect(() => {
		if (ready() && id) load().catch((e) => (notice = e.message));
	});

	async function addModule(e: SubmitEvent) {
		e.preventDefault();
		await api.post('/api/teacher/classrooms/' + id + '/modules', { name: newModule });
		newModule = '';
		load();
	}
	async function addTopic(moduleId: string, e: SubmitEvent) {
		e.preventDefault();
		await api.post('/api/teacher/modules/' + moduleId + '/topics', { name: newTopic[moduleId] });
		newTopic[moduleId] = '';
		load();
	}
	async function rename(kind: 'modules' | 'topics', itemId: string, current: string) {
		const name = prompt('New name', current);
		if (name && name !== current) {
			await api.patch('/api/teacher/' + kind + '/' + itemId, { name });
			load();
		}
	}
	async function remove(kind: 'modules' | 'topics', itemId: string) {
		if (!confirm('Delete this ' + kind.slice(0, -1) + '?')) return;
		try {
			await api.del('/api/teacher/' + kind + '/' + itemId);
			load();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Delete failed';
		}
	}
	async function setEnrolment(en: Enrolment, status: string) {
		await api.patch('/api/teacher/enrolments/' + en.id, { status });
		load();
	}
	async function editNumber(en: Enrolment) {
		const v = prompt('Student ID', en.student_number ?? '');
		if (v === null) return;
		try {
			await api.patch('/api/teacher/enrolments/' + en.id, { student_number: v });
			load();
		} catch (e) {
			notice = e instanceof ApiError ? (e.fields.student_number ?? e.message) : '';
		}
	}
	async function saveSettings(o: Overrides) {
		errors = {};
		try {
			c = await api.patch<Classroom>('/api/teacher/classrooms/' + id, { settings: o });
		} catch (e) {
			if (e instanceof ApiError) errors = e.fields;
			throw e;
		}
	}
	async function newCode() {
		if (!confirm('Generate a new join code? The old code and QR stop working.')) return;
		await api.post('/api/teacher/classrooms/' + id + '/join-code');
		load();
	}
	async function archive(v: boolean) {
		await api.post('/api/teacher/classrooms/' + id + (v ? '/archive' : '/unarchive'));
		load();
	}
	async function del() {
		if (!confirm('Delete this classroom and everything in it?')) return;
		try {
			await api.del('/api/teacher/classrooms/' + id);
			goto('/t');
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Delete failed';
		}
	}
	const joinUrl = $derived(c ? location.origin + '/c/' + c.join_code : '');
</script>

<div class="container stack">
	{#if c}
		<div class="row"><a href="/t">← Classrooms</a></div>
		<div class="row"><h1 style="margin:0">{c.name}</h1>{#if c.archived}<span class="badge">archived</span>{/if}</div>
		{#if notice}<p class="alert">{notice}</p>{/if}
		<div class="tabs" role="tablist">
			{#each [['content', 'Modules & topics'], ['students', `Students (${enrolments.length})`], ['settings', 'Settings'], ['join', 'Join code']] as [k, l] (k)}
				<button role="tab" aria-selected={tab === k} class:active={tab === k} onclick={() => (tab = k as typeof tab)}>{l}</button>
			{/each}
		</div>

		{#if tab === 'content'}
			{#each modules as m (m.id)}
				<div class="card stack">
					<div class="row"><h3 style="margin:0">{m.name}</h3><span class="spacer"></span>
						<button class="small" onclick={() => rename('modules', m.id, m.name)}>Rename</button>
						<button class="small danger" onclick={() => remove('modules', m.id)}>Delete</button></div>
					{#each m.topics as t (t.id)}
						<div class="row">
							<a href={'/t/topics/' + t.id + '?name=' + encodeURIComponent(t.name)}>{t.name}</a>
							<span class="spacer"></span>
							<button class="small" onclick={() => rename('topics', t.id, t.name)}>Rename</button>
							<button class="small danger" onclick={() => remove('topics', t.id)}>Delete</button>
						</div>
					{/each}
					<form class="row" onsubmit={(e) => addTopic(m.id, e)}>
						<input style="flex:1" placeholder="New topic" bind:value={newTopic[m.id]} required aria-label="New topic name" />
						<button class="small">Add topic</button>
					</form>
				</div>
			{/each}
			<form class="card row" onsubmit={addModule}>
				<input style="flex:1" placeholder="New module (e.g. Term 1)" bind:value={newModule} required aria-label="New module name" />
				<button class="primary">Add module</button>
			</form>
		{:else if tab === 'students'}
			<div class="card table-wrap">
				<table>
					<thead><tr><th>Name</th><th>Student ID</th><th>Status</th><th></th></tr></thead>
					<tbody>
						{#each enrolments as en (en.id)}
							<tr>
								<td>{en.student_name}<br /><span class="small muted">{en.student_email}</span></td>
								<td>{en.student_number ?? '—'} <button class="small" onclick={() => editNumber(en)}>Edit</button></td>
								<td><span class="badge {en.status === 'active' ? 'ok' : en.status === 'pending' ? 'warn' : ''}">{en.status}</span></td>
								<td class="row">
									{#if en.status !== 'active'}<button class="small" onclick={() => setEnrolment(en, 'active')}>Approve</button>{/if}
									{#if en.status === 'pending'}<button class="small" onclick={() => setEnrolment(en, 'rejected')}>Reject</button>{/if}
									{#if en.status === 'active'}<button class="small danger" onclick={() => setEnrolment(en, 'removed')}>Remove</button>{/if}
								</td>
							</tr>
						{:else}<tr><td colspan="4" class="muted">No students yet. Share the join code.</td></tr>{/each}
					</tbody>
				</table>
			</div>
		{:else if tab === 'settings'}
			<div class="card"><SettingsEditor level="classroom" value={c.settings} effective={c.effective} onsave={saveSettings} {errors} /></div>
			<div class="card row">
				<button onclick={() => archive(!c!.archived)}>{c.archived ? 'Unarchive' : 'Archive'} classroom</button>
				<button class="danger" onclick={del}>Delete classroom</button>
			</div>
		{:else}
			<div class="card stack" style="text-align:center">
				<p>Students enrol with this code, link or QR.</p>
				<p style="font-size:2rem;font-weight:700;letter-spacing:0.1em">{c.join_code}</p>
				<QrCode text={joinUrl} size={260} />
				<p class="small"><a href={joinUrl}>{joinUrl}</a></p>
				<div><button onclick={newCode}>Generate a new code</button></div>
			</div>
		{/if}
	{/if}
</div>
