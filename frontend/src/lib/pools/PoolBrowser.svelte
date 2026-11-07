<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A pool's questions by category (D-123): each category with its count
	// and marks, opened to its questions; edit or delete one, select many to
	// move to another category or delete; rename a category (into another
	// merges them) or delete it, with its questions or keeping them.
	import { api, ApiError } from '../api';
	import QuestionEditor from '../QuestionEditor.svelte';
	import { QTYPE_LABEL, type Question } from '../types';
	import { confirmDialog, promptDialog } from '../ui/dialog.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { byCategory, plain } from './pools';

	let { quizId, onchange }: { quizId: string; onchange?: () => void } = $props();
	let questions = $state<Question[] | null>(null);
	let find = $state('');
	let selected = $state<Set<string>>(new Set());
	let open = $state<Set<string>>(new Set());
	let editing = $state<Question | 'new' | null>(null);
	let dialog = $state<HTMLDialogElement>();

	async function load() {
		questions = (await api.get<{ questions: Question[] }>(`/api/teacher/quizzes/${quizId}/questions`)).questions ?? [];
		selected = new Set([...selected].filter((id) => questions!.some((q) => q.id === id)));
	}
	$effect(() => {
		void quizId;
		questions = null;
		selected = new Set();
		load();
	});
	function changed() {
		load();
		onchange?.();
	}

	const shown = $derived.by(() => {
		const q = find.trim().toLowerCase();
		const list = (questions ?? []).filter((x) => !q || x.code.toLowerCase().includes(q) || x.text.toLowerCase().includes(q) || (x.category ?? '').toLowerCase().includes(q));
		return byCategory(list);
	});
	const categories = $derived([...new Set((questions ?? []).map((q) => q.category).filter(Boolean))].sort());
	const label = (c: string) => c || 'No category';

	function toggle(set: Set<string>, id: string) {
		const s = new Set(set);
		if (s.has(id)) s.delete(id);
		else s.add(id);
		return s;
	}
	function selectAll(qs: Question[], on: boolean) {
		const s = new Set(selected);
		for (const q of qs) on ? s.add(q.id) : s.delete(q.id);
		selected = s;
	}

	async function bulk(action: 'move' | 'delete') {
		const ids = [...selected];
		let category = '';
		if (action === 'move') {
			const c = await promptDialog({ title: tr("Move {0} {1}", [ids.length, ids.length === 1 ? 'question' : 'questions']), label: tr("To the category"), placeholder: categories[0] ?? tr("Algebra"), allowEmpty: true, maxlength: 60, confirm: tr("Move") });
			if (c === null) return;
			category = c;
		} else if (!(await confirmDialog({ title: tr("Delete {0} {1}?", [ids.length, ids.length === 1 ? 'question' : 'questions']), body: tr("They go from this pool for good. Sessions already run keep their copies."), confirm: tr("Delete"), danger: true }))) return;
		try {
			const r = await api.post<{ affected: number }>(`/api/teacher/quizzes/${quizId}/questions/bulk`, { action, ids, category });
			toast(action === 'move' ? tr("{0} moved to {1}", [r.affected, label(category.trim())]) : tr("{0} deleted", [r.affected]));
			selected = new Set();
			changed();
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : tr("That didn’t work"), 'error');
		}
	}
	async function removeOne(q: Question) {
		if (!(await confirmDialog({ title: tr("Delete question {0}?", [q.code]), body: plain(q.text).slice(0, 160), confirm: tr("Delete"), danger: true }))) return;
		try {
			await api.del(`/api/teacher/questions/${q.id}`);
			toast(tr("{0} deleted", [q.code]));
			changed();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		}
	}
	async function rename(c: string) {
		const to = await promptDialog({ title: tr("Rename “{0}”", [label(c)]), label: tr("New name"), value: c, maxlength: 60, confirm: tr("Rename"), body: tr("Renaming into a category that already exists merges them. Quizzes drawing from the old name need their pools updated.") });
		if (to === null || to.trim() === c) return;
		try {
			const r = await api.post<{ affected: number }>(`/api/teacher/quizzes/${quizId}/categories/rename`, { from: c, to });
			toast(tr("{0} moved to {1}", [r.affected, label(to.trim())]));
			changed();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		}
	}
	async function removeCategory(c: string, n: number) {
		const withQuestions = await confirmDialog({ title: tr("Delete the category “{0}”?", [label(c)]), body: tr("Delete it with its {0} questions? Choose “Keep questions” to leave them without a category instead.", [n]), confirm: tr("Delete {0} questions", [n]), cancel: tr("Keep questions"), danger: true });
		if (!withQuestions && !(await confirmDialog({ title: tr("Keep the {0} questions without a category?", [n]), confirm: tr("Remove the category only") }))) return;
		try {
			const r = await api.post<{ affected: number }>(`/api/teacher/quizzes/${quizId}/categories/delete`, { name: c, delete_questions: withQuestions });
			toast(withQuestions ? tr("{0} questions deleted", [r.affected]) : tr("{0} questions left without a category", [r.affected]));
			changed();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		}
	}
	function edit(q: Question | 'new') {
		editing = q;
		queueMicrotask(() => dialog?.showModal());
	}
	function closeEditor() {
		dialog?.close();
		editing = null;
	}
</script>

