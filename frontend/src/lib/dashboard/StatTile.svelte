<script lang="ts">
	// A stat tile (D-105): label, value, and the change against the period
	// before, coloured by whether that way is good, with an arrow and words
	// so colour never carries it alone.
	import Icon from '../ui/Icon.svelte';
	import { countTo } from '../ui/motion';
	import { t } from '../i18n/i18n.svelte';
	import { compact, delta, deltaText, type KPI } from './insights';

	let { label, kpi, unit = '', deltaUnit = '', upIsGood = true, versus, hint = '' }: { label: string; kpi?: KPI; unit?: string; deltaUnit?: string; upIsGood?: boolean; versus: string; hint?: string } = $props();
	const d = $derived(delta(kpi));
	const good = $derived(d === null || d === 0 ? null : d > 0 === upIsGood);
</script>

<div class="tile">
	<p class="label">{label}</p>
	<p class="value">{#if kpi && Math.abs(kpi.value) < 10000}<span use:countTo={kpi.value}>{compact(kpi.value)}</span>{:else}{kpi ? compact(kpi.value) : '—'}{/if}{#if kpi && unit}<span class="unit">{unit}</span>{/if}</p>
	{#if d !== null}
		<p class="delta" class:good={good === true} class:bad={good === false} title="Against {versus}">
			{#if d !== 0}<Icon name={d > 0 ? 'arrow-up' : 'arrow-down'} size={13} />{/if}
			<span>{deltaText(d, deltaUnit)}</span><span class="vs">{t('ins.vs', { prev: versus })}</span>
		</p>
	{:else}
		<p class="delta vs">{hint || ' '}</p>
	{/if}
</div>

<style>
	.tile { display: grid; gap: 0.15rem; padding: 0.9rem 1rem; border-radius: var(--radius-box); background: var(--color-base-100); border: 1px solid var(--color-base-300); min-width: 0; }
	.label { margin: 0; font-size: 0.8rem; color: color-mix(in oklab, var(--color-base-content) 70%, transparent); }
	.value { margin: 0; font-size: 1.85rem; font-weight: 650; line-height: 1.1; letter-spacing: -0.01em; }
	.unit { font-size: 1rem; font-weight: 600; margin-left: 0.1rem; color: color-mix(in oklab, var(--color-base-content) 70%, transparent); }
	.delta { margin: 0; display: flex; align-items: center; gap: 0.25rem; flex-wrap: wrap; font-size: 0.78rem; font-weight: 600; color: color-mix(in oklab, var(--color-base-content) 70%, transparent); min-height: 1.2em; }
	.delta.good { color: var(--viz-good-text); }
	.delta.bad { color: var(--viz-bad-text); }
	.vs { font-weight: 400; color: color-mix(in oklab, var(--color-base-content) 60%, transparent); }
</style>
