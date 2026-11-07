<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A student's own months across their classrooms (D-119): their average
	// beside their classes', and what they did each month.
	import { api } from '../api';
	import StatTile from '../dashboard/StatTile.svelte';
	import TrendChart from '../dashboard/TrendChart.svelte';
	import { fromFirst, monthLabel, points, SPANS, summary, type StudentTrends } from './trends';

	let months = $state(12);
	let data = $state<StudentTrends | null>(null);
	$effect(() => {
		const m = months;
		api.get<StudentTrends>(`/api/my/trends?months=${m}`).then((d) => (data = d), () => (data = null));
	});
	const shown = $derived(data ? fromFirst(data.months) : []);
	const sum = $derived(summary(shown));
	const k = (v: number | null) => (v === null ? undefined : { value: v, prev: null });
	const pts = $derived(points(shown, (m) => m.avg, (m) => m.class_avg, (m) => `${m.quizzes} results`));
</script>

{#if data && sum.results + sum.tutorials > 0}
	<section class="vstack" aria-labelledby="my-trends-h">
		<div class="flex flex-wrap items-end gap-3">
			<h2 id="my-trends-h" class="m-0 grow">{tr("Over time")}</h2>
			<div class="join" role="group" aria-label={tr("Span")}>
				{#each SPANS as s (s.months)}<button class="btn btn-sm join-item" class:btn-active={months === s.months} aria-pressed={months === s.months} onclick={() => (months = s.months)}>{tr(s.label)}</button>{/each}
			</div>
		</div>
		<div class="tiles">
			<StatTile label={tr("Average score")} kpi={k(sum.avg)} unit="%" versus="" />
			<StatTile label={tr("Quizzes")} kpi={k(sum.results)} versus="" />
			<StatTile label={tr("Tutorials finished")} kpi={k(sum.tutorials)} versus="" />
			<StatTile label={tr("Active months")} kpi={k(sum.activeMonths)} versus="" />
		</div>
		{#if pts.length}
			<div class="card card-border bg-base-100 p-4">
				<TrendChart title={tr("Your average each month")} name="You" contextName="Your classes" rowName="Month" noun="months" points={pts} />
			</div>
		{/if}
		<details class="card card-border bg-base-100 p-4">
			<summary class="font-semibold">{tr("Every month")}</summary>
			<div class="table-wrap mt-2">
				<table class="table table-sm">
					<thead><tr><th>{tr("Month")}</th><th>{tr("Quizzes")}</th><th>{tr("Your average")}</th><th>{tr("Your classes")}</th><th>{tr("Tutorials")}</th><th>{tr("Active days")}</th></tr></thead>
					<tbody>
						{#each [...shown].reverse() as m (m.month)}
							<tr><td>{monthLabel(m.month)}</td><td class="tabular">{m.quizzes}</td><td class="tabular">{m.avg ?? '—'}{m.avg !== null ? '%' : ''}</td><td class="tabular">{m.class_avg ?? '—'}{m.class_avg != null ? '%' : ''}</td><td class="tabular">{m.tutorials}</td><td class="tabular">{m.active}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</details>
	</section>
{/if}

<style>
	.tiles { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr)); }
</style>
