<script lang="ts">
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
		const name = await promptDialog({ title: 'Rename classroom', label: 'Name', value: open!.name, maxlength: 200 });
		if (name && name !== open!.name) run('Could not rename', 'Renamed', () => api.patch('/api/admin/classrooms/' + open!.id, { name }));
	}
	function archive() {
		const to = !open!.archived;
		run('Could not change it', to ? 'Archived' : 'Unarchived', () => api.patch('/api/admin/classrooms/' + open!.id, { archived: to }));
	}
	async function move() {
		const email = await promptDialog({
			title: `Move "${open!.name}" to another teacher`,
			body: `Its quizzes, tutorials, presentations and past results go with it; ${open!.teacher_name} no longer sees them. Nothing moves while one of its quiz sessions is open.`,
			label: "The new teacher's email",
			placeholder: 'teacher@school.edu',
			confirm: 'Move'
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
				await confirmDialog({ title: 'A quiz session is still open', body: `${c.teacher_name} ends it first, or you end it from Live activity.`, confirm: 'OK', cancel: 'Close' });
				return;
			}
			const what = contentsList(p);
			let confirm = '';
			if (p.needs_confirm) {
				const typed = await promptDialog({ title: `Delete "${c.name}" and its results?`, body: `This also deletes ${resultsList(p)}${what ? ', and ' + what : ''}. Nothing brings them back; archiving keeps them instead. Type its name below to confirm.`, label: `Type ${c.name} to confirm`, placeholder: c.name, confirm: 'Delete for good', danger: true });
				if (typed === null) return;
				if (typed.trim() !== c.name.trim()) return void toast("The name doesn't match: nothing was deleted", 'error');
				confirm = typed;
			} else if (!(await confirmDialog({ title: `Delete "${c.name}"?`, body: what ? `Everything inside goes too: ${what}.` : undefined, confirm: 'Delete', danger: true }))) return;
			await api.del(`/api/admin/classrooms/${c.id}` + (confirm ? '?confirm=' + encodeURIComponent(confirm) : ''));
			toast('Classroom deleted');
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
		if (await confirmDialog({ title: `Remove ${s.name} from "${open!.name}"?`, body: 'Their results so far stay with the classroom.', confirm: 'Remove', danger: true })) setStudent(s, 'removed');
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
					<h2 id="cls-h" class="m-0 text-lg">{open.name}{#if open.archived}{' '}<span class="badge badge-soft badge-sm">Archived</span>{/if}</h2>
					<p class="m-0 small muted">Taught by {open.teacher_name} ({open.teacher_email}){' · '}join code {open.join_code}{' · '}{open.students} {open.students === 1 ? 'student' : 'students'}{#if open.pending}{', '}{open.pending} waiting{/if}</p>
				</div>
				<button class="btn btn-sm btn-ghost" onclick={() => ((open = null), (students = null))}>Back to all classes</button>
			</div>
			<div class="flex flex-wrap gap-2">
				<button class="btn btn-sm" onclick={rename}>Rename</button>
				<button class="btn btn-sm" onclick={archive}>{open.archived ? 'Unarchive' : 'Archive'}</button>
				<button class="btn btn-sm" onclick={move}>Move to another teacher</button>
				<button class="btn btn-sm btn-error btn-outline" onclick={remove}>Delete</button>
			</div>

			<h3 class="m-0 mt-2 text-base">Students</h3>
			<form class="flex flex-wrap items-end gap-2" onsubmit={add}>
				<div>
					<label for="cls-add" class="small font-semibold">Add a student by email</label>
					<input id="cls-add" class="input input-sm w-72 max-w-full" type="email" placeholder="student@school.edu" bind:value={addEmail} required />
				</div>
				<button class="btn btn-sm">Add</button>
			</form>
			{#if students}
				<ListSearch list={students} placeholder="Search name, email or student ID" label="Search students" />
				<div class="table-wrap">
					<table class="table">
						<thead><tr><th>Student</th><th>Student ID</th><th>Status</th><th><span class="sr-only">Actions</span></th></tr></thead>
						<tbody>
							{#each students.rows as s (s.id)}
								<tr>
									<td>{s.name}<br /><span class="small muted">{s.email}</span></td>
									<td>{s.student_number ?? '—'}</td>
									<td><span class="badge badge-soft {s.status === 'active' ? 'ok' : 'warn'}">{s.status === 'pending' ? 'waiting' : s.status}</span></td>
									<td class="text-right whitespace-nowrap">
										{#if s.status === 'pending'}<button class="btn btn-xs btn-primary" onclick={() => setStudent(s, 'active')}>Approve</button>{/if}
										<button class="btn btn-xs" onclick={() => confirmRemove(s)}>Remove</button>
									</td>
								</tr>
							{:else}
								<tr><td colspan="4" class="muted">{students.loaded ? (students.q ? `No students match “${students.q}”.` : 'No students yet.') : 'Loading…'}</td></tr>
							{/each}
						</tbody>
					</table>
				</div>
				<Pager list={students} label="Students" />
			{/if}
		</section>
	{:else}
		<div class="row">
			<ListSearch {list} placeholder="Search classroom or teacher" label="Search classrooms" />
			<label class="flex items-center gap-2 small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={archived} onchange={() => list.refilter()} />Show archived</label>
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader {list} key="name" label="Classroom" /></th><th><SortHeader {list} key="teacher" label="Teacher" /></th><th><SortHeader {list} key="students" label="Students" /></th><th><SortHeader {list} key="created" label="Created" desc /></th></tr></thead>
				<tbody>
					{#each list.rows as c (c.id)}
						<tr>
							<td><button class="link link-hover text-left font-semibold" onclick={() => choose(c)}>{c.name}</button>{#if c.archived}{' '}<span class="badge badge-soft badge-sm">Archived</span>{/if}</td>
							<td>{c.teacher_name}<br /><span class="small muted">{c.teacher_email}</span></td>
							<td class="tabular">{c.students}{#if c.pending}{' '}<span class="badge badge-soft warn badge-sm">{c.pending} waiting</span>{/if}</td>
							<td class="small tabular">{new Date(c.created_at).toLocaleDateString()}</td>
						</tr>
					{:else}
						<tr><td colspan="4" class="muted">{list.loaded ? (list.q ? `No classrooms match “${list.q}”.` : 'No classrooms yet.') : 'Loading…'}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pager {list} label="Classrooms" />
	{/if}
</div>
