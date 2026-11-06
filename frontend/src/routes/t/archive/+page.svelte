<script lang="ts">
	// A teacher's finished activities in one place (D-149): search and filter
	// them, put them in the archive, bring them back, or delete them from there.
	import { api, ApiError } from '$lib/api';
	import { deleteDialog, fromTutoring, kindInfo, KINDS, type ArchiveItem, type Kind, type TutoringRow, type View } from '$lib/archive/archive';
	import { requireRole } from '$lib/guard.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { TutorClient, tutoringConfig } from '$lib/tutoring/client';
	import type { Classroom } from '$lib/types';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { urlState } from '$lib/urlstate';

	const ready = requireRole('teacher');
	const client = new TutorClient();
	let view = $state<View>(Paged.fromUrl(urlState, 'view') === 'archived' ? 'archived' : 'past');
	let kind = $state<Kind | ''>(Paged.fromUrl(urlState, 'kind') as Kind | '');
	let classroom = $state(Paged.fromUrl(urlState, 'classroom_id'));
	let from = $state(Paged.fromUrl(urlState, 'from'));
	let to = $state(Paged.fromUrl(urlState, 'to'));
	const common = () => ({ view, classroom_id: classroom, from, to });
	const items = new Paged<ArchiveItem>(() => '/api/teacher/archive', 'items', { url: urlState, sort: 'ended', desc: true, filters: () => ({ ...common(), kind: kind === 'tutoring' ? '' : kind }) });
	// Tutoring is a separate service with its own list.
	const sessions = new Paged<TutoringRow>(() => '/api/sessions', 'sessions', { url: null, sort: 'created', desc: true, filters: common, get: (url) => client.get(url) });
	let tutoring = $state(false);
	let classrooms = $state<Classroom[]>([]);
	let text = $state(urlState.read().get('q') ?? '');
	let timer: ReturnType<typeof setTimeout> | undefined;
	let busy = $state('');

	const showItems = $derived(kind !== 'tutoring');
	const showSessions = $derived(tutoring && (kind === '' || kind === 'tutoring'));
	const filtered = $derived(!!(text || kind || classroom || from || to));
	const kinds = $derived(KINDS.filter((k) => k.v !== 'tutoring' || tutoring));
	const nothing = $derived((!showItems || (items.loaded && items.total === 0)) && (!showSessions || (sessions.loaded && sessions.total === 0)));

	function reload() {
		if (showItems) items.refilter();
		if (showSessions) sessions.refilter();
	}
	$effect(() => {
		if (!ready()) return;
		items.load();
		api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => (classrooms = r.classrooms ?? []));
		tutoringConfig().then((c) => {
			tutoring = c.enabled;
			if (c.enabled) {
				sessions.search(text);
				sessions.load();
			}
		});
	});
	function typed() {
		clearTimeout(timer);
		timer = setTimeout(() => {
			items.search(text);
			if (tutoring) sessions.search(text);
		}, 300);
	}
	function clearFilters() {
		text = '';
		kind = '';
		classroom = from = to = '';
		items.search('');
		if (tutoring) sessions.search('');
		reload();
	}
	function setView(v: View) {
		view = v;
		reload();
	}

	const when = (s: string) => new Date(s).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });

	async function run(id: string, done: string, act: () => Promise<unknown>, list: { load(): Promise<void> }) {
		busy = id;
		try {
			await act();
			toast(done);
			await list.load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work. Try again.', 'error');
		} finally {
			busy = '';
		}
	}
	const itemUrl = (it: ArchiveItem) => `/api/teacher/archive/${it.kind}/${it.id}`;
	const archive = (it: ArchiveItem) => run(it.id, 'Moved to the archive', () => api.post(itemUrl(it)), items);
	const restore = (it: ArchiveItem) => run(it.id, 'Brought back', () => api.post(itemUrl(it) + '/restore'), items);
	async function remove(it: ArchiveItem) {
		if (await confirmDialog(deleteDialog(it.kind, it.title))) run(it.id, 'Deleted', () => api.del(itemUrl(it)), items);
	}
	const sessionUrl = (s: TutoringRow) => `/api/sessions/${s.id}`;
	const archiveSession = (s: TutoringRow) => run(s.id, 'Moved to the archive', () => client.post(sessionUrl(s) + '/archive'), sessions);
	const restoreSession = (s: TutoringRow) => run(s.id, 'Brought back', () => client.post(sessionUrl(s) + '/restore'), sessions);
	async function removeSession(s: TutoringRow) {
		if (await confirmDialog(deleteDialog('tutoring', s.title))) run(s.id, 'Deleted', () => client.del(sessionUrl(s)), sessions);
	}
</script>

<svelte:head><title>Past sessions</title></svelte:head>

