<script lang="ts">
	// A range slider that only counts once moved: "untouched" is not an answer.
	let { min = 0, max = 100, step = 1, minLabel = '', maxLabel = '', value, disabled = false, label, onchange }: {
		min?: number; max?: number; step?: number; minLabel?: string; maxLabel?: string; value?: number; disabled?: boolean; label: string; onchange: (v: number | undefined) => void;
	} = $props();
	const current = $derived(value ?? (min + max) / 2);
	const snapped = (v: number) => Math.round((v - min) / step) * step + min;
	const pct = $derived(((current - min) / (max - min || 1)) * 100);
	const fmt = (v: number) => (Number.isInteger(step) ? String(Math.round(v)) : v.toFixed(String(step).split('.')[1]?.length ?? 1));
</script>

<div class="slider" class:untouched={value === undefined}>
	<div class="bubble-track" aria-hidden="true">
		<span class="bubble tabular" style:left="{pct}%">{value === undefined ? '?' : fmt(value)}</span>
	</div>
	<input
		type="range"
		class="range range-primary w-full"
		{min}
		{max}
		{step}
		value={current}
		{disabled}
		aria-label={label}
		aria-valuetext={value === undefined ? 'Not answered yet' : fmt(value)}
		oninput={(e) => onchange(snapped(Number(e.currentTarget.value)))}
	/>
	<div class="flex justify-between small muted mt-1">
		<span>{minLabel || fmt(min)}</span>
		<span>{maxLabel || fmt(max)}</span>
	</div>
	{#if value === undefined}<p class="small muted m-0 mt-1">Move the slider to answer.</p>{:else if !disabled}<button type="button" class="btn btn-ghost btn-xs mt-1" onclick={() => onchange(undefined)}>Clear</button>{/if}
</div>

<style>
	.slider { padding-top: 0.25rem; }
	.bubble-track { position: relative; height: 2rem; margin: 0 0.75rem; }
	.bubble { position: absolute; transform: translateX(-50%); bottom: 0.25rem; min-width: 2.25rem; text-align: center; padding: 0.1rem 0.5rem; border-radius: 0.5rem; font-weight: 700; background: var(--color-primary); color: var(--color-primary-content); transition: left var(--motion-fast) linear; }
	.untouched .bubble { background: var(--color-base-200); color: var(--color-muted); }
	.untouched :global(.range) { --range-fill: 0; opacity: 0.75; }
</style>
