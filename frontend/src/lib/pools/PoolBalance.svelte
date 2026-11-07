<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Randomizing a pool (D-132): where the right answers of single-answer
	// questions sit, A, B, C…, per category, and a button that spreads them
	// evenly. Students then can't guess "it's usually A".
	import { api, ApiError } from '../api';
	import type { Question } from '../types';
	import { confirmDialog } from '../ui/dialog.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { letter, spread, spreadLine, type Spread } from './review';

	let { quizId, onchange }: { quizId: string; onchange?: () => void } = $props();
	let qs = $state<Question[] | null>(null);
	let busy = $state('');

	async function load() {
		qs = (await api.get<{ questions: Question[] }>(`/api/teacher/quizzes/${quizId}/questions`)).questions ?? [];
	}
	$effect(() => {
		void quizId;
		load();
	});
	const s = $derived(qs ? spread(qs) : null);
	const multis = (c: string | null) => (qs ?? []).filter((q) => q.type === 'MULTI' && (c === null || (q.category ?? '') === c)).length;

	async function balance(category: string | null) {
		const name = category === null ? 'every category' : `“${category || 'No category'}”`;
		const ok = await confirmDialog({
			title: tr("Spread the right answers in {0}?", [name]),
			body: tr("Each single-answer question’s options are reordered so the right one falls evenly on A, B, C…; the wrong ones are shuffled. Multiple-answer questions are shuffled. Options like “All of the above” and True/False keep their order. Answer keys stay right, and sessions already run keep their questions."),
			confirm: tr("Spread them")
		});
		if (!ok) return;
		busy = category ?? '*';
		try {
			const r = await api.post<{ moved: number; shuffled: number; kept: number }>(`/api/teacher/quizzes/${quizId}/questions/balance`, { category });
			toast(tr("{0} reordered{1}{2}", [r.moved, r.shuffled ? tr(", {0} shuffled", [r.shuffled]) : '', r.kept ? tr("; {0} kept in order", [r.kept]) : '']));
			await load();
			onchange?.();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		} finally {
			busy = '';
		}
	}
	const bar = (sp: Spread, n: number) => (sp.singles ? (n / sp.singles) * 100 : 0);
</script>

{#if !s}
	<Skeleton lines={5} />
{:else}
	<div class="vstack gap-3">
		<div class="card card-border bg-base-100 p-4 sm:p-5 flex flex-wrap items-center gap-3">
			<div class="grow min-w-0">
				<h3 class="m-0 text-base flex items-center gap-2"><Icon name="shuffle" size={18} />{tr("Where the right answers are")}</h3>
				<p class="m-0 small muted">{tr("Files written by hand often put the right answer first. Spread them so students can’t guess by position. To also give each student their own order, set")} <strong>{tr("Answer option order: shuffled")}</strong> {tr("in a quiz’s Settings.")}</p>
			</div>
			<button class="btn btn-primary" disabled={!!busy || !s.all.singles} onclick={() => balance(null)}><Icon name="shuffle" size={16} />{busy === '*' ? tr("Spreading…") : tr("Spread every category")}</button>
		</div>

		{#each s.categories as c (c.category)}
			<section class="card card-border bg-base-100 p-4 sm:p-5 vstack gap-3" aria-label={c.category || tr("No category")}>
				<div class="flex flex-wrap items-center gap-2">
					<strong>{c.category || tr("No category")}</strong>
					<span class="small muted">{tr("{0} single-answer{1}", [c.singles, multis(c.category) ? tr(" · {0} multiple-answer", [multis(c.category)]) : ''])}</span>
					{#if c.singles}<span class="badge badge-soft badge-sm" class:badge-warning={c.skewed} class:badge-success={!c.skewed}>{#if c.skewed}<Icon name="alert" size={12} />{/if}{spreadLine(c)}</span>{/if}
					<span class="grow"></span>
					{#if c.singles || multis(c.category)}<button class="btn btn-sm" class:btn-primary={c.skewed} disabled={!!busy} onclick={() => balance(c.category)}>{busy === c.category ? tr("Spreading…") : tr("Spread")}</button>{/if}
				</div>
				{#if c.singles}
					<div class="bars" role="img" aria-label={tr("Right answers by position: {0}", [c.at.map((n, i) => `${letter(i)} ${n}`).join(', ')])}>
						{#each c.at as n, i (i)}
							<div class="bar">
								<span class="pos">{letter(i)}</span>
								<span class="track"><span class="fill" class:hot={c.skewed && i === c.top} style:width="{bar(c, n)}%"></span></span>
								<span class="small tabular count">{n} <span class="muted">({Math.round(bar(c, n))}%)</span></span>
							</div>
						{/each}
					</div>
				{:else}
					<p class="m-0 small muted">{tr("No single-answer questions here.")}</p>
				{/if}
			</section>
		{/each}
	</div>
{/if}

<style>
	.bars { display: grid; gap: 0.35rem; }
	.bar { display: grid; grid-template-columns: 1.6rem 1fr 6.5rem; align-items: center; gap: 0.6rem; }
	.pos { display: grid; place-items: center; height: 1.5rem; font-weight: 700; font-size: 0.8rem; border: 1px solid var(--color-base-300); }
	.track { height: 0.75rem; background: var(--color-base-200); }
	.fill { display: block; height: 100%; background: var(--color-primary); transition: width 0.3s; }
	.fill.hot { background: var(--color-warning); }
	.count { text-align: right; }
</style>