{#snippet row(k: Kind, title: string, detail: string, room: string, started: string, ended: string, people: number | null, href: string, id: string, onarchive: () => void, onrestore: () => void, onremove: () => void)}
	<tr>
		<td class="whitespace-nowrap"><span class="flex items-center gap-2"><Icon name={kindInfo(k).icon} size={16} />{kindInfo(k).label}</span></td>
		<td class="min-w-48">{#if href}<a {href}>{title}</a>{:else}{title}{/if}{#if detail && detail !== title}<br /><span class="text-xs muted">{detail}</span>{/if}</td>
		<td>{room || '—'}</td>
		<td class="whitespace-nowrap">{when(started)}{#if ended !== started}<br /><span class="text-xs muted">ended {when(ended)}</span>{/if}</td>
		<td class="tabular">{people ?? '—'}</td>
		<td class="whitespace-nowrap text-right">
			{#if view === 'past'}
				<button class="btn btn-sm" disabled={busy === id} onclick={onarchive} aria-label={'Archive ' + title}><Icon name="archive" size={15} />Archive</button>
			{:else}
				<button class="btn btn-sm" disabled={busy === id} onclick={onrestore} aria-label={'Bring back ' + title}><Icon name="undo" size={15} />Bring back</button>
				<button class="btn btn-sm btn-error btn-outline" disabled={busy === id} onclick={onremove} aria-label={'Delete ' + title}><Icon name="trash" size={15} />Delete</button>
			{/if}
		</td>
	</tr>
{/snippet}

{#snippet head()}
	<thead><tr><th>Activity</th><th>Title</th><th>Classroom</th><th>When</th><th>People</th><th><span class="sr-only">Actions</span></th></tr></thead>
{/snippet}

<div class="page-container vstack">
	<h1 class="m-0">Past sessions</h1>
	<p class="muted m-0">Quiz sessions, polls, meetings, live presentations and tutoring sessions that have finished. Archiving takes one out of your everyday lists and keeps its marks and attendance. Delete it from the archive to remove it for good.</p>

	<div class="join" role="group" aria-label="Which sessions">
		<button class="btn btn-sm join-item" class:btn-primary={view === 'past'} aria-pressed={view === 'past'} onclick={() => setView('past')}>Finished</button>
		<button class="btn btn-sm join-item" class:btn-primary={view === 'archived'} aria-pressed={view === 'archived'} onclick={() => setView('archived')}><Icon name="archive" size={15} />Archive</button>
	</div>

	<div class="flex flex-wrap items-end gap-3">
		<label class="vstack gap-1 small grow sm:grow-0">Search
			<input class="input input-sm w-full sm:w-64" type="search" bind:value={text} oninput={typed} placeholder="Title, quiz or classroom" />
		</label>
		<label class="vstack gap-1 small">Activity
			<select class="select select-sm w-auto" bind:value={kind} onchange={reload}>
				<option value="">All activities</option>
				{#each kinds as k (k.v)}<option value={k.v}>{k.label}</option>{/each}
			</select>
		</label>
		<label class="vstack gap-1 small">Classroom
			<select class="select select-sm w-auto max-w-56" bind:value={classroom} onchange={reload}>
				<option value="">All classrooms</option>
				{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
			</select>
		</label>
		<label class="vstack gap-1 small">From<input class="input input-sm w-auto" type="date" bind:value={from} max={to || undefined} onchange={reload} /></label>
		<label class="vstack gap-1 small">To<input class="input input-sm w-auto" type="date" bind:value={to} min={from || undefined} onchange={reload} /></label>
		{#if filtered}<button class="btn btn-ghost btn-sm" onclick={clearFilters}>Clear filters</button>{/if}
	</div>

	{#if !items.loaded}
		<Skeleton lines={4} />
	{:else if nothing}
		<div class="card card-border bg-base-100">
			<EmptyState icon="archive" title={filtered ? 'Nothing matches' : view === 'past' ? 'Nothing finished yet' : 'The archive is empty'}>
				{filtered ? 'Try fewer filters.' : view === 'past' ? 'Sessions appear here when they end.' : 'Archive a finished session and it appears here.'}
			</EmptyState>
		</div>
	{:else}
		{#if showItems && items.rows.length}
			<div class="card card-border bg-base-100 shadow-sm overflow-x-auto">
				<table class="table table-sm">
					{@render head()}
					<tbody>
						{#each items.rows as it (it.kind + it.id)}
							{@render row(it.kind, it.title, it.detail, it.classroom, it.started_at, it.ended_at, it.people, it.href, it.id, () => archive(it), () => restore(it), () => remove(it))}
						{/each}
					</tbody>
				</table>
			</div>
			<Pager list={items} label="Past sessions" />
		{/if}
		{#if showSessions && sessions.rows.length}
			{#if showItems && items.rows.length}<h2 class="m-0 text-base">Tutoring sessions</h2>{/if}
			<div class="card card-border bg-base-100 shadow-sm overflow-x-auto">
				<table class="table table-sm">
					{@render head()}
					<tbody>
						{#each sessions.rows as s (s.id)}
							{@const it = fromTutoring(s)}
							{@render row('tutoring', it.title, it.detail, it.classroom, it.started_at, it.ended_at, it.people, it.href, s.id, () => archiveSession(s), () => restoreSession(s), () => removeSession(s))}
						{/each}
					</tbody>
				</table>
			</div>
			<Pager list={sessions} label="Tutoring sessions" />
		{/if}
	{/if}
</div>
