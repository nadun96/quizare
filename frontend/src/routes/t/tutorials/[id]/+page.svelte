<script lang="ts">
	// A tutorial's editor (D-74): its pages (content, questions, checkpoints)
	// in order, its settings, and how its students are doing.
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { describeKey, describeResponse } from '$lib/answerText';
	import { requireRole } from '$lib/guard.svelte';
	import QuestionEditor from '$lib/QuestionEditor.svelte';
	import QuestionView from '$lib/QuestionView.svelte';
	import RichText from '$lib/richtext/RichText.svelte';
	import BlockEditor from '$lib/tutorial/BlockEditor.svelte';
	import Blocks from '$lib/tutorial/Blocks.svelte';
	import { CHECKPOINTS, FEEDBACK, fromLocalInput, move, toLocalInput, type Editor, type Page, type Tutorial } from '$lib/tutorial/tutorial';
	import type { Question, Response } from '$lib/types';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import IconBtn from '$lib/ui/IconBtn.svelte';
	import OptionGroup from '$lib/ui/OptionGroup.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { QTYPE_LABEL } from '$lib/types';
	import { count } from '$lib/words';

	type Progress = { pages: number; questions: number; max_score: number; students: Student[] };
	type Student = { user_id: string; run_id?: string; name: string; email: string; student_number?: string; started: boolean; reached: number; finished: boolean; answered: number; score: number; pending: number; updated_at?: string };
	type Answer = { question: Question; page: number; response?: Response; score: number; max: number; correct: boolean; status?: string; tries: number; comment?: string };
	type RunDetail = { run_id: string; name: string; finished: boolean; answers: Answer[] };

	const ready = requireRole('teacher');
	const id = $derived(appPage.params.id ?? '');
	let ed = $state<Editor | null>(null);
	let tab = $state<'pages' | 'settings' | 'students'>('pages');
	let sel = $state(0);
	let draft = $state<Page | null>(null);
	let dirty = $state(false);
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	let editing = $state<Question | 'new' | null>(null);
	let preview = $state(false);
	let previewAnswers = $state<Record<string, Response>>({});

	async function load(keep = true) {
		ed = await api.get<Editor>('/api/teacher/tutorials/' + id);
		if (!keep || sel >= ed.pages.length) sel = Math.max(0, Math.min(sel, ed.pages.length - 1));
		pick(sel, true);
	}
	$effect(() => {
		if (ready() && id) load(false);
	});

	function pick(i: number, force = false) {
		if (!ed) return;
		sel = i;
		const p = ed.pages[i];
		draft = p ? (JSON.parse(JSON.stringify(p)) as Page) : null;
		dirty = false;
		errors = {};
		editing = null;
		if (force) preview = false;
	}
	async function choose(i: number) {
		if (dirty && !(await confirmDialog({ title: 'Leave this page without saving?', body: 'Your changes to it will be lost.', confirm: 'Leave it' }))) return;
		pick(i);
	}
	$effect(() => {
		void draft?.title, draft?.blocks, draft?.question_ids, draft?.checkpoint, draft?.max_tries;
		if (draft && ed && JSON.stringify(draft) !== JSON.stringify(ed.pages[sel])) dirty = true;
	});

	const byId = $derived(new Map((ed?.questions ?? []).map((q) => [q.id, q])));
	const placed = $derived(new Set((ed?.pages ?? []).flatMap((p, i) => (i === sel ? [] : p.question_ids))));
	const unplaced = $derived((ed?.questions ?? []).filter((q) => !placed.has(q.id) && !draft?.question_ids.includes(q.id)));

	function fail(e: unknown, what: string) {
		if (e instanceof ApiError) {
			errors = e.fields;
			toast(Object.keys(e.fields).length ? 'Check the highlighted parts' : e.message, 'error');
		} else toast(what, 'error');
	}

	async function addPage() {
		if (!ed) return;
		try {
			await api.post('/api/teacher/tutorials/' + id + '/pages', { title: `Page ${ed.pages.length + 1}` });
			await load();
			pick(ed.pages.length - 1, true);
		} catch (e) {
			fail(e, 'Could not add a page');
		}
	}
	async function savePage() {
		if (!draft || !ed) return;
		saving = true;
		errors = {};
		try {
			const { title, blocks, question_ids, checkpoint, max_tries } = draft;
			await api.put('/api/teacher/tutorials/' + id + '/pages/' + draft.id, { title, blocks, question_ids, checkpoint, max_tries });
			await load();
			toast('Page saved');
		} catch (e) {
			fail(e, 'Could not save the page');
		} finally {
			saving = false;
		}
	}
	async function deletePage() {
		if (!draft) return;
		if (!(await confirmDialog({ title: `Delete “${draft.title || 'this page'}”?`, body: 'Its questions stay in the tutorial, ready to put on another page.', confirm: 'Delete page', danger: true }))) return;
		await api.del('/api/teacher/tutorials/' + id + '/pages/' + draft.id);
		await load();
	}
	async function movePage(i: number, d: number) {
		if (!ed) return;
		const ids = move(ed.pages, i, d).map((p) => p.id);
		await api.put('/api/teacher/tutorials/' + id + '/page-order', { ids });
		await load();
		pick(i + d);
	}
	function questionSaved(q: Question) {
		toast(`Question ${q.code} saved`);
		if (draft && !draft.question_ids.includes(q.id)) draft.question_ids = [...draft.question_ids, q.id];
		editing = null;
		api.get<Editor>('/api/teacher/tutorials/' + id).then((e) => ed && (ed.questions = e.questions));
	}

	// ---- settings ----
	let form = $state<{ title: string; description: string; allow_back: boolean; lock_answers: boolean; feedback: Tutorial['feedback']; opens: string; due: string } | null>(null);
	$effect(() => {
		if (ed && !form) {
			const t = ed.tutorial;
			form = { title: t.title, description: t.description, allow_back: t.allow_back, lock_answers: t.lock_answers, feedback: t.feedback, opens: toLocalInput(t.opens_at), due: toLocalInput(t.due_at) };
		}
	});
	async function saveSettings(e: SubmitEvent) {
		e.preventDefault();
		if (!form || !ed) return;
		errors = {};
		try {
			ed.tutorial = await api.put<Tutorial>('/api/teacher/tutorials/' + id, {
				title: form.title, description: form.description, allow_back: form.allow_back, lock_answers: form.lock_answers, feedback: form.feedback,
				clear_dates: true, opens_at: fromLocalInput(form.opens), due_at: fromLocalInput(form.due)
			});
			toast('Settings saved');
		} catch (err) {
			fail(err, 'Could not save');
		}
	}
	async function setStatus(status: 'draft' | 'published') {
		if (!ed) return;
		if (dirty) await savePage();
		try {
			ed.tutorial = await api.post<Tutorial>('/api/teacher/tutorials/' + id + '/status', { status });
			toast(status === 'published' ? 'Published: the classroom’s students can open it' : 'Back to a draft: students can’t open it');
		} catch (e) {
			fail(e, 'Could not change it');
		}
	}
	async function release(released: boolean) {
		if (!ed) return;
		ed.tutorial = await api.post<Tutorial>('/api/teacher/tutorials/' + id + '/release', { released });
		toast(released ? 'Marks and feedback released' : 'Marks and feedback hidden again');
	}
	async function remove() {
		if (!ed) return;
		if (!(await confirmDialog({ title: 'Delete this tutorial?', body: 'Its pages and questions are deleted.', confirm: 'Delete', danger: true }))) return;
		try {
			await api.del('/api/teacher/tutorials/' + id);
			goto('/t/topics/' + ed.tutorial.topic_id);
		} catch (e) {
			fail(e, 'Could not delete');
		}
	}

	// ---- students ----
	let progress = $state<Progress | null>(null);
	let run = $state<RunDetail | null>(null);
	let markForm = $state<Record<string, { score: number; comment: string }>>({});
	async function loadProgress() {
		progress = await api.get<Progress>('/api/teacher/tutorials/' + id + '/progress');
	}
	$effect(() => {
		if (tab === 'students' && ready()) loadProgress();
	});
	async function openRun(s: Student) {
		if (!s.run_id) return;
		run = await api.get<RunDetail>('/api/teacher/tutorials/' + id + '/runs/' + s.run_id);
		markForm = Object.fromEntries(run.answers.map((a) => [a.question.id, { score: a.score, comment: a.comment ?? '' }]));
	}
	async function mark(a: Answer) {
		if (!run) return;
		try {
			run = await api.put<RunDetail>(`/api/teacher/tutorials/${id}/runs/${run.run_id}/answers/${a.question.id}`, { score: Number(markForm[a.question.id].score), comment: markForm[a.question.id].comment });
			toast('Marked');
			loadProgress();
		} catch (e) {
			fail(e, 'Could not save the mark');
		}
	}
	const status = (s: Student) => (s.finished ? 'Finished' : s.started ? `Page ${s.reached + 1} of ${progress?.pages}` : 'Not started');
