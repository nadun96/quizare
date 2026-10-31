<script lang="ts">
	// Twelve weeks of activity (D-105): a cell a week, darker for more (a
	// five-step ordinal blue ramp; an empty week stays the surface's gray).
	// Each cell says its week and counts on hover and focus; a Less–More key
	// reads the ramp, and the table gives every number.
	import { step, weekLabel, type Week } from './insights';
	import { cascade } from '../ui/motion';
	import { t } from '../i18n/i18n.svelte';

	let { weeks, measure, title, detail = (w: Week) => `${w.quizzes} quizzes, ${w.tutorials} tutorials` }: { weeks: Week[]; measure: (w: Week) => number; title: string; detail?: (w: Week) => string } = $props();
	const max = $derived(Math.max(0, ...weeks.map(measure)));
	const total = $derived(weeks.reduce((s, w) => s + measure(w), 0));
	const activeWeeks = $derived(weeks.filter((w) => measure(w) > 0).length);
	let active = $state<number | null>(null);
</script>

<div class="vstack gap-2">
	<p class="m-0 small muted">{t('ins.inWeeks', { n: total, weeks: activeWeeks })}</p>
	<div class="relative">
		<ol class="strip" aria-label={title} use:cascade={{ y: 8, stagger: 0.03 }}>
			{#each weeks as w, i (w.week)}
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<li class="cell s{step(measure(w), max)}" class:now={i === weeks.length - 1} tabindex="0" aria-label="{t('ins.weekOf', { week: weekLabel(w.week) })}: {detail(w)}"
					onpointerenter={() => (active = i)} onpointerleave={() => (active = null)} onfocus={() => (active = i)} onblur={() => (active = null)}></li>
			{/each}
		</ol>
		{#if active !== null && weeks[active]}
			<div class="tip small" style="left: {((active + 0.5) / weeks.length) * 100}%" aria-hidden="true">
				<p class="m-0"><strong class="text-base">{measure(weeks[active])}</strong></p>
				<p class="m-0 muted">{t('ins.weekOf', { week: weekLabel(weeks[active].week) })}</p>
				<p class="m-0 muted">{detail(weeks[active])}</p>
			</div>
		{/if}
	</div>
	<div class="flex items-center justify-between gap-2 small muted">
		<span>{weeks.length ? weekLabel(weeks[0].week) : ''}</span>
		<span class="key" aria-hidden="true">{t('ins.less')} <i class="cell s0"></i><i class="cell s1"></i><i class="cell s3"></i><i class="cell s5"></i> {t('ins.more')}</span>
		<span>{t('ins.thisWeek')}</span>
	</div>
	<details class="small">
		<summary>{t('common.showTable')}</summary>
		<table class="table table-sm">
			<thead><tr><th>Week of</th><th>Count</th><th>Details</th></tr></thead>
			<tbody>{#each weeks as w (w.week)}<tr><td>{weekLabel(w.week)}</td><td class="tabular">{measure(w)}</td><td>{detail(w)}</td></tr>{/each}</tbody>
		</table>
	</details>
</div>

<style>
	.strip { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 2px; }
	.cell { display: block; height: 2.25rem; border-radius: 4px; background: var(--color-base-200); outline: none; transition: transform 120ms; }
	.strip .cell:hover, .strip .cell:focus-visible { transform: translateY(-2px); box-shadow: 0 0 0 2px var(--color-base-100), 0 0 0 4px var(--color-base-content); }
	.cell.now { box-shadow: inset 0 -3px 0 color-mix(in oklab, var(--color-base-content) 35%, transparent); }
	.s1 { background: var(--viz-seq-1); }
	.s2 { background: var(--viz-seq-2); }
	.s3 { background: var(--viz-seq-3); }
	.s4 { background: var(--viz-seq-4); }
	.s5 { background: var(--viz-seq-5); }
	.key { display: inline-flex; align-items: center; gap: 2px; }
	.key .cell { width: 12px; height: 12px; border-radius: 3px; display: inline-block; }
	.tip { position: absolute; bottom: calc(100% + 6px); transform: translateX(-50%); pointer-events: none; background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: 10px; padding: 0.45rem 0.6rem; box-shadow: 0 6px 18px rgb(0 0 0 / 0.14); min-width: 9rem; display: grid; gap: 0.1rem; z-index: 2; white-space: nowrap; }
</style>
