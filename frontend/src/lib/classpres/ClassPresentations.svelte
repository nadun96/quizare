<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// A classroom's group presentations, for its teachers (D-166): tasks, each
	// group's upload, letting a group present, what's on the screen, marks,
	// and deleting uploads afterwards.
	import { api, ApiError } from '$lib/api';
	import TopicSelect from '$lib/classroom/TopicSelect.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import StageCard from './StageCard.svelte';
	import { fromLocalInput, ratingText, statusOf, taskForm, teacherSlide, toLocalInput, type ClassStage, type GroupPres, type PresTask, type TaskInput, type TaskView } from './classpres';

	let { classroomId }: { classroomId: string } = $props();
	let tasks = $state<PresTask[] | null>(null);
	let stage = $state<ClassStage | null>(null);
	let open = $state<string>(''); // the task shown
	let view = $state<TaskView | null>(null);
	let editing = $state<'new' | 'edit' | null>(null);
	let form = $state<TaskInput>(taskForm());
	let due = $state('');
	let busy = $state(false);
	let marks = $state<Record<string, { mark: string; comment: string }>>({});
	let preview = $state<{ id: string; n: number; slides: number; title: string } | null>(null);
	const base = $derived(`/api/teacher/classrooms/${classroomId}`);

	async function loadTasks() {
		const r = await api.get<{ tasks: PresTask[]; stage: ClassStage | null }>(`${base}/presentation-tasks`);
		tasks = r.tasks;
		stage = r.stage;
		if (open && !r.tasks.some((t) => t.id === open)) close();
	}
	async function loadTask(fresh = false) {
		if (!open) return;
		const id = open;
		const v = await api.get<TaskView>(`/api/teacher/presentation-tasks/${id}`);
		if (id !== open) return;
		view = v;
		stage = v.stage;
		for (const g of v.groups) {
			const p = g.presentation;
			if (p && (fresh || !marks[p.id])) marks[p.id] = { mark: p.mark == null ? '' : String(p.mark), comment: p.comment };
		}
	}
	$effect(() => {
		if (classroomId) loadTasks().catch(() => (tasks = []));
	});

	// Keep up with students presenting, uploading and rating.
	let tick = 0;
	$effect(() => {
		const timer = setInterval(refresh, 2000);
		return () => clearInterval(timer);
	});
	async function refresh() {
		if (document.hidden || busy) return;
		tick++;
		try {
			if (stage || tick % 2 === 0) {
				if (open) await loadTask();
				else stage = (await api.get<{ stage: ClassStage | null }>(`${base}/stage`)).stage;
			}
			if (tick % 6 === 0) await loadTasks();
		} catch {
			/* next time */
		}
	}

	async function run(f: () => Promise<unknown>, done?: string) {
		busy = true;
		try {
			await f();
			if (done) toast(done);
			await Promise.all([loadTasks(), loadTask()]);
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields ?? {})[0] ?? e.message) : tr("That didn’t work. Try again."), 'error');
		} finally {
			busy = false;
		}
	}

	function show(id: string) {
		open = id;
		view = null;
		preview = null;
		editing = null;
		loadTask(true).catch(() => {});
	}
	function close() {
		open = '';
		view = null;
		preview = null;
	}
	function startNew() {
		form = taskForm();
		due = '';
		editing = 'new';
	}
	function startEdit() {
		form = taskForm(view?.task);
		due = toLocalInput(view?.task.due_at ?? null);
		editing = 'edit';
	}
	function save(e: SubmitEvent) {
		e.preventDefault();
		const body = { ...form, out_of: Number(form.out_of) || 0, topic_id: form.topic_id || '', due_at: fromLocalInput(due) };
		if (editing === 'new')
			run(async () => {
				const t = await api.post<PresTask>(`${base}/presentation-tasks`, body);
				editing = null;
				show(t.id);
			}, tr("Task added"));
		else
			run(async () => {
				await api.put(`/api/teacher/presentation-tasks/${open}`, body);
				editing = null;
			}, tr("Saved"));
	}
	async function removeTask() {
		if (!view) return;
		const t = view.task;
		if (await confirmDialog({ title: tr("Delete “{0}”?", [t.title]), body: tr("Every group’s presentation, mark and rating for it goes too."), confirm: tr("Delete"), danger: true }))
			run(async () => {
				await api.del(`/api/teacher/presentation-tasks/${t.id}`);
				close();
			}, tr("Deleted"));
	}
	async function removeAllSlides() {
		if (!view) return;
		if (await confirmDialog({ title: tr("Delete every group’s slides?"), body: tr("This frees the space they take. Marks, comments and ratings stay. Groups can upload again, and need your permission again to present."), confirm: tr("Delete slides"), danger: true }))
			run(async () => {
				const r = await api.del<{ removed: number }>(`/api/teacher/presentation-tasks/${view!.task.id}/slides`);
				toast(tr("Slides deleted from {0} presentations", [r?.removed ?? 0]));
			});
	}
	const allow = (p: GroupPres, allowed: boolean) =>
		run(() => api.put(`/api/teacher/group-presentations/${p.id}/allowed`, { allowed }), allowed ? tr("{0} can present now", [p.group]) : tr("Permission taken back"));
	const showIt = (p: GroupPres) => run(() => api.post(`/api/teacher/group-presentations/${p.id}/show`), tr("On the screen"));
	const stop = () => run(() => api.del(`${base}/stage`), tr("Off the screen"));
	const turn = (n: number) => run(() => api.put(`${base}/stage/slide`, { n }));
	function saveMark(p: GroupPres) {
		const m = marks[p.id];
		const text = m.mark.trim().replace(',', '.');
		run(() => api.put(`/api/teacher/group-presentations/${p.id}/mark`, { mark: text === '' ? null : Number(text), comment: m.comment }), tr("Mark saved"));
	}
	async function removeSlides(p: GroupPres) {
		if (await confirmDialog({ title: tr("Delete the slides of “{0}”?", [p.title]), body: tr("The mark, comment and ratings stay."), confirm: tr("Delete slides"), danger: true }))
			run(() => api.del(`/api/teacher/group-presentations/${p.id}/slides`), tr("Slides deleted"));
	}
	async function removePres(p: GroupPres) {
		if (await confirmDialog({ title: tr("Delete “{0}”?", [p.title]), body: tr("Its slides, mark and ratings go too. {0} can upload again.", [p.group]), confirm: tr("Delete"), danger: true }))
			run(() => api.del(`/api/teacher/group-presentations/${p.id}`), tr("Deleted"));
	}
	const when = (iso: string) => new Date(iso).toLocaleString(localeTag(), { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
	const onStage = (p: GroupPres | null) => !!p && stage?.submission_id === p.id;
</script>

<div class="vstack">
	{#if stage}
		{@const st = stage}
		<StageCard stage={st} src={teacherSlide(st.submission_id, st.slide)} canTurn {busy} onturn={turn}>
			{#snippet actions()}
				<span class="small muted grow">{st.presenter_name ? tr("{0} turns the slides; you can too.", [st.presenter_name]) : tr("You turn the slides.")}</span>
				{#if st.presenter_name}<button class="btn btn-sm" disabled={busy} onclick={() => run(() => api.post(`/api/teacher/group-presentations/${st.submission_id}/show`), tr("You have the controls"))}>{tr("Take over")}</button>{/if}
				<button class="btn btn-sm btn-error btn-outline" disabled={busy} onclick={stop}>{tr("Stop presenting")}</button>
			{/snippet}
		</StageCard>
	{/if}

	{#if editing}
		<form class="card card-border bg-base-100 p-4 vstack gap-3" onsubmit={save} aria-label={editing === 'new' ? tr("New presentation task") : tr("Edit the task")}>
			<h2 class="m-0 text-base">{editing === 'new' ? tr("New presentation task") : tr("Edit the task")}</h2>
			<label class="vstack gap-1 small">{tr("Title")}<input class="input input-sm w-full" bind:value={form.title} maxlength="200" required placeholder={tr("e.g. Causes of the war")} /></label>
			<label class="vstack gap-1 small">{tr("Instructions")}<textarea class="textarea textarea-sm w-full" rows="3" maxlength="5000" bind:value={form.instructions} placeholder={tr("e.g. 5 slides, 5 minutes, every member speaks")}></textarea></label>
			<div class="flex flex-wrap items-end gap-3">
				<label class="vstack gap-1 small">{tr("Topic")}<TopicSelect {classroomId} bind:value={() => form.topic_id || null, (v) => (form.topic_id = v ?? '')} compact /></label>
				<label class="vstack gap-1 small">{tr("Uploaded by")}
					<select class="select select-sm w-auto" bind:value={form.upload_by}><option value="any">{tr("Any member")}</option><option value="leader">{tr("The leader or assistant leaders")}</option></select>
				</label>
				<label class="vstack gap-1 small">{tr("Due")}<input class="input input-sm w-auto" type="datetime-local" bind:value={due} /></label>
				<label class="vstack gap-1 small">{tr("Marked out of")}<input class="input input-sm w-24 tabular" type="number" min="0" max="10000" step="any" bind:value={form.out_of} /></label>
				<label class="flex items-center gap-2 small pb-1"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={form.rating} />{tr("Classmates rate each (1–5)")}</label>
			</div>
			<p class="m-0 text-xs muted">{tr("0 marks: not marked. Marks count in the class report under Presentations, the same mark for every member of the group.")}</p>
			<div class="flex gap-2"><button class="btn btn-primary btn-sm" disabled={busy}>{editing === 'new' ? tr("Add task") : tr("Save")}</button><button type="button" class="btn btn-ghost btn-sm" onclick={() => (editing = null)}>{tr("Cancel")}</button></div>
		</form>
	{/if}

	{#if open && view}
		{@const t = view.task}
		<section class="card card-border bg-base-100 p-4 vstack gap-3" aria-labelledby="task-h">
			<div class="flex flex-wrap items-center gap-2">
				<button class="btn btn-ghost btn-sm" onclick={close}><Icon name="chevron-left" size={16} />{tr("All tasks")}</button>
				<h2 id="task-h" class="m-0 text-base grow min-w-0 truncate">{t.title}</h2>
				<button class="btn btn-sm" onclick={startEdit} disabled={busy}><Icon name="pencil" size={14} />{tr("Edit")}</button>
				<button class="btn btn-sm" onclick={removeAllSlides} disabled={busy || !t.submitted}><Icon name="trash" size={14} />{tr("Delete all slides")}</button>
				<button class="btn btn-sm btn-error btn-outline" onclick={removeTask} disabled={busy}>{tr("Delete task")}</button>
			</div>
			<p class="m-0 small muted">
				{t.topic || tr("No topic")} · {t.upload_by === 'leader' ? tr("uploaded by the leader") : tr("uploaded by any member")}{#if t.due_at} · {tr("due {0}", [when(t.due_at)])}{/if} · {t.out_of ? tr("out of {0}", [t.out_of]) : tr("not marked")}{#if t.rating} · {tr("classmates rate")}{/if}
			</p>
			{#if t.instructions}<p class="m-0 small whitespace-pre-line">{t.instructions}</p>{/if}
			{#if !view.groups.length}
				<p class="m-0 small muted">{tr("This classroom has no groups yet. Make them in the Groups tab; each group then uploads its presentation.")}</p>
			{:else}
				<div class="overflow-x-auto">
					<table class="table table-sm">
						<thead><tr><th>{tr("Group")}</th><th>{tr("Presentation")}</th><th>{tr("Status")}</th>{#if t.rating}<th>{tr("Rating")}</th>{/if}{#if t.out_of}<th>{tr("Mark (out of {0})", [t.out_of])}</th>{/if}<th><span class="sr-only">{tr("Actions")}</span></th></tr></thead>
						<tbody>
							{#each view.groups as g (g.id)}
								{@const p = g.presentation}
								{@const s = statusOf(p)}
								<tr class:on={onStage(p)}>
									<td class="whitespace-nowrap"><strong>{g.name}</strong><br /><span class="text-xs muted">{g.members === 1 ? tr("1 member") : tr("{0} members", [g.members])}</span></td>
									<td class="min-w-48">
										{#if p}
											{#if p.ready && !p.files_removed}<button class="link text-left" onclick={() => (preview = { id: p.id, n: 1, slides: p.slides, title: p.title })}>{p.title}</button>{:else}<span>{p.title}</span>{/if}<br />
											<span class="text-xs muted">{p.slides} {p.slides === 1 ? tr("slide") : tr("slides")} · {tr("by {0}", [p.uploaded_by || '—'])} · {when(p.updated_at)}</span>
										{:else}<span class="muted">—</span>{/if}
									</td>
									<td class="whitespace-nowrap">{#if onStage(p)}<span class="badge badge-primary badge-sm">{tr("on screen")}</span>{:else}<span class="badge badge-soft badge-sm {s.tone}">{tr(s.label)}</span>{/if}</td>
									{#if t.rating}<td class="small whitespace-nowrap">{p ? (ratingText(p) || '—') : '—'}</td>{/if}
									{#if t.out_of}
										<td class="whitespace-nowrap">
											{#if p && marks[p.id]}
												<input class="input input-xs w-16 tabular" inputmode="decimal" bind:value={marks[p.id].mark} aria-label={tr("Mark for {0}", [g.name])} />
												<input class="input input-xs w-36" bind:value={marks[p.id].comment} maxlength="1000" placeholder={tr("Comment")} aria-label={tr("Comment for {0}", [g.name])} />
												<button class="btn btn-xs" disabled={busy} onclick={() => saveMark(p)}>{tr("Save")}</button>
											{:else}<span class="muted">—</span>{/if}
										</td>
									{/if}
									<td class="whitespace-nowrap text-right">
										{#if p}
											{#if p.ready && !p.files_removed && !onStage(p)}
												{#if p.allowed}<button class="btn btn-xs" disabled={busy} onclick={() => allow(p, false)} title={tr("They can’t press Present any more")}>{tr("Take back permission")}</button>
												{:else}<button class="btn btn-xs btn-primary" disabled={busy} onclick={() => allow(p, true)} title={tr("Its members get a Present button")}>{tr("Allow to present")}</button>{/if}
												<button class="btn btn-xs" disabled={busy} onclick={() => showIt(p)}>{tr("Show it myself")}</button>
											{/if}
											{#if !p.files_removed && p.ready}<button class="btn btn-ghost btn-xs" disabled={busy} onclick={() => removeSlides(p)} title={tr("Delete the slides, keep the mark")} aria-label={tr("Delete the slides of {0}", [g.name])}><Icon name="eraser" size={13} /></button>{/if}
											<button class="btn btn-ghost btn-xs" disabled={busy} onclick={() => removePres(p)} title={tr("Delete everything")} aria-label={tr("Delete the presentation of {0}", [g.name])}><Icon name="trash" size={13} /></button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		{#if preview}
			{@const pv = preview}
			<section class="card card-border bg-base-100 p-3 vstack gap-2" aria-label={tr("Preview")}>
				<div class="flex items-center gap-2"><strong class="grow truncate">{pv.title}</strong><span class="tabular small">{pv.n} / {pv.slides}</span><button class="btn btn-ghost btn-xs" onclick={() => (preview = null)} aria-label={tr("Close the preview")}><Icon name="x" size={14} /></button></div>
				<img class="slide" src={teacherSlide(pv.id, pv.n)} alt={tr("Slide {0} of {1}", [pv.n, pv.slides])} />
				<div class="flex gap-2">
					<button class="btn btn-sm" disabled={pv.n <= 1} onclick={() => preview && (preview.n -= 1)} aria-label={tr("Back")}><Icon name="chevron-left" size={16} /></button>
					<button class="btn btn-sm" disabled={pv.n >= pv.slides} onclick={() => preview && (preview.n += 1)} aria-label={tr("Next")}><Icon name="chevron-right" size={16} /></button>
				</div>
			</section>
		{/if}
	{:else if !open}
		<div class="flex flex-wrap items-center gap-2">
			<p class="m-0 small muted grow">{tr("Groups upload slides (PDF or PowerPoint) for each task. When you allow a group, its members get a Present button: their slides go on the classroom’s screen and classmates follow on their devices.")}</p>
			{#if !editing}<button class="btn btn-primary btn-sm" onclick={startNew}><Icon name="plus" size={15} />{tr("New task")}</button>{/if}
		</div>
		{#if tasks === null}
			<p class="small muted">{tr("Loading…")}</p>
		{:else if !tasks.length}
			{#if !editing}<p class="m-0 small muted">{tr("No presentation tasks yet.")}</p>{/if}
		{:else}
			<ul class="list card card-border bg-base-100" aria-label={tr("Presentation tasks")}>
				{#each tasks as t (t.id)}
					<li class="item">
						<span class="kind" aria-hidden="true"><Icon name="monitor" size={17} /></span>
						<div class="grow min-w-0">
							<button class="link link-hover font-semibold text-left truncate max-w-full" onclick={() => show(t.id)}>{t.title}</button>
							<p class="m-0 small muted">{t.topic || tr("No topic")}{#if t.due_at} · {tr("due {0}", [when(t.due_at)])}{/if} · {tr("{0} of {1} groups uploaded", [t.submitted, t.groups])}{#if t.out_of} · {tr("{0} marked", [t.marked])}{/if}</p>
						</div>
						{#if stage?.task_id === t.id}<span class="badge badge-error badge-soft badge-sm">{tr("Live")}</span>{/if}
						<button class="btn btn-sm" onclick={() => show(t.id)}>{tr("Open")}</button>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</div>

<style>
	.slide { width: 100%; max-height: 60vh; object-fit: contain; background: #fff; border-radius: 0.5rem; border: 1px solid var(--color-base-300); }
	tr.on { background: color-mix(in oklab, var(--color-primary) 8%, transparent); }
	.list { list-style: none; margin: 0; padding: 0; overflow: hidden; }
	.item { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 0.75rem; padding: 0.7rem 1rem; }
	.item + .item { border-top: 1px solid var(--color-base-200); }
	.item > .grow { flex: 1 1 12rem; }
	.kind { display: grid; place-items: center; flex: none; width: 2rem; height: 2rem; color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 10%, transparent); }
</style>
