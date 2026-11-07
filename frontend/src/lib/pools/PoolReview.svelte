<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Reviewing a pool's questions (D-132): one at a time as a student would
	// read it, with the right answer marked, to check it's correct. Marking
	// is optional (unreviewed questions are still asked) and fast from the
	// keyboard: R reviewed and on to the next, U not reviewed, ← → to move,
	// E to edit. Editing a question clears its mark.
	import { api, ApiError } from '../api';
	import { describeKey } from '../answerText';
	import QuestionEditor from '../QuestionEditor.svelte';
	import MathText from '../richtext/MathText.svelte';
	import RichText from '../richtext/RichText.svelte';
	import { QTYPE_LABEL, type Question } from '../types';
	import { confirmDialog } from '../ui/dialog.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { letter, nextTodo, reviewList, type ReviewFilter } from './review';

	let { quizId, onchange }: { quizId: string; onchange?: () => void } = $props();
	let all = $state<Question[] | null>(null);
	let filter = $state<ReviewFilter>('todo');
	let category = $state<string | null>(null);
	let at = $state(0);
	let editing = $state<Question | null>(null);
	let dialog = $state<HTMLDialogElement>();
	let busy = $state(false);

	async function load() {
		all = (await api.get<{ questions: Question[] }>(`/api/teacher/quizzes/${quizId}/questions`)).questions ?? [];
	}
	$effect(() => {
		void quizId;
		load();
	});

	const categories = $derived([...new Set((all ?? []).map((q) => q.category ?? ''))].sort((a, b) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b))));
	const scope = $derived((all ?? []).filter((q) => category === null || (q.category ?? '') === category));
	const done = $derived(scope.filter((q) => q.reviewed_at).length);
	const list = $derived(all ? reviewList(all, filter, category) : []);
	const q = $derived(list[Math.min(at, Math.max(0, list.length - 1))]);
	$effect(() => {
		if (at >= list.length && list.length) at = list.length - 1;
	});

	async function mark(reviewed: boolean, ids = q ? [q.id] : [], advance = true) {
		if (!ids.length || busy) return;
		busy = true;
		try {
			await api.post(`/api/teacher/quizzes/${quizId}/questions/review`, { ids, reviewed });
			const now = new Date().toISOString();
			const set = new Set(ids);
			const current = q?.id;
			all = all!.map((x) => (set.has(x.id) ? { ...x, reviewed_at: reviewed ? (x.reviewed_at ?? now) : null } : x));
			onchange?.();
			if (advance && ids.length === 1) {
				// "Not reviewed" questions shown: the one just marked leaves the list, so stay put; otherwise go to the next to do.
				const fresh = reviewList(all, filter, category);
				const i = fresh.findIndex((x) => x.id === current);
				if (i >= 0) {
					const n = nextTodo(fresh, i);
					at = n >= 0 ? n : Math.min(i + 1, fresh.length - 1);
				}
			}
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t save"), 'error');
		} finally {
			busy = false;
		}
	}
	async function markAllShown() {
		const ids = list.filter((x) => !x.reviewed_at).map((x) => x.id);
		if (!ids.length) return;
		if (!(await confirmDialog({ title: tr("Mark {0} {1} reviewed?", [ids.length, ids.length === 1 ? 'question' : 'questions']), body: tr("Only do this for questions you’ve checked another way, such as in the import preview."), confirm: tr("Mark reviewed") }))) return;
		await mark(true, ids, false);
		toast(tr("{0} marked reviewed", [ids.length]));
	}
	function edit() {
		if (!q) return;
		editing = q;
		queueMicrotask(() => dialog?.showModal());
	}
	function onkey(e: KeyboardEvent) {
		if (editing || !q || e.ctrlKey || e.metaKey || e.altKey) return;
		const t = e.target as HTMLElement;
		if (t.closest('input, textarea, select, [contenteditable]')) return;
		const k = e.key.toLowerCase();
		if (k === 'r') mark(true);
		else if (k === 'u') mark(false, [q.id], false);
		else if (k === 'e') edit();
		else if (e.key === 'ArrowRight' || k === 'j') at = Math.min(at + 1, list.length - 1);
		else if (e.key === 'ArrowLeft' || k === 'k') at = Math.max(at - 1, 0);
		else return;
		e.preventDefault();
	}
	const isRight = (id: string) => !!q?.key?.correct?.includes(id);
	const pct = $derived(scope.length ? Math.round((done / scope.length) * 100) : 0);
