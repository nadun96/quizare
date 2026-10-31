<script lang="ts">
	// Announcements (D-103). Teachers post to one of their classrooms (and
	// may email them too), edit, delete, and see who has read each; students
	// read their classrooms' posts, which are marked read as they're shown.
	import { untrack } from 'svelte';
	import { api, ApiError } from '../api';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import { confirmDialog } from '../ui/dialog.svelte';
	import Avatar from '../ui/Avatar.svelte';
	import Icon from '../ui/Icon.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import { toast } from '../ui/toast.svelte';
	import { inbox, when, type Announcement } from './inbox.svelte';

	let { teacher, classroom = '', compose = false }: { teacher: boolean; classroom?: string; compose?: boolean } = $props();

	type Room = { id: string; name: string };
	type Reader = { id: string; name: string; student_number: string | null; read_at: string | null };

	let rooms = $state<Room[]>([]);
	let filter = $state(untrack(() => classroom));
	const list = new Paged<Announcement>(() => '/api/my/announcements', 'announcements', { url: urlState, prefix: 'an', sort: 'created', desc: true, filters: () => ({ classroom: filter || undefined }) });
	// What was new when the student opened the page stays marked for this visit.
	let fresh = $state(new Set<string>());

	list.load();
	// Each page a student sees marks its posts read (they stay marked New for this visit).
	$effect(() => {
		if (teacher) return;
		const rows = list.rows;
		untrack(() => {
			const unread = rows.filter((a) => !a.read && !fresh.has(a.id)).map((a) => a.id);
			if (!unread.length) return;
			fresh = new Set([...fresh, ...unread]);
			api.post('/api/my/announcements/read', { ids: unread }).then(() => inbox.load(true), () => {});
		});
	});
	if (untrack(() => teacher)) api.get<{ classrooms: Room[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => (rooms = r.classrooms ?? []));

	// Writing (and editing) as the teacher.
	let open = $state(untrack(() => compose));
	let editing = $state<Announcement | null>(null);
	let to = $state(untrack(() => classroom));
	let title = $state('');
	let body = $state('');
	let email = $state(true);
	let busy = $state(false);
	let errors = $state<Record<string, string>>({});
	$effect(() => {
		if (!to && rooms.length === 1) to = rooms[0].id;
	});

	function startEdit(a: Announcement) {
		editing = a;
		title = a.title;
		body = a.body;
		to = a.classroom_id;
		open = true;
		errors = {};
		requestAnimationFrame(() => document.getElementById('an-title')?.focus());
	}
	function reset() {
		editing = null;
		title = body = '';
		open = false;
		errors = {};
	}
	async function post(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		errors = {};
		try {
			if (editing) {
				await api.patch('/api/teacher/announcements/' + editing.id, { title, body });
				toast('Announcement saved');
			} else {
				await api.post(`/api/teacher/classrooms/${to}/announcements`, { title, body, email });
				toast(email ? 'Posted and emailed to its students' : 'Posted');
			}
			reset();
			await list.load();
		} catch (err) {
			if (err instanceof ApiError) {
				errors = err.fields;
				toast(Object.values(err.fields)[0] ?? err.message, 'error');
			} else toast('Could not post it', 'error');
		} finally {
			busy = false;
		}
	}
	async function remove(a: Announcement) {
		if (!(await confirmDialog({ title: `Delete "${a.title}"?`, body: `It disappears for the students of ${a.classroom}. Emails already sent stay in their mailboxes.`, confirm: 'Delete', danger: true }))) return;
		try {
			await api.del('/api/teacher/announcements/' + a.id);
			toast('Announcement deleted');
			list.load();
		} catch {
			toast('Could not delete it', 'error');
		}
	}

	// Who has read one.
	let readersOf = $state<string | null>(null);
	let readers = $state<Paged<Reader> | null>(null);
	function showReaders(a: Announcement) {
		if (readersOf === a.id) return void ((readersOf = null), (readers = null));
		readersOf = a.id;
		const r = new Paged<Reader>(() => `/api/teacher/announcements/${a.id}/readers`, 'readers', { sort: 'name' });
		r.load();
		readers = r;
	}
</script>

<div class="vstack">
	{#if teacher}
		{#if open}
			<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={post} aria-labelledby="an-h">
				<h2 id="an-h" class="m-0 text-lg">{editing ? 'Edit the announcement' : 'New announcement'}</h2>
				{#if !editing}
					<div>
						<label for="an-to" class="small font-semibold">Classroom</label>
						<select id="an-to" class="select w-full max-w-md" bind:value={to} required>
							<option value="" disabled>Choose a classroom</option>
							{#each rooms as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
						</select>
					</div>
				{:else}
					<p class="m-0 small muted">In {editing.classroom}. Saving changes it for its students; nothing is emailed again.</p>
				{/if}
				<div>
					<label for="an-title" class="small font-semibold">Title</label>
					<input id="an-title" class="input w-full" class:input-error={errors.title} maxlength="200" placeholder="Test on Friday" bind:value={title} required />
				</div>
				<div>
					<label for="an-body" class="small font-semibold">Announcement</label>
					<textarea id="an-body" class="textarea w-full" class:textarea-error={errors.body} rows="5" maxlength="10000" bind:value={body} required></textarea>
				</div>
				{#if !editing}
					<label class="flex items-center gap-2 small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={email} />Also email it to the students (those who turned these emails off don't get one)</label>
				{/if}
				<div class="flex flex-wrap gap-2">
					<button class="btn btn-primary" disabled={busy || !to || !title.trim() || !body.trim()}><Icon name="send" size={16} />{editing ? 'Save' : 'Post'}</button>
					<button type="button" class="btn btn-ghost" onclick={reset}>Cancel</button>
				</div>
			</form>
		{:else}
			<div><button class="btn btn-primary" onclick={() => (open = true)} disabled={rooms.length === 0}><Icon name="plus" size={16} />New announcement</button></div>
		{/if}
	{/if}

	<div class="row">
		<ListSearch {list} placeholder="Search announcements" label="Search announcements" />
		{#if teacher && rooms.length > 1}
			<select class="select select-sm w-auto" aria-label="Classroom" bind:value={filter} onchange={() => list.refilter()}>
				<option value="">All classrooms</option>
				{#each rooms as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
			</select>
		{/if}
	</div>

	{#each list.rows as a (a.id)}
		<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2" class:unread={fresh.has(a.id)} aria-labelledby="an-{a.id}">
			<div class="flex items-start gap-3">
				<Avatar id={a.author_id ?? a.id} name={a.author || 'Teacher'} avatar={a.avatar} size={36} />
				<div class="grow min-w-0">
					<h3 id="an-{a.id}" class="m-0 text-base">{a.title}{#if fresh.has(a.id)}{' '}<span class="badge badge-primary badge-sm">New</span>{/if}</h3>
					<p class="m-0 small muted">{a.author} · {a.classroom} · <time datetime={a.created_at} title={new Date(a.created_at).toLocaleString()}>{when(a.created_at)}</time>{#if a.updated_at !== a.created_at && new Date(a.updated_at).getTime() - new Date(a.created_at).getTime() > 1000}{' · edited'}{/if}{#if a.emailed}{' · emailed'}{/if}</p>
				</div>
			</div>
			<p class="m-0 whitespace-pre-wrap">{a.body}</p>
			{#if teacher}
				<div class="flex flex-wrap items-center gap-2">
					<button class="btn btn-xs btn-ghost" aria-expanded={readersOf === a.id} onclick={() => showReaders(a)}><Icon name="eye" size={14} />Read by {a.reads} of {a.students}</button>
					<span class="spacer"></span>
					<button class="btn btn-xs" onclick={() => startEdit(a)}>Edit</button>
					<button class="btn btn-xs btn-error btn-outline" onclick={() => remove(a)}>Delete</button>
				</div>
				{#if readersOf === a.id && readers}
					<ul class="readers">
						{#each readers.rows as r (r.id)}
							<li class="flex justify-between gap-2 small"><span>{r.name}{#if r.student_number}<span class="muted"> · {r.student_number}</span>{/if}</span><span class={r.read_at ? 'muted' : 'text-warning'}>{r.read_at ? 'read ' + when(r.read_at) : 'not yet'}</span></li>
						{:else}
							<li class="small muted">{readers.loaded ? 'No students in this classroom yet.' : 'Loading…'}</li>
						{/each}
					</ul>
					<Pager list={readers} label="Readers" />
				{/if}
			{/if}
		</article>
	{:else}
		<div class="card card-border bg-base-100 p-6 text-center muted">
			{#if !list.loaded}Loading…{:else if list.q}No announcements match “{list.q}”.{:else if teacher}Nothing posted yet. Announcements reach every student of the classroom.{:else}No announcements from your teachers yet.{/if}
		</div>
	{/each}
	<Pager {list} label="Announcements" />
</div>

<style>
	.unread { border-left: 4px solid var(--color-primary); }
	.readers { list-style: none; margin: 0; padding: 0.5rem 0.75rem; border-radius: var(--radius-box); background: var(--color-base-200); display: grid; gap: 0.25rem; max-height: 18rem; overflow-y: auto; }
</style>
