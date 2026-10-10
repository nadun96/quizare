<script lang="ts">
	// The teacher's tutoring sessions (TS-FR-01, TS-FR-90, TS-FR-91): create one
	// for a classroom, and every session one page at a time.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import SortHeader from '$lib/ui/SortHeader.svelte';
	import { TutorClient, tutoringConfig } from '$lib/tutoring/client';
	import type { TutoringSession } from '$lib/tutoring/types';

	const ready = requireRole('teacher');
	const client = new TutorClient();
	let enabled = $state<boolean | null>(null);
	let classrooms = $state<{ id: string; name: string }[]>([]);
	let classroom = $state(Paged.fromUrl(urlState, 'classroom_id'));
	let status = $state(Paged.fromUrl(urlState, 'status'));
	const list = new Paged<TutoringSession>(() => '/api/sessions', 'sessions', {
		url: urlState,
		sort: 'created',
		desc: true,
		filters: () => ({ classroom_id: classroom, status }),
		get: (url) => client.get(url)
	});
	let title = $state('');
	let pick = $state('');
	let manual = $state(false);
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);

	onMount(async () => {
		enabled = (await tutoringConfig()).enabled;
	});
	$effect(() => {
		if (!ready() || !enabled) return;
		list.load();
		api.get<{ classrooms: { id: string; name: string }[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => {
			classrooms = r.classrooms;
			pick ||= classroom || r.classrooms[0]?.id || '';
		});
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		busy = true;
		try {
			const s = await client.post<TutoringSession>('/api/sessions', { classroom_id: pick, title, admit_mode: manual ? 'manual' : 'auto' });
			goto('/tutor/' + s.join_code);
		} catch (err) {
			if (err instanceof ApiError) errors = Object.keys(err.fields).length ? err.fields : { _: err.message };
			else throw err;
		} finally {
			busy = false;
		}
	}
	const when = (s?: string) => (s ? new Date(s).toLocaleString() : '');
	const label = { open: 'not started', live: 'live', ended: 'ended' };
</script>

<div class="page-container vstack">
	<h1>Tutoring</h1>
	{#if enabled === false}
		<p class="alert alert-soft alert-info">Tutoring isn't switched on for this platform.</p>
	{:else if enabled}
		<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={create}>
			<h2 class="m-0 text-base">New session</h2>
			<div class="grid gap-3 sm:grid-cols-2">
				<div><label for="ts-title">Title</label><input id="ts-title" class="input w-full" bind:value={title} required maxlength="200" aria-invalid={!!errors.title} />{#if errors.title}<p class="field-error">{errors.title}</p>{/if}</div>
				<div>
					<label for="ts-class">Classroom</label>
					<select id="ts-class" class="select w-full" bind:value={pick} required>
						{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{:else}<option value="">No classrooms yet</option>{/each}
					</select>
				</div>
			</div>
			<label class="row font-normal"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={manual} /> Students wait until I admit them</label>
			<p class="small muted m-0">Students in the classroom join with the session's code or link, on a computer. They can't share anything until you allow it.</p>
			{#if errors._}<p class="alert alert-soft alert-error small m-0">{errors._}</p>{/if}
			<div><button class="btn btn-primary" disabled={busy || !pick}>Create and open</button></div>
		</form>

		<div class="row">
			<ListSearch {list} placeholder="Search titles" label="Search sessions" />
			<select class="select select-sm w-auto" bind:value={classroom} onchange={() => list.refilter()} aria-label="Classroom"><option value="">All classrooms</option>{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{/each}</select>
			<select class="select select-sm w-auto" bind:value={status} onchange={() => list.refilter()} aria-label="Status"><option value="">Any status</option><option value="open">Not started</option><option value="live">Live</option><option value="ended">Ended</option></select>
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader {list} key="title" label="Session" /></th><th>Classroom</th><th>Status</th><th><SortHeader {list} key="created" label="Created" desc /></th><th><span class="sr-only">Open</span></th></tr></thead>
				<tbody>
					{#each list.rows as s (s.id)}
						<tr>
							<td>{s.title}<br /><span class="small muted tabular">{s.join_code}</span></td>
							<td class="small">{s.classroom_name}</td>
							<td><span class="badge badge-soft {s.status === 'live' ? 'danger' : s.status === 'ended' ? '' : 'warn'}">{label[s.status]}</span></td>
							<td class="small tabular">{when(s.created_at)}</td>
							<td>{#if s.status !== 'ended'}<a class="btn btn-sm" href={'/tutor/' + s.join_code}>Open</a>{/if}</td>
						</tr>
					{:else}
						<tr><td colspan="5" class="muted">{list.loaded ? (list.error ? list.error : 'No tutoring sessions yet.') : 'Loading…'}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pager {list} label="Tutoring sessions" />
	{/if}
</div>
