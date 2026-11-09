<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// An offline assignment's marks (D-159): typed in, or through the class
	// list downloaded as Excel or CSV, filled in and uploaded. In a group
	// assignment each group gets one mark, which its members get unless they
	// have their own.
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { changed, markText, progress, readable, sheetUrl, type Draft, type Mode, type OfflineSheet, type Upload } from '$lib/offline/offline';
	import { fromLocalInput, toLocalInput } from '$lib/tutorial/tutorial';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';

	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let sh = $state<OfflineSheet | null>(null);
	let students = $state<Record<string, Draft>>({});
	let groups = $state<Record<string, Draft>>({});
	let busy = $state(false);
	let q = $state('');
	let upload = $state<Upload | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);
	// Editing the assignment itself.
	let editing = $state(false);
	let title = $state('');
	let instructions = $state('');
	let max = $state(0);
	let mode = $state<Mode>('individual');
	let due = $state('');

	function fill(s: OfflineSheet) {
		sh = s;
		students = Object.fromEntries(s.students.map((st) => [st.user_id, { mark: markText(st.own), comment: st.own_comment }]));
		groups = Object.fromEntries(s.groups.map((g) => [g.id, { mark: markText(g.score), comment: g.comment }]));
		title = s.title;
		instructions = s.instructions;
		max = s.max_marks;
		mode = s.mode;
		due = toLocalInput(s.due_at);
	}
	async function load() {
		fill(await api.get<OfflineSheet>(`/api/teacher/offline-assignments/${id}`));
	}
	$effect(() => {
		if (ready() && id) load();
	});

	const own = $derived(sh ? changed(sh.students.map((st) => ({ id: st.user_id, score: st.own, comment: st.own_comment })), students, sh.max_marks) : { changes: [], errors: {} as Record<string, string> });
	const grp = $derived(sh ? changed(sh.groups.map((g) => ({ id: g.id, score: g.score, comment: g.comment })), groups, sh.max_marks) : { changes: [], errors: {} as Record<string, string> });
	const dirty = $derived(own.changes.length + grp.changes.length);
	const invalid = $derived(Object.keys(own.errors).length + Object.keys(grp.errors).length);
	const needle = $derived(q.trim().toLowerCase());
	const shownStudents = $derived((sh?.students ?? []).filter((st) => !needle || st.name.toLowerCase().includes(needle) || (st.student_number ?? '').toLowerCase().includes(needle) || st.groups.some((g) => g.toLowerCase().includes(needle))));
	const isGroup = $derived(sh?.mode === 'group');
	const when = (s: string | null) => (s ? new Date(s).toLocaleString(localeTag(), { dateStyle: 'medium', timeStyle: 'short' }) : '');

	function problem(e: unknown) {
		toast(e instanceof ApiError ? (Object.values(e.fields ?? {})[0] ?? e.message) : tr("That didn’t work. Try again."), 'error');
	}
	async function save() {
		if (!dirty || invalid) return;
		busy = true;
		try {
			fill(await api.put<OfflineSheet>(`/api/teacher/offline-assignments/${id}/marks`, {
				students: own.changes.map((c) => ({ user_id: c.id, score: c.score, comment: c.comment })),
				groups: grp.changes.map((c) => ({ group_id: c.id, score: c.score, comment: c.comment }))
			}));
			toast(tr("Marks saved"));
		} catch (e) {
			problem(e);
		} finally {
			busy = false;
		}
	}
	async function readFile(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;
		if (!readable(file.name)) {
			toast(tr("Upload the .xlsx or .csv file you downloaded"), 'error');
			return;
		}
		if (dirty && !(await confirmDialog({ title: tr("Marks not saved"), body: tr("You have typed marks that aren’t saved. Reading a file keeps them on screen, but save them first if you want them."), confirm: tr("Read the file anyway") }))) return;
		busy = true;
		try {
			const res = await fetch(`/api/teacher/offline-assignments/${id}/sheet`, { method: 'POST', headers: { 'X-Requested-With': 'fetch', 'Content-Type': 'application/octet-stream' }, body: file, credentials: 'same-origin' });
			const data = await res.json();
			if (!res.ok) throw new ApiError(res.status, data?.code ?? 'error', data?.message ?? res.statusText, data?.fields ?? {});
			upload = data as Upload;
		} catch (err) {
			problem(err);
		} finally {
			busy = false;
		}
	}
	async function applyUpload() {
		if (!upload) return;
		busy = true;
		try {
			fill(await api.put<OfflineSheet>(`/api/teacher/offline-assignments/${id}/marks`, {
				students: upload.students.map((m) => ({ user_id: m.user_id, score: m.score, comment: m.comment })),
				groups: upload.groups.map((m) => ({ group_id: m.group_id, score: m.score, comment: m.comment }))
			}));
			toast(tr("{0} marks saved from the file", [upload.students.length + upload.groups.length]));
			upload = null;
		} catch (e) {
			problem(e);
		} finally {
			busy = false;
		}
	}
	async function release(on: boolean) {
		if (!sh) return;
		if (on && sh.marked < sh.students && !(await confirmDialog({ title: tr("Show students their marks?"), body: tr("{0} students have no mark yet. They’ll see that they have none.", [sh.students - sh.marked]), confirm: tr("Show marks") }))) return;
		busy = true;
		try {
			await api.put(`/api/teacher/offline-assignments/${id}/release`, { released: on });
			await load();
			toast(on ? tr("Students can see their marks") : tr("Marks hidden from students"));
		} catch (e) {
			problem(e);
		} finally {
			busy = false;
		}
	}
	async function saveDetails(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		try {
			await api.put(`/api/teacher/offline-assignments/${id}`, { title, instructions, max_marks: Number(max), mode, due_at: fromLocalInput(due) ?? null });
			editing = false;
			await load();
			toast(tr("Saved"));
		} catch (err) {
			problem(err);
		} finally {
			busy = false;
		}
	}
	async function remove() {
		if (!sh) return;
		if (!(await confirmDialog({ title: tr("Delete “{0}”?", [sh.title]), body: tr("Its marks go too, from the gradebook and the class report."), confirm: tr("Delete"), danger: true }))) return;
		try {
			await api.del(`/api/teacher/offline-assignments/${id}`);
			goto('/t/topics/' + sh.topic_id);
		} catch (e) {
			problem(e);
		}
	}
	const diff = (was: number | null, now: number) => (was == null ? String(now) : `${was} → ${now}`);
