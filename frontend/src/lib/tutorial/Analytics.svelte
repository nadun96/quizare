<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A tutorial's figures for its teacher (D-75, TU-FR-40 to TU-FR-42): per
	// page, who reached it, how long they stayed, where they stopped, how its
	// questions went, and whether reading longer went with better scores.
	import { api } from '../api';
	import RichText from '../richtext/RichText.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { count } from '../words';
	import { duration, type Analytics } from './tutorial';

	let { tutorialId }: { tutorialId: string } = $props();
	let a = $state<Analytics | null>(null);
	$effect(() => {
		api.get<Analytics>(`/api/teacher/tutorials/${tutorialId}/analytics`).then((r) => (a = r));
	});
	const pct = (v: number | undefined | null) => (v == null ? '—' : `${Math.round(v)}%`);
</script>

{#if !a}
	<Skeleton lines={5} />
{:else if !a.started}
	<div class="card card-border bg-base-100 shadow-sm p-6 text-center">
		<p class="m-0 font-semibold">{tr("No figures yet")}</p>
		<p class="small muted m-0">{tr("They appear once students start the tutorial.")}</p>
	</div>
{:else}
	<p class="m-0">{tr("{0} started, {1} finished.", [count(a.started, 'student'), a.finished])}</p>
	{#each a.pages as p (p.index)}
		<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-3" aria-labelledby="ap-{p.index}">
			<div class="flex flex-wrap items-baseline gap-2">
				<h3 class="m-0 text-base" id="ap-{p.index}">{p.index + 1}. {p.title || tr("Untitled page")}</h3>
				<span class="badge badge-soft badge-sm">{p.reading ? tr("Reading") : count(p.questions.length, 'question')}</span>
			</div>
			<dl class="figures">
				<div><dt>{tr("Reached")}</dt><dd class="tabular">{tr("{0} of {1}", [p.reached, a.started])}</dd></div>
				<div><dt>{tr("Median time")}</dt><dd class="tabular">{duration(p.median_seconds)}</dd></div>
				<div><dt>{tr("Stopped here")}</dt><dd class="tabular">{p.stopped_here}</dd></div>
				<div><dt>{tr("Left the app")}</dt><dd class="tabular">{p.leaves ? `${count(p.leaves, 'time')} · ${count(p.left_students, 'student')}` : '—'}</dd></div>
				{#if p.locked}<div><dt>{tr("Locked now")}</dt><dd class="tabular">{p.locked}</dd></div>{/if}
				{#if p.checkpoint === 'correct'}<div><dt>{tr("Out of tries")}</dt><dd class="tabular">{p.out_of_tries}</dd></div>{/if}
			</dl>
			{#if p.then && p.then.longer_n && p.then.shorter_n}
				<p class="finding m-0"><Icon name="info" size={16} />
					<span>{tr("On the next {0}, students who spent at least {1} here scored", [count(p.then.questions, 'question'), duration(p.then.median_seconds)])}
						<strong>{pct(p.then.longer_pct)}</strong> {tr("({0}); those who spent less scored", [p.then.longer_n])} <strong>{pct(p.then.shorter_pct)}</strong> ({p.then.shorter_n}).</span></p>
			{/if}
			{#if p.questions.length}
				<div class="table-wrap">
					<table class="table table-sm">
						<thead><tr><th>{tr("Question")}</th><th class="text-right">{tr("Answered")}</th><th class="text-right">{tr("Right")}</th><th class="text-right">{tr("Average")}</th><th class="text-right">{tr("Tries")}</th></tr></thead>
						<tbody>
							{#each p.questions as q (q.id)}
								<tr>
									<td><span class="small muted">{q.code}</span> <span class="qtext"><RichText text={q.text} /></span></td>
									<td class="text-right tabular">{q.answered}</td>
									<td class="text-right tabular">{q.correct}{#if q.pending}<span class="small muted"> {tr("· {0} to mark", [q.pending])}</span>{/if}</td>
									<td class="text-right tabular">{pct(q.score_pct)}</td>
									<td class="text-right tabular">{q.answered ? q.avg_tries : '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>
	{/each}
{/if}

<style>
	.figures { display: flex; flex-wrap: wrap; gap: 0.5rem 1.75rem; margin: 0; }
	.figures dt { font-size: 0.8rem; color: var(--color-muted); }
	.figures dd { margin: 0; font-weight: 600; }
	.finding { display: flex; gap: 0.5rem; align-items: flex-start; padding: 0.6rem 0.75rem; border-radius: 0.6rem; background: var(--color-base-200); }
	.finding :global(svg) { flex: none; margin-top: 0.2rem; color: var(--color-primary); }
	.qtext { display: inline; }
	.qtext :global(p) { display: inline; margin: 0; }
</style>