</script>

<svelte:head><title>{ed?.tutorial.title ?? 'Tutorial'}</title></svelte:head>

<div class="page-container vstack">
	{#if !ed}
		<Skeleton lines={6} />
	{:else}
		{@const t = ed.tutorial}
		<p class="small m-0"><a href={'/t/topics/' + t.topic_id}>← Back to the topic</a></p>
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="m-0">{t.title}</h1>
			<span class="badge badge-soft" class:ok={t.status === 'published'}>{t.status === 'published' ? 'Published' : 'Draft'}</span>
			<span class="spacer"></span>
			{#if t.status === 'published'}<button class="btn" onclick={() => setStatus('draft')}>Unpublish</button>
			{:else}<button class="btn btn-primary" onclick={() => setStatus('published')}><Icon name="check" size={16} />Publish to the class</button>{/if}
		</div>
		<div class="tabs tabs-border" role="tablist">
			{#each [['pages', `Pages (${ed.pages.length})`], ['settings', 'Settings'], ['students', 'Students']] as [k, l] (k)}
				<button class="tab" role="tab" aria-selected={tab === k} class:tab-active={tab === k} onclick={() => (tab = k as typeof tab)}>{l}</button>
			{/each}
		</div>

		{#if tab === 'pages'}
			<div class="layout">
				<nav class="card card-border bg-base-100 shadow-sm p-3 pages" aria-label="Pages">
					<ol>
						{#each ed.pages as p, i (p.id)}
							<li class:on={i === sel}>
								<button class="pick" onclick={() => choose(i)} aria-current={i === sel ? 'page' : undefined}>
									<span class="num">{i + 1}</span>
									<span class="min-w-0"><span class="block truncate font-semibold">{p.title || 'Untitled page'}</span>
										<span class="small muted">{p.blocks.length ? count(p.blocks.length, 'block') : ''}{p.blocks.length && p.question_ids.length ? ' · ' : ''}{p.question_ids.length ? count(p.question_ids.length, 'question') : ''}{p.checkpoint !== 'none' ? ' · checkpoint' : ''}</span></span>
								</button>
								<span class="moves">
									<IconBtn icon="arrow-up" label="Up" hint="Move page {i + 1} up" class="btn-xs btn-ghost" disabled={i === 0} onclick={() => movePage(i, -1)} />
									<IconBtn icon="arrow-down" label="Down" hint="Move page {i + 1} down" class="btn-xs btn-ghost" disabled={i === ed.pages.length - 1} onclick={() => movePage(i, 1)} />
								</span>
							</li>
						{/each}
					</ol>
					<button class="btn btn-sm w-full mt-2" onclick={addPage}><Icon name="plus" size={14} />Add a page</button>
				</nav>

				<section class="vstack min-w-0">
					{#if !draft}
						<div class="card card-border bg-base-100 shadow-sm p-6 text-center vstack">
							<p class="m-0 font-semibold">Start with a page</p>
							<p class="small muted m-0">A page holds reading (text, pictures, videos, documents, links), questions about it, or both. Students go through the pages in order.</p>
							<button class="btn btn-primary mx-auto" onclick={addPage}><Icon name="plus" size={16} />Add the first page</button>
						</div>
					{:else}
						<div class="flex flex-wrap items-center gap-2">
							<input class="input flex-1 font-semibold" style="min-width:12rem" placeholder="Page title" aria-label="Page title" bind:value={draft.title} />
							<button class="btn" aria-pressed={preview} onclick={() => { preview = !preview; previewAnswers = {}; }}><Icon name={preview ? 'pencil' : 'eye'} size={16} />{preview ? 'Edit' : 'Preview'}</button>
							<button class="btn btn-primary" disabled={saving || !dirty} onclick={savePage}>{dirty ? 'Save page' : 'Saved'}</button>
							<IconBtn icon="trash" label="Delete" hint="Delete this page" class="btn-ghost" onclick={deletePage} />
						</div>

						{#if preview}
							<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
								<p class="small muted m-0">As students see this page. Answers here aren’t saved.</p>
								<Blocks blocks={draft.blocks} />
								{#each draft.question_ids as qid (qid)}
									{@const q = byId.get(qid)}
									{#if q}<div class="question"><QuestionView question={q} value={previewAnswers[qid] ?? null} onchange={(r) => (previewAnswers[qid] = r)} /></div>{/if}
								{/each}
							</div>
						{:else}
							<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack">
								<h3 class="m-0 text-base">Content</h3>
								<BlockEditor bind:blocks={draft.blocks} {errors} />
							</div>

							<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack">
								<h3 class="m-0 text-base">Questions on this page</h3>
								{#if errors.question_ids}<p class="field-error m-0">{errors.question_ids}</p>{/if}
								{#each draft.question_ids as qid, i (qid)}
									{@const q = byId.get(qid)}
									{#if q}
										<div class="qrow">
											<span class="badge badge-soft">{QTYPE_LABEL[q.type]}</span>
											<span class="min-w-0 flex-1 truncate"><RichText text={q.text} format={q.body.format} /></span>
											<span class="small muted whitespace-nowrap">{count(q.marks, 'mark')}</span>
											<IconBtn icon="arrow-up" label="Up" hint="Move up" class="btn-xs btn-ghost" disabled={i === 0} onclick={() => draft && (draft.question_ids = move(draft.question_ids, i, -1))} />
											<IconBtn icon="pencil" label="Edit" hint="Edit question {q.code}" class="btn-xs btn-ghost" onclick={() => (editing = q)} />
											<IconBtn icon="x" label="Take off" hint="Take question {q.code} off this page" class="btn-xs btn-ghost" onclick={() => draft && (draft.question_ids = draft.question_ids.filter((x) => x !== qid))} />
										</div>
									{/if}
								{:else}
									<p class="small muted m-0">None yet: this is a reading page.</p>
								{/each}
								{#if editing}
									{#key editing}
										<div class="editor-box"><QuestionEditor quizId={t.quiz_id} question={editing === 'new' ? null : editing} onsaved={questionSaved} oncancel={() => (editing = null)} /></div>
									{/key}
								{:else}
									<div class="flex flex-wrap gap-2">
										<button class="btn btn-sm" onclick={() => (editing = 'new')}><Icon name="plus" size={14} />New question</button>
										{#if unplaced.length}
											<select class="select select-sm w-auto" aria-label="Add a question that isn’t on a page" onchange={(e) => { const v = e.currentTarget.value; if (v && draft) draft.question_ids = [...draft.question_ids, v]; e.currentTarget.value = ''; }}>
												<option value="">Add one not on a page ({unplaced.length})</option>
												{#each unplaced as q (q.id)}<option value={q.id}>{q.code}: {q.text.slice(0, 60)}</option>{/each}
											</select>
										{/if}
									</div>
									<p class="small muted m-0">Questions use the quiz editor: all seven types, marks, feedback and pictures.</p>
								{/if}
							</div>

							{#if draft.question_ids.length}
								<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2">
									<h3 class="m-0 text-base">Before going on</h3>
									<OptionGroup label="Before going on" value={draft.checkpoint} onchange={(v) => draft && (draft.checkpoint = v as Page['checkpoint'])} options={CHECKPOINTS.map((c) => ({
										...c, disabled: c.v === 'correct' && t.feedback !== 'page', why: 'Needs feedback “As they go” (Settings): it shows whether answers are right.'
									}))} />
									{#if draft.checkpoint === 'correct'}
										<label class="small flex items-center gap-2">Tries before they go on anyway
											<input class="input input-sm w-24" type="number" min="0" max="20" bind:value={draft.max_tries} aria-describedby="tries-h" />
										</label>
										<p class="small muted m-0" id="tries-h">0 means as many as they need.</p>
									{/if}
									{#if errors.checkpoint}<p class="field-error m-0">{errors.checkpoint}</p>{/if}
								</div>
							{/if}
						{/if}
					{/if}
				</section>
			</div>
		{:else if tab === 'settings' && form}
			<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={saveSettings}>
				<div><label for="tt">Title</label><input id="tt" class="input w-full" class:input-error={errors.title} bind:value={form.title} maxlength="200" required /></div>
				<div><label for="td">Description <span class="muted">(shown before students start)</span></label><textarea id="td" class="textarea w-full" rows="2" bind:value={form.description}></textarea></div>
				<div class="vstack gap-1">
					<span class="font-semibold">Going back</span>
					<OptionGroup label="Going back" value={String(form.allow_back)} onchange={(v) => form && (form.allow_back = v === 'true')} options={[
						{ v: 'true', label: 'Allowed', icon: 'arrow-left', hint: 'Students can return to earlier pages, for example to re-read before answering.' },
						{ v: 'false', label: 'Forward only', icon: 'arrow-right', hint: 'Once they move on, earlier pages close.' }
					]} />
				</div>
				<div class="vstack gap-1">
					<span class="font-semibold">Marks and feedback</span>
					<OptionGroup label="Marks and feedback" value={form.feedback} onchange={(v) => form && (form.feedback = v as Tutorial['feedback'])} options={FEEDBACK} />
					{#if errors.feedback}<p class="field-error m-0">{errors.feedback}</p>{/if}
				</div>
				{#if form.feedback !== 'page'}
					<div class="vstack gap-1">
						<span class="font-semibold">Changing answers</span>
						<OptionGroup label="Changing answers" value={String(form.lock_answers)} onchange={(v) => form && (form.lock_answers = v === 'true')} options={[
							{ v: 'false', label: 'Until they finish', icon: 'pencil', hint: 'Students can change an answer until they finish the tutorial.' },
							{ v: 'true', label: 'Once checked', icon: 'check', hint: 'An answer is final once its page is checked.' }
						]} />
					</div>
				{/if}
				<div class="flex flex-wrap gap-4">
					<div><label for="to">Opens <span class="muted">(optional)</span></label><input id="to" class="input" type="datetime-local" bind:value={form.opens} /></div>
					<div><label for="tdue">Due <span class="muted">(optional)</span></label><input id="tdue" class="input" class:input-error={errors.due_at} type="datetime-local" bind:value={form.due} />{#if errors.due_at}<p class="field-error m-0">{errors.due_at}</p>{/if}</div>
				</div>
				<p class="small muted m-0">After the due date, students can still read it, but not answer.</p>
				<div class="flex flex-wrap gap-2"><button class="btn btn-primary">Save settings</button><span class="spacer"></span><button type="button" class="btn btn-error btn-outline" onclick={remove}>Delete tutorial</button></div>
			</form>
		{:else if tab === 'students'}
			{#if t.feedback === 'release'}
				<div class="card card-border bg-base-100 shadow-sm p-4 flex flex-wrap items-center gap-3">
					<span class="flex-1">{t.released_at ? 'Marks and feedback are released.' : 'Students don’t see marks or feedback until you release them.'}</span>
					{#if t.released_at}<button class="btn btn-sm" onclick={() => release(false)}>Hide again</button>{:else}<button class="btn btn-sm btn-primary" onclick={() => release(true)}>Release to students</button>{/if}
				</div>
			{/if}
			{#if !progress}<Skeleton lines={4} />
			{:else}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table">
						<thead><tr><th>Student</th><th>Progress</th><th>Score</th><th><span class="sr-only">Actions</span></th></tr></thead>
						<tbody>
							{#each progress.students as s (s.user_id)}
								<tr>
									<td>{s.name}{#if s.student_number}<span class="small muted"> · {s.student_number}</span>{/if}</td>
									<td><span class="badge badge-soft" class:ok={s.finished}>{status(s)}</span></td>
									<td class="tabular">{s.started ? `${s.score} / ${progress.max_score}` : '—'}{#if s.pending}<span class="badge badge-warning badge-sm ml-1">{s.pending} to mark</span>{/if}</td>
									<td>{#if s.run_id}<button class="btn btn-sm" onclick={() => openRun(s)}>{s.pending ? 'Mark' : 'Answers'}</button>{/if}</td>
								</tr>
							{:else}<tr><td colspan="4" class="muted">No students in this classroom yet.</td></tr>{/each}
						</tbody>
					</table>
				</div>
				{#if run}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<div class="flex items-center gap-2"><h3 class="m-0">{run.name}’s answers</h3><span class="spacer"></span><IconBtn icon="x" label="Close" hint="Close the answers" onclick={() => (run = null)} /></div>
						{#each run.answers as a (a.question.id)}
							<div class="answer">
								<p class="small muted m-0">Page {a.page + 1} · {a.question.code} · {count(a.max, 'mark')}</p>
								<RichText text={a.question.text} format={a.question.body.format} />
								<p class="m-0"><strong>Answer:</strong> {a.response ? describeResponse(a.question.type, a.question.body, a.response) || '—' : 'Not answered'}</p>
								{#if a.question.type !== 'ESSAY'}<p class="small muted m-0">Right answer: {describeKey(a.question.type, a.question.body, a.question.key)}</p>{/if}
								{#if a.status}
									<form class="flex flex-wrap items-end gap-2" onsubmit={(e) => { e.preventDefault(); mark(a); }}>
										<label class="small">Score <input class="input input-sm w-20" type="number" step="0.5" min="0" max={a.max} bind:value={markForm[a.question.id].score} /></label>
										<input class="input input-sm flex-1" style="min-width:12rem" placeholder="Comment for the student (optional)" aria-label="Comment" bind:value={markForm[a.question.id].comment} />
										<button class="btn btn-sm" class:btn-primary={a.status === 'pending'}>{a.status === 'pending' ? 'Mark' : 'Change mark'}</button>
										{#if a.status === 'pending'}<span class="badge badge-warning badge-sm">Waiting for your mark</span>{/if}
									</form>
								{/if}
							</div>
						{/each}
					</div>
				{/if}
			{/if}
		{/if}
	{/if}
</div>

<style>
	.layout { display: grid; grid-template-columns: 16rem minmax(0, 1fr); gap: 1rem; align-items: start; }
	@media (max-width: 860px) { .layout { grid-template-columns: minmax(0, 1fr); } }
	.pages ol { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
	.pages li { display: flex; align-items: center; border-radius: 0.5rem; }
	.pages li.on { background: color-mix(in oklab, var(--color-primary) 12%, transparent); }
	.pick { flex: 1; min-width: 0; display: flex; align-items: center; gap: 0.5rem; padding: 0.45rem 0.5rem; text-align: left; background: none; border: 0; cursor: pointer; color: inherit; }
	.num { flex: none; display: grid; place-items: center; width: 1.6rem; height: 1.6rem; border-radius: 999px; background: var(--color-base-200); font-size: 0.8rem; font-weight: 700; }
	.on .num { background: var(--color-primary); color: var(--color-primary-content); }
	.moves { display: flex; flex-direction: column; }
	.qrow { display: flex; align-items: center; gap: 0.5rem; padding: 0.4rem 0; border-top: 1px solid var(--color-base-300); }
	.qrow :global(p) { margin: 0; }
	.editor-box, .question { padding: 0.75rem; border: 1px solid var(--color-base-300); border-radius: 0.75rem; }
	.answer { display: grid; gap: 0.35rem; padding: 0.75rem 0; border-top: 1px solid var(--color-base-300); }
</style>