{#if !questions}
	<Skeleton lines={5} />
{:else}
	<div class="vstack gap-3">
		<div class="flex flex-wrap items-center gap-2">
			<label class="input input-sm w-full sm:w-72"><Icon name="search" size={14} /><span class="sr-only">{tr("Find questions")}</span><input type="search" placeholder={tr("Find by code, text or category")} bind:value={find} /></label>
			<span class="small muted grow">{tr("{0} {1} in {2} {3}", [questions.length, questions.length === 1 ? 'question' : 'questions', categories.length, categories.length === 1 ? 'category' : 'categories'])}</span>
			<button class="btn btn-sm" onclick={() => edit('new')}><Icon name="plus" size={15} />{tr("New question")}</button>
			<a class="btn btn-sm btn-ghost" href="/t/quizzes/{quizId}">{tr("Full editor")}</a>
		</div>

		{#if selected.size}
			<div class="bulk" role="toolbar" aria-label={tr("With the selected questions")}>
				<strong>{tr("{0} selected", [selected.size])}</strong>
				<button class="btn btn-sm" onclick={() => bulk('move')}><Icon name="arrow-right" size={14} />{tr("Move to category")}</button>
				<button class="btn btn-sm btn-error btn-outline" onclick={() => bulk('delete')}><Icon name="trash" size={14} />{tr("Delete")}</button>
				<button class="btn btn-sm btn-ghost" onclick={() => (selected = new Set())}>{tr("Clear")}</button>
			</div>
		{/if}

		{#each shown as g (g.category)}
			{@const all = g.questions.every((q) => selected.has(q.id))}
			{@const isOpen = open.has(g.category) || !!find.trim()}
			<section class="cat card card-border bg-base-100">
				<div class="cat-head">
					<input type="checkbox" class="checkbox checkbox-sm" checked={all} aria-label={tr("Select every question in {0}", [label(g.category)])} onchange={(e) => selectAll(g.questions, e.currentTarget.checked)} />
					<button class="cat-name" aria-expanded={isOpen} onclick={() => (open = toggle(open, g.category))}>
						<Icon name={isOpen ? 'chevron-down' : 'chevron-right'} size={16} />
						<span class="font-semibold">{label(g.category)}</span>
						<span class="badge badge-soft badge-sm">{g.questions.length}</span>
						<span class="small muted">{tr("{0} marks", [g.questions.reduce((n, q) => n + q.marks, 0)])}</span>
					</button>
					{#if g.category}<button class="btn btn-ghost btn-xs" onclick={() => rename(g.category)}>{tr("Rename")}</button>{/if}
					<button class="btn btn-ghost btn-xs text-error" onclick={() => removeCategory(g.category, g.questions.length)}>{tr("Delete")}</button>
				</div>
				{#if isOpen}
					<ul class="qs">
						{#each g.questions as q (q.id)}
							<li>
								<input type="checkbox" class="checkbox checkbox-sm" checked={selected.has(q.id)} aria-label={tr("Select {0}", [q.code])} onchange={() => (selected = toggle(selected, q.id))} />
								<span class="code tabular">{q.code}</span>
								{#if q.reviewed_at}<span class="text-success flex-none" title={tr("Reviewed")}><Icon name="check-circle" size={14} /><span class="sr-only">{tr("reviewed")}</span></span>{/if}
								<span class="badge badge-ghost badge-sm flex-none">{QTYPE_LABEL[q.type] ?? q.type}</span>
								<span class="text grow">{plain(q.text)}</span>
								<span class="small muted tabular flex-none">{q.marks} {q.marks === 1 ? 'mark' : 'marks'}</span>
								<button class="btn btn-ghost btn-xs" onclick={() => edit(q)}>{tr("Edit")}</button>
								<button class="btn btn-ghost btn-xs" aria-label={tr("Delete {0}", [q.code])} onclick={() => removeOne(q)}><Icon name="trash" size={14} /></button>
							</li>
						{/each}
					</ul>
				{/if}
			</section>
		{:else}
			<p class="muted m-0">{find ? tr("No questions match “{0}”.", [find]) : tr("No questions yet. Import CSV files above, or add one.")}</p>
		{/each}
	</div>
{/if}

<dialog bind:this={dialog} class="modal" onclose={() => (editing = null)} aria-label={tr("Edit a question")}>
	<div class="modal-box max-w-4xl">
		{#if editing}
			<QuestionEditor {quizId} {categories} question={editing === 'new' ? null : editing} onsaved={(saved) => { toast(`Question ${saved.code} saved`); closeEditor(); changed(); }} oncancel={closeEditor} />
		{/if}
	</div>
</dialog>

<style>
	.bulk { position: sticky; top: 4.5rem; z-index: 5; display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; padding: 0.5rem 0.75rem; background: var(--color-base-100); border: 1px solid var(--color-primary); box-shadow: 0 6px 18px rgb(0 0 0 / 0.08); }
	.cat-head { display: flex; align-items: center; gap: 0.5rem; padding: 0.6rem 0.75rem; }
	.cat-name { display: flex; align-items: center; gap: 0.5rem; flex: 1; min-width: 0; text-align: left; }
	.qs { list-style: none; margin: 0; padding: 0 0.75rem 0.5rem; }
	.qs li { display: flex; align-items: center; gap: 0.5rem; padding: 0.35rem 0; border-top: 1px solid var(--color-base-200); min-width: 0; }
	.code { flex: none; min-width: 3.5rem; font-weight: 600; font-size: 0.85rem; }
	.text { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.9rem; }
	@media (max-width: 640px) {
		.qs li { flex-wrap: wrap; }
		.text { white-space: normal; flex-basis: 100%; order: 5; }
	}
</style>