</script>

<svelte:window onkeydown={onkey} />

{#if !all}
	<Skeleton lines={6} />
{:else if !all.length}
	<p class="muted m-0">{tr("No questions to review yet. Import some first.")}</p>
{:else}
	<div class="vstack gap-3">
		<div class="card card-border bg-base-100 p-3 sm:p-4 vstack gap-2">
			<div class="flex flex-wrap items-center gap-2">
				<div class="join" role="group" aria-label={tr("Show")}>
					{#each [['todo', 'Not reviewed'], ['done', 'Reviewed'], ['all', 'All']] as [k, l] (k)}
						<button class="btn btn-sm join-item" class:btn-primary={filter === k} aria-pressed={filter === k} onclick={() => ((filter = k as ReviewFilter), (at = 0))}>{l}</button>
					{/each}
				</div>
				{#if categories.length > 1}
					<select class="select select-sm w-auto" aria-label={tr("Category")} value={category ?? '*'} onchange={(e) => ((category = e.currentTarget.value === '*' ? null : e.currentTarget.value), (at = 0))}>
						<option value="*">{tr("Every category")}</option>
						{#each categories as c (c)}<option value={c}>{c || tr("No category")}</option>{/each}
					</select>
				{/if}
				<span class="grow"></span>
				{#if list.some((x) => !x.reviewed_at)}<button class="btn btn-sm btn-ghost" onclick={markAllShown}>{tr("Mark all shown reviewed")}</button>{/if}
			</div>
			<div class="flex items-center gap-3">
				<progress class="progress progress-success grow" value={done} max={scope.length || 1} aria-label={tr("Reviewed")}></progress>
				<span class="small tabular whitespace-nowrap"><strong>{done}</strong> {tr("of {0} reviewed · {1}%", [scope.length, pct])}</span>
			</div>
			<p class="m-0 small muted">{tr("Optional: questions are asked whether reviewed or not. Keys:")} <kbd class="kbd kbd-xs">R</kbd> {tr("reviewed, next ·")} <kbd class="kbd kbd-xs">U</kbd> {tr("not reviewed ·")} <kbd class="kbd kbd-xs">←</kbd> <kbd class="kbd kbd-xs">→</kbd> {tr("move ·")} <kbd class="kbd kbd-xs">E</kbd> {tr("edit")}</p>
		</div>

		{#if !q}
			<div class="card card-border bg-base-100 p-6 text-center vstack gap-2">
				<Icon name="check-circle" size={34} />
				<p class="m-0 font-semibold">{filter === 'todo' ? tr("Everything here is reviewed.") : tr("Nothing to show.")}</p>
				{#if filter === 'todo'}<div><button class="btn btn-sm" onclick={() => (filter = 'all')}>{tr("Show all")}</button></div>{/if}
			</div>
		{:else}
			<article class="qcard card card-border bg-base-100" class:ok={q.reviewed_at}>
				<header class="flex flex-wrap items-center gap-2 px-4 pt-4 sm:px-6">
					<strong class="tabular">{q.code}</strong>
					<span class="badge badge-soft badge-sm">{QTYPE_LABEL[q.type] ?? q.type}</span>
					{#if q.category}<span class="badge badge-soft badge-primary badge-sm">{q.category}</span>{/if}
					<span class="small muted">{q.marks} {q.marks === 1 ? 'mark' : 'marks'}{#if q.settings?.question_time_limit_sec}{' · '}{q.settings.question_time_limit_sec}s{/if}</span>
					<span class="grow"></span>
					{#if q.reviewed_at}<span class="badge badge-success badge-sm gap-1"><Icon name="check" size={12} />{tr("Reviewed")}</span>{:else}<span class="badge badge-ghost badge-sm">{tr("Not reviewed")}</span>{/if}
					<span class="small muted tabular">{at + 1} / {list.length}</span>
				</header>
				<div class="px-4 py-3 sm:px-6 vstack gap-3">
					<div class="qtext"><RichText text={q.text} format={q.body.format} blanks="chip" /></div>
					{#if (q.type === 'SINGLE' || q.type === 'MULTI') && q.body.options?.length}
						<ul class="opts">
							{#each q.body.options as o, i (o.id)}
								<li class:right={isRight(o.id)}>
									<span class="pos">{letter(i)}</span>
									<span class="grow"><MathText text={o.text} /></span>
									{#if isRight(o.id)}<span class="flex items-center gap-1 small font-semibold"><Icon name="check" size={15} />{tr("Right answer")}</span>{/if}
								</li>
							{/each}
						</ul>
					{:else}
						<p class="m-0 answer"><span class="small muted">{tr("Answer:")}</span> <MathText text={describeKey(q.type, q.body, q.key) || '(marked by the teacher or AI)'} /></p>
					{/if}
					{#if q.feedback?.correct || q.feedback?.incorrect}
						<div class="small vstack gap-1">
							{#if q.feedback.correct}<p class="m-0"><span class="muted">{tr("When right:")}</span> {q.feedback.correct}</p>{/if}
							{#if q.feedback.incorrect}<p class="m-0"><span class="muted">{tr("When wrong:")}</span> {q.feedback.incorrect}</p>{/if}
						</div>
					{/if}
				</div>
				<footer class="flex flex-wrap items-center gap-2 px-4 pb-4 sm:px-6">
					<button class="btn btn-sm btn-ghost" disabled={at === 0} onclick={() => (at = Math.max(0, at - 1))} aria-label={tr("Previous question")}><Icon name="arrow-left" size={16} /></button>
					<button class="btn btn-sm btn-ghost" disabled={at >= list.length - 1} onclick={() => (at = Math.min(list.length - 1, at + 1))} aria-label={tr("Next question")}><Icon name="arrow-right" size={16} /></button>
					<button class="btn btn-sm" onclick={edit}><Icon name="pencil" size={14} />{tr("Edit")}</button>
					<span class="grow"></span>
					{#if q.reviewed_at}<button class="btn btn-sm btn-ghost" disabled={busy} onclick={() => mark(false, [q.id], false)}>{tr("Mark not reviewed")}</button>{/if}
					<button class="btn btn-success" disabled={busy} onclick={() => mark(true)}><Icon name="check" size={16} />{q.reviewed_at ? tr("Reviewed · next") : tr("Looks right · next")}</button>
				</footer>
			</article>
		{/if}
	</div>
{/if}

<dialog bind:this={dialog} class="modal" onclose={() => (editing = null)} aria-label={tr("Edit a question")}>
	<div class="modal-box max-w-4xl">
		{#if editing}
			<QuestionEditor {quizId} categories={categories.filter(Boolean)} question={editing} onsaved={(saved) => { toast(`Question ${saved.code} saved; mark it reviewed when you’re happy`); dialog?.close(); editing = null; load(); onchange?.(); }} oncancel={() => { dialog?.close(); editing = null; }} />
		{/if}
	</div>
</dialog>

<style>
	.qcard { border-left: 4px solid var(--color-base-300); }
	.qcard.ok { border-left-color: var(--color-success); }
	.qtext { font-size: 1.05rem; }
	.opts { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.4rem; }
	.opts li { display: flex; align-items: center; gap: 0.75rem; padding: 0.55rem 0.75rem; border: 1px solid var(--color-base-300); }
	.opts li.right { border-color: var(--color-success); background: color-mix(in oklab, var(--color-success) 10%, transparent); color: inherit; }
	.pos { display: grid; place-items: center; flex: none; width: 1.6rem; height: 1.6rem; font-weight: 700; font-size: 0.8rem; border: 1px solid var(--color-base-300); }
	.right .pos { border-color: var(--color-success); background: var(--color-success); color: var(--color-success-content); }
	.answer { padding: 0.55rem 0.75rem; border: 1px solid var(--color-success); background: color-mix(in oklab, var(--color-success) 10%, transparent); }
</style>
