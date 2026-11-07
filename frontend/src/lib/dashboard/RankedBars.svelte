<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Ranked horizontal bars (D-105): one per classroom, largest first, one
	// hue on a lighter track of the same ramp, the value at the tip in ink
	// and a second figure beside it. No value: an empty track and "—".
	import { grow } from '../ui/motion';
	type Row = { label: string; value: number | null; note: string };
	// max is the full bar: 100 for percentages; Explore passes the largest count (D-120).
	let { rows, title, unit = '%', max = 100 }: { rows: Row[]; title: string; unit?: string; max?: number } = $props();
</script>

<ol class="bars" aria-label={title} use:grow={rows}>
	{#each rows as r (r.label)}
		<li aria-label="{r.label}: {r.value === null ? 'no results' : r.value + unit}, {r.note}">
			<div class="head small"><span class="name truncate">{tr(r.label)}</span><span class="figs"><strong>{r.value === null ? '—' : r.value + unit}</strong><span class="muted">{r.note}</span></span></div>
			<div class="track" aria-hidden="true"><div class="fill" data-grow="x" style="width: {Math.max(0, Math.min(100, ((r.value ?? 0) / (max || 1)) * 100))}%"></div></div>
		</li>
	{/each}
</ol>

<style>
	.bars { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.75rem; }
	.head { display: flex; justify-content: space-between; gap: 0.75rem; margin-bottom: 0.3rem; }
	.name { min-width: 0; font-weight: 600; }
	.figs { display: inline-flex; gap: 0.5rem; white-space: nowrap; font-variant-numeric: tabular-nums; }
	.track { height: 10px; border-radius: 5px; background: var(--viz-accent-wash); overflow: hidden; }
	.fill { height: 100%; border-radius: 5px; background: var(--viz-accent); }
</style>
