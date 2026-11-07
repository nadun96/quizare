<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// Every classroom (D-101), for admins and managers given "Classes": who
	// teaches it and who is in it; adding, approving and removing students;
	// renaming, archiving, deleting, and moving it to another teacher.
	// Never quiz content or answers (BA §4).
	import { api, ApiError } from '../api';
	import { contentsList, resultsList, type DeleteContents } from '../deleting';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import { confirmDialog, promptDialog } from '../ui/dialog.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import SortHeader from '../ui/SortHeader.svelte';
	import { toast } from '../ui/toast.svelte';

	type Cls = { id: string; name: string; join_code: string; archived: boolean; created_at: string; teacher_id: string; teacher_name: string; teacher_email: string; students: number; pending: number };
	type Student = { id: string; user_id: string; name: string; email: string; student_number: string | null; status: string };

	let archived = $state(Paged.fromUrl(urlState, 'archived', 'cl') === '1');
	const list = new Paged<Cls>(() => '/api/admin/classrooms', 'classrooms', { url: urlState, prefix: 'cl', sort: 'name', filters: () => ({ archived: archived ? '1' : '' }) });
	list.load();

	let open = $state<Cls | null>(null);
	let students = $state<Paged<Student> | null>(null);
	let addEmail = $state('');

	function choose(c: Cls) {
		open = c;
		const s = new Paged<Student>(() => `/api/admin/classrooms/${c.id}/students`, 'students', { url: urlState, prefix: 'st', sort: 'name' });
		s.load();
		students = s;
	}
	async function refresh() {
		if (open) open = await api.get<Cls>('/api/admin/classrooms/' + open.id);
		await Promise.all([list.load(), students?.load()]);
	}
	function fail(err: unknown, what: string) {
		toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : what, 'error');
	}
	async function run(what: string, done: string, f: () => Promise<unknown>) {
		try {
			await f();
			toast(done);
			await refresh();
		} catch (err) {
			fail(err, what);
		}
	}

	async function rename() {
		const name = await promptDialog({ title: tr("Rename classroom"), label: tr("Name"), value: open!.name, maxlength: 200 });
		if (name && name !== open!.name) run('Could not rename', 'Renamed', () => api.patch('/api/admin/classrooms/' + open!.id, { name }));
	}
	function archive() {
		const to = !open!.archived;
		run('Could not change it', to ? 'Archived' : 'Unarchived', () => api.patch('/api/admin/classrooms/' + open!.id, { archived: to }));
	}
	async function move() {
		const email = await promptDialog({
			title: tr("Move \"{0}\" to another teacher", [open!.name]),
			body: tr("Its quizzes, tutorials, presentations and past results go with it; {0} no longer sees them. Nothing moves while one of its quiz sessions is open.", [open!.teacher_name]),
			label: tr("The new teacher's email"),
			placeholder: 'teacher@school.edu',
			confirm: tr("Move")
		});
		if (!email) return;
		try {
			const r = await api.post<{ classroom: Cls; quizzes: number; bank_quizzes: number }>(`/api/admin/classrooms/${open!.id}/owner`, { teacher_email: email });
			toast(`Moved to ${r.classroom.teacher_name}` + (r.bank_quizzes ? `. ${r.bank_quizzes} of its quizzes draw from a question bank: now ${r.classroom.teacher_name}'s, so they should check them.` : ''));
			await refresh();
		} catch (err) {
			fail(err, 'Could not move it');
		}
	}
	async function remove() {
		const c = open!;
		try {
			const p = await api.get<DeleteContents>(`/api/admin/classrooms/${c.id}/delete-preview`);
			if (p.live_sessions) {
				await confirmDialog({ title: tr("A quiz session is still open"), body: tr("{0} ends it first, or you end it from Live activity.", [c.teacher_name]), confirm: tr("OK"), cancel: tr("Close") });
				return;
			}
			const what = contentsList(p);
			let confirm = '';
			if (p.needs_confirm) {
				const typed = await promptDialog({ title: tr("Delete \"{0}\" and its results?", [c.name]), body: tr("This also deletes {0}{1}. Nothing brings them back; archiving keeps them instead. Type its name below to confirm.", [resultsList(p), what ? tr(", and {0}", [what]) : '']), label: tr("Type {0} to confirm", [c.name]), placeholder: c.name, confirm: tr("Delete for good"), danger: true });
				if (typed === null) return;
				if (typed.trim() !== c.name.trim()) return void toast(tr("The name doesn't match: nothing was deleted"), 'error');
				confirm = typed;
			} else if (!(await confirmDialog({ title: tr("Delete \"{0}\"?", [c.name]), body: what ? tr("Everything inside goes too: {0}.", [what]) : undefined, confirm: tr("Delete"), danger: true }))) return;
			await api.del(`/api/admin/classrooms/${c.id}` + (confirm ? '?confirm=' + encodeURIComponent(confirm) : ''));
			toast(tr("Classroom deleted"));
			open = null;
			students = null;
			await list.load();
		} catch (err) {
			fail(err, 'Could not delete it');
		}
	}
	function setStudent(s: Student, status: 'active' | 'removed') {
		run('Could not change it', status === 'active' ? `${s.name} approved` : `${s.name} removed`, () => api.put(`/api/admin/classrooms/${open!.id}/students/${s.id}`, { status }));
	}
	async function confirmRemove(s: Student) {
		if (await confirmDialog({ title: tr("Remove {0} from \"{1}\"?", [s.name, open!.name]), body: tr("Their results so far stay with the classroom."), confirm: tr("Remove"), danger: true })) setStudent(s, 'removed');
	}
	function add(e: SubmitEvent) {
		e.preventDefault();
		run('Could not add the student', 'Student added', async () => {
			await api.post(`/api/admin/classrooms/${open!.id}/students`, { email: addEmail });
			addEmail = '';
		});
	}
