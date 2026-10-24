<script lang="ts">
	// Questions from the teacher's question bank (D-66), copied into a
	// tutorial page (D-75, TU-FR-04). Copies, so editing them here leaves the
	// bank as it is.
	import { api, ApiError } from '../api';
	import RichText from '../richtext/RichText.svelte';
	import { QTYPE_LABEL, type Question, type Quiz } from '../types';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { count } from '../words';

	let { tutorialId, oncopied, oncancel }: { tutorialId: string; oncopied: (qs: Question[]) => void; oncancel: () => void } = $props();
	let bank = $state<Question[] | null>(null);
	let chosen = $state<string[]>([]);
	let filter = $state('');
	let busy = $state(false);

	$effect(() => {
		api.get<Quiz>('/api/teacher/bank')
			.then((b) => api.get<{ questions: Question[] }>('/api/teacher/quizzes/' + b.id + '/questions'))
			.then((r) => (bank = r.questions), () => (bank = []));
	});
	const shown = $derived(
		(bank ?? []).filter((q) => {
			const f = filter.trim().toLowerCase();
			return !f || q.code.toLowerCase().includes(f) || q.text.toLowerCase().includes(f) || (q.category ?? '').toLowerCase().includes(f);
		})
	);
	function toggle(id: string) {
		chosen = chosen.includes(id) ? chosen.filter((x) => x !== id) : [...chosen, id];
	}
	async function copy() {
		busy = true;
		try {
			const r = await api.post<{ questions: Question[] }>(`/api/teacher/tutorials/${tutorialId}/questions/copy`, { ids: chosen });
			oncopied(r.questions);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not copy the questions', 'error');
		} finally {
			busy = false;
		}
	}
</script>

<div class="picker vstack gap-2">
	<div class="flex flex-wrap items-center gap-2">
		<strong class="flex-1">From your question bank</strong>
		<input class="input input-sm" style="min-width:12rem" placeholder="Search code, text or category" aria-label="Search the question bank" bind:value={filter} />
	</div>
	{#if !bank}
		<p class="small muted m-0">Loading…</p>
	{:else if !bank.length}
		<p class="small muted m-0">Your question bank is empty. <a href="/t/bank">Add questions to it</a> to reuse them in quizzes and tutorials.</p>
	{:else}
		<ul class="list" aria-label="Questions in your bank">
			{#each shown as q (q.id)}
				<li>
					<label class="row">
						<input type="checkbox" class="checkbox checkbox-sm" checked={chosen.includes(q.id)} onchange={() => toggle(q.id)} />
						<span class="badge badge-soft badge-sm">{QTYPE_LABEL[q.type]}</span>
						<span class="min-w-0 flex-1 truncate"><RichText text={q.text} format={q.body.format} /></span>
						<span class="small muted whitespace-nowrap">{q.category ? q.category + ' · ' : ''}{count(q.marks, 'mark')}</span>
					</label>
				</li>
			{:else}<li class="small muted">Nothing matches.</li>{/each}
		</ul>
	{/if}
	<div class="flex flex-wrap items-center gap-2">
		<button type="button" class="btn btn-sm btn-primary" disabled={!chosen.length || busy} onclick={copy}><Icon name="copy" size={14} />{chosen.length ? `Copy ${count(chosen.length, 'question')} to this page` : 'Choose questions'}</button>
		<button type="button" class="btn btn-sm btn-ghost" onclick={oncancel}>Cancel</button>
		<span class="small muted">They’re copied: changing them here leaves the bank as it is.</span>
	</div>
</div>

<style>
	.picker { padding: 0.75rem; border: 1px solid var(--color-base-300); border-radius: 0.75rem; }
	.list { list-style: none; margin: 0; padding: 0; max-height: 18rem; overflow-y: auto; }
	.row { display: flex; align-items: center; gap: 0.5rem; padding: 0.35rem 0.25rem; border-top: 1px solid var(--color-base-300); cursor: pointer; }
	.list li:first-child .row { border-top: 0; }
	.row :global(p) { margin: 0; }
</style>