</script>

<svelte:head><title>{sh?.title ?? tr("Offline assignment")}</title></svelte:head>

<div class="page-container vstack">
	{#if !sh}
		<Skeleton lines={6} />
	{:else}
		<p class="small m-0"><a href={'/t/topics/' + sh.topic_id}>← {sh.topic}</a> · {sh.classroom} · {sh.module}</p>
		<div class="flex flex-wrap items-start gap-2">
			<div class="grow min-w-0">
				<h1 class="m-0 break-words">{sh.title}</h1>
				<p class="m-0 small muted">
					{isGroup ? tr("Marked by group") : tr("Marked student by student")} · {tr("out of {0}", [sh.max_marks])}{#if sh.due_at} · {tr("due {0}", [when(sh.due_at)])}{/if} · {tr(progress(sh))}
				</p>
			</div>
			<button class="btn btn-sm" onclick={() => (editing = !editing)} aria-expanded={editing}><Icon name="pencil" size={15} />{tr("Edit")}</button>
			<button class="btn btn-sm btn-error btn-outline" onclick={remove}><Icon name="trash" size={15} />{tr("Delete")}</button>
		</div>
		{#if sh.instructions && !editing}<p class="m-0 whitespace-pre-line">{sh.instructions}</p>{/if}

		{#if editing}
			<form class="card card-border bg-base-100 p-4 vstack gap-3" onsubmit={saveDetails}>
				<label class="vstack gap-1 small">{tr("Title")}<input class="input w-full" bind:value={title} required maxlength="200" /></label>
				<label class="vstack gap-1 small">{tr("Instructions for students")}<textarea class="textarea w-full" rows="3" bind:value={instructions} maxlength="5000"></textarea></label>
				<div class="flex flex-wrap items-end gap-3">
					<label class="vstack gap-1 small">{tr("Out of")}<input class="input w-24 tabular" type="number" min="0.01" max="10000" step="any" bind:value={max} required /></label>
					<label class="vstack gap-1 small">{tr("Marked")}
						<select class="select w-auto" bind:value={mode}><option value="individual">{tr("Student by student")}</option><option value="group">{tr("By group")}</option></select>
					</label>
					<label class="vstack gap-1 small">{tr("Due (optional)")}<input class="input w-auto" type="datetime-local" bind:value={due} /></label>
				</div>
				<div class="flex gap-2"><button class="btn btn-primary btn-sm" disabled={busy}>{tr("Save")}</button><button type="button" class="btn btn-ghost btn-sm" onclick={() => fill(sh!)}>{tr("Cancel")}</button></div>
			</form>
		{/if}

		<section class="card card-border bg-base-100 p-4 vstack gap-2" aria-label={tr("Mark sheet")}>
			<div class="flex flex-wrap items-center gap-2">
				<h2 class="m-0 text-base grow">{tr("Mark sheet")}</h2>
				<a class="btn btn-sm" href={sheetUrl(sh.id, 'xlsx')} download><Icon name="download" size={15} />{tr("Excel")}</a>
				<a class="btn btn-sm" href={sheetUrl(sh.id, 'csv', 'students')} download><Icon name="download" size={15} />{isGroup ? tr("CSV: students") : tr("CSV")}</a>
				{#if isGroup}<a class="btn btn-sm" href={sheetUrl(sh.id, 'csv', 'groups')} download><Icon name="download" size={15} />{tr("CSV: groups")}</a>{/if}
				<button class="btn btn-sm btn-primary" disabled={busy} onclick={() => fileInput?.click()}><Icon name="upload" size={15} />{tr("Upload marks")}</button>
				<input class="hidden" type="file" accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv" bind:this={fileInput} onchange={readFile} aria-label={tr("Upload marks")} />
			</div>
			<p class="m-0 small muted">{isGroup ? tr("Download the list, type each group’s mark (and any student’s own mark) and upload it. Keep the headings and the Key column; an empty mark changes nothing.") : tr("Download the class list, type the marks and upload it. Keep the headings and the Key column; an empty mark changes nothing.")}</p>
		</section>

		{#if upload}
			<section class="card card-border border-primary bg-base-100 p-4 vstack gap-2" aria-label={tr("From the file")}>
				<h2 class="m-0 text-base">{tr("From the file")}</h2>
				{#if upload.students.length || upload.groups.length}
					<p class="m-0 small">{tr("{0} marks will change; {1} rows are the same as now.", [upload.students.length + upload.groups.length, upload.same])}</p>
					<div class="overflow-x-auto max-h-80">
						<table class="table table-sm">
							<thead><tr><th>{tr("Who")}</th><th>{tr("Mark")}</th><th>{tr("Comment")}</th></tr></thead>
							<tbody>
								{#each upload.groups as m (m.group_id)}<tr><td><Icon name="users" size={14} /> {m.name}</td><td class="tabular">{diff(m.was, m.score)}</td><td class="small">{m.comment}</td></tr>{/each}
								{#each upload.students as m (m.user_id)}<tr><td>{m.name}</td><td class="tabular">{diff(m.was, m.score)}</td><td class="small">{m.comment}</td></tr>{/each}
							</tbody>
						</table>
					</div>
				{:else}
					<p class="m-0 small">{tr("Nothing to change: {0} rows are the same as now.", [upload.same])}</p>
				{/if}
				{#if upload.problems.length}
					<div class="alert alert-soft alert-warning vstack items-start gap-1" role="status">
						<strong>{tr("{0} rows weren’t used", [upload.problems.length])}</strong>
						<ul class="m-0 pl-4 small">{#each upload.problems as p, i (i)}<li>{#if p.row}{p.sheet} {tr("row {0}", [p.row])}: {/if}{p.message}</li>{/each}</ul>
					</div>
				{/if}
				<div class="flex gap-2">
					<button class="btn btn-primary btn-sm" disabled={busy || !(upload.students.length || upload.groups.length)} onclick={applyUpload}>{tr("Save these marks")}</button>
					<button class="btn btn-ghost btn-sm" onclick={() => (upload = null)}>{tr("Cancel")}</button>
				</div>
			</section>
		{/if}

		{#if isGroup}
			<section class="vstack gap-2" aria-label={tr("Group marks")}>
				<h2 class="m-0 text-base">{tr("Group marks")}</h2>
				{#if !sh.groups.length}
					<p class="m-0 small muted">{tr("This classroom has no groups yet.")} <a href={'/t/classrooms/' + sh.classroom_id + '?tab=groups'}>{tr("Make them on the Groups tab")}</a></p>
				{:else}
					<div class="card card-border bg-base-100 overflow-x-auto">
						<table class="table table-sm">
							<thead><tr><th>{tr("Group")}</th><th>{tr("Members")}</th><th>{tr("Mark")}</th><th>{tr("Comment")}</th></tr></thead>
							<tbody>
								{#each sh.groups as g (g.id)}
									<tr>
										<td class="whitespace-nowrap"><span class="dot" style:background="var(--cat-{g.color})" aria-hidden="true"></span> {g.name}</td>
										<td class="small muted min-w-40">{g.members.join(', ') || '—'}</td>
										<td>
											<input class="input input-sm w-20 tabular" class:input-error={!!grp.errors[g.id]} inputmode="decimal" bind:value={groups[g.id].mark} aria-label={tr("Mark for {0}", [g.name])} />
											{#if grp.errors[g.id]}<div class="text-xs text-error">{tr(grp.errors[g.id])}</div>{/if}
										</td>
										<td><input class="input input-sm w-full min-w-40" bind:value={groups[g.id].comment} maxlength="1000" aria-label={tr("Comment for {0}", [g.name])} /></td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</section>
		{/if}

		<section class="vstack gap-2" aria-label={tr("Student marks")}>
			<div class="flex flex-wrap items-center gap-2">
				<h2 class="m-0 text-base grow">{isGroup ? tr("Students") : tr("Marks")}</h2>
				<input class="input input-sm w-full sm:w-56" type="search" bind:value={q} placeholder={tr("Name, ID or group")} aria-label={tr("Search students")} />
			</div>
			{#if isGroup}<p class="m-0 small muted">{tr("Each student gets their group’s mark. Type a mark only to give a student a different one, for example if they didn’t take part.")}</p>{/if}
			<div class="card card-border bg-base-100 overflow-x-auto">
				<table class="table table-sm">
					<thead><tr><th>{tr("No.")}</th><th>{tr("Student")}</th><th>{tr("Student ID")}</th>{#if isGroup}<th>{tr("Group’s mark")}</th>{/if}<th>{isGroup ? tr("Own mark") : tr("Mark")}</th><th>{tr("Comment")}</th>{#if isGroup}<th>{tr("Counts")}</th>{/if}</tr></thead>
					<tbody>
						{#each shownStudents as st, i (st.user_id)}
							<tr>
								<td class="tabular muted">{i + 1}</td>
								<td class="min-w-36">{st.name}{#if st.groups.length && !isGroup}<br /><span class="text-xs muted">{st.groups.join(', ')}</span>{/if}</td>
								<td class="tabular small">{st.student_number ?? ''}</td>
								{#if isGroup}<td class="tabular small">{#if st.group_name}{markText(st.group_score)} <span class="muted">({st.group_name})</span>{:else}<span class="muted">—</span>{/if}</td>{/if}
								<td>
									<input class="input input-sm w-20 tabular" class:input-error={!!own.errors[st.user_id]} inputmode="decimal" bind:value={students[st.user_id].mark} placeholder={isGroup ? markText(st.group_score) : ''} aria-label={tr("Mark for {0}", [st.name])} />
									{#if own.errors[st.user_id]}<div class="text-xs text-error">{tr(own.errors[st.user_id])}</div>{/if}
								</td>
								<td><input class="input input-sm w-full min-w-40" bind:value={students[st.user_id].comment} maxlength="1000" aria-label={tr("Comment for {0}", [st.name])} /></td>
								{#if isGroup}<td class="tabular font-semibold">{markText(st.score) || '—'}</td>{/if}
							</tr>
						{:else}
							<tr><td colspan="7" class="muted">{q ? tr("No student matches.") : tr("No students in this classroom yet.")}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>

		<div class="savebar card card-border bg-base-100 p-3 flex flex-wrap items-center gap-2">
			<span class="small grow">{dirty ? tr("{0} unsaved changes", [dirty]) : tr("All marks saved")}{#if invalid} · <span class="text-error">{tr("{0} marks to fix", [invalid])}</span>{/if}</span>
			{#if sh.released_at}
				<span class="badge badge-success badge-soft">{tr("Students see marks since {0}", [when(sh.released_at)])}</span>
				<button class="btn btn-sm" disabled={busy} onclick={() => release(false)}>{tr("Hide marks")}</button>
			{:else}
				<button class="btn btn-sm" disabled={busy || dirty > 0} onclick={() => release(true)}><Icon name="eye" size={15} />{tr("Show students their marks")}</button>
			{/if}
			<button class="btn btn-primary btn-sm" disabled={busy || !dirty || invalid > 0} onclick={save}>{tr("Save marks")}</button>
		</div>
	{/if}
</div>

<style>
	.dot { display: inline-block; width: 0.7rem; height: 0.7rem; border-radius: 999px; vertical-align: middle; }
	.savebar { position: sticky; bottom: calc(4.75rem + env(safe-area-inset-bottom)); z-index: 5; box-shadow: 0 4px 16px rgb(0 0 0 / 0.08); }
	@media (min-width: 640px) { .savebar { bottom: 0.75rem; } }
</style>