</script>

<div class="vstack">
	{#if open}
		<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="cls-h">
			<div class="flex flex-wrap items-start gap-2">
				<div class="grow min-w-0">
					<h2 id="cls-h" class="m-0 text-lg">{open.name}{#if open.archived}{' '}<span class="badge badge-soft badge-sm">{tr("Archived")}</span>{/if}</h2>
					<p class="m-0 small muted">{tr("Taught by {0} ({1}) · join code {2} · {3} {4}", [open.teacher_name, open.teacher_email, open.join_code, open.students, open.students === 1 ? 'student' : 'students'])}{#if open.pending}{tr(", {0} waiting", [open.pending])}{/if}</p>
				</div>
				<button class="btn btn-sm btn-ghost" onclick={() => ((open = null), (students = null))}>{tr("Back to all classes")}</button>
			</div>
			<div class="flex flex-wrap gap-2">
				<button class="btn btn-sm" onclick={rename}>{tr("Rename")}</button>
				<button class="btn btn-sm" onclick={archive}>{open.archived ? tr("Unarchive") : tr("Archive")}</button>
				<button class="btn btn-sm" onclick={move}>{tr("Move to another teacher")}</button>
				<button class="btn btn-sm btn-error btn-outline" onclick={remove}>{tr("Delete")}</button>
			</div>

			<h3 class="m-0 mt-2 text-base">{tr("Students")}</h3>
			<form class="flex flex-wrap items-end gap-2" onsubmit={add}>
				<div>
					<label for="cls-add" class="small font-semibold">{tr("Add a student by email")}</label>
					<input id="cls-add" class="input input-sm w-72 max-w-full" type="email" placeholder={tr("student@school.edu")} bind:value={addEmail} required />
				</div>
				<button class="btn btn-sm">{tr("Add")}</button>
			</form>
			{#if students}
				<ListSearch list={students} placeholder={tr("Search name, email or student ID")} label={tr("Search students")} />
				<div class="table-wrap">
					<table class="table">
						<thead><tr><th>{tr("Student")}</th><th>{tr("Student ID")}</th><th>{tr("Status")}</th><th><span class="sr-only">{tr("Actions")}</span></th></tr></thead>
						<tbody>
							{#each students.rows as s (s.id)}
								<tr>
									<td>{s.name}<br /><span class="small muted">{s.email}</span></td>
									<td>{s.student_number ?? '—'}</td>
									<td><span class="badge badge-soft {s.status === 'active' ? 'ok' : 'warn'}">{s.status === 'pending' ? 'waiting' : s.status}</span></td>
									<td class="text-right whitespace-nowrap">
										{#if s.status === 'pending'}<button class="btn btn-xs btn-primary" onclick={() => setStudent(s, 'active')}>{tr("Approve")}</button>{/if}
										<button class="btn btn-xs" onclick={() => confirmRemove(s)}>{tr("Remove")}</button>
									</td>
								</tr>
							{:else}
								<tr><td colspan="4" class="muted">{students.loaded ? (students.q ? tr("No students match “{0}”.", [students.q]) : tr("No students yet.")) : tr("Loading…")}</td></tr>
							{/each}
						</tbody>
					</table>
				</div>
				<Pager list={students} label={tr("Students")} />
			{/if}
		</section>
	{:else}
		<div class="row">
			<ListSearch {list} placeholder={tr("Search classroom or teacher")} label={tr("Search classrooms")} />
			<label class="flex items-center gap-2 small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={archived} onchange={() => list.refilter()} />{tr("Show archived")}</label>
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader {list} key="name" label={tr("Classroom")} /></th><th><SortHeader {list} key="teacher" label={tr("Teacher")} /></th><th><SortHeader {list} key="students" label={tr("Students")} /></th><th><SortHeader {list} key="created" label={tr("Created")} desc /></th></tr></thead>
				<tbody>
					{#each list.rows as c (c.id)}
						<tr>
							<td><button class="link link-hover text-left font-semibold" onclick={() => choose(c)}>{c.name}</button>{#if c.archived}{' '}<span class="badge badge-soft badge-sm">{tr("Archived")}</span>{/if}</td>
							<td>{c.teacher_name}<br /><span class="small muted">{c.teacher_email}</span></td>
							<td class="tabular">{c.students}{#if c.pending}{' '}<span class="badge badge-soft warn badge-sm">{tr("{0} waiting", [c.pending])}</span>{/if}</td>
							<td class="small tabular">{new Date(c.created_at).toLocaleDateString(localeTag())}</td>
						</tr>
					{:else}
						<tr><td colspan="4" class="muted">{list.loaded ? (list.q ? tr("No classrooms match “{0}”.", [list.q]) : tr("No classrooms yet.")) : tr("Loading…")}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pager {list} label={tr("Classrooms")} />
	{/if}
</div>
