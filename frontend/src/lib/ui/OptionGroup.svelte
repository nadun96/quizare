<script lang="ts" module>
	import type { IconName } from './Icon.svelte';
	export type Choice = {
		v: string;
		label: string;
		icon?: IconName;
		/** One line, shown under the group while this option is selected. */
		hint?: string;
		disabled?: boolean;
		/** Why it's disabled, shown on hover and to screen readers. */
		why?: string;
	};
</script>

<script lang="ts">
	// One choice out of a few: icon plus a short label per option, and only the
	// selected option's explanation below (D-63), so the form reads as a row
	// of choices, not paragraphs. A radio group: arrow keys move, Home/End jump.
	// `fallback` is the inherited value: shown selected but soft, tagged
	// "default", until the person picks something here.
	import Icon from './Icon.svelte';

	let {
		options,
		value,
		onchange,
		label,
		fallback,
		size = 'sm'
	}: { options: Choice[]; value: string | undefined; onchange: (v: string) => void; label: string; fallback?: string; size?: 'sm' | 'md' } = $props();

	const shown = $derived(value ?? fallback);
	const current = $derived(options.find((o) => o.v === shown));
	const id = $props.id();
	let el = $state<HTMLDivElement>();

	function key(e: KeyboardEvent, i: number) {
		const enabled = options.map((o, j) => (o.disabled ? -1 : j)).filter((j) => j >= 0);
		const pos = enabled.indexOf(i);
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = enabled[(pos + 1) % enabled.length];
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = enabled[(pos - 1 + enabled.length) % enabled.length];
		else if (e.key === 'Home') next = enabled[0];
		else if (e.key === 'End') next = enabled[enabled.length - 1];
		if (next < 0) return;
		e.preventDefault();
		onchange(options[next].v);
		el?.querySelectorAll<HTMLButtonElement>('button')[next]?.focus();
	}
</script>

<div class="og-wrap">
	<div class="og og-{size}" role="radiogroup" aria-label={label} aria-describedby={current?.hint ? id + '-hint' : undefined} bind:this={el}>
		{#each options as o, i (o.v)}
			{@const on = shown === o.v}
			<button
				type="button"
				role="radio"
				aria-checked={on}
				tabindex={on || (shown === undefined && i === 0) ? 0 : -1}
				class:on
				class:soft={on && value === undefined}
				disabled={o.disabled}
				title={o.disabled ? o.why : undefined}
				aria-label={o.disabled && o.why ? `${o.label} (${o.why})` : undefined}
				onclick={() => onchange(o.v)}
				onkeydown={(e) => key(e, i)}
			>
				{#if o.icon}<Icon name={o.icon} size={size === 'md' ? 17 : 15} />{/if}<span>{o.label}</span>{#if on && value === undefined}<span class="dft">default</span>{/if}
			</button>
		{/each}
	</div>
	{#if current?.hint}<p class="og-hint" id="{id}-hint">{current.hint}</p>{/if}
</div>

<style>
	.og-wrap { display: grid; gap: 0.3rem; min-width: 0; }
	.og { display: flex; flex-wrap: wrap; gap: 0.3rem; }
	.og button { display: inline-flex; align-items: center; gap: 0.4rem; min-height: 2.25rem; padding: 0 0.75rem; border-radius: var(--radius-field); border: 1px solid var(--color-base-300); background: var(--color-base-100); color: var(--color-base-content); font-size: 0.875rem; font-weight: 550; cursor: pointer; transition: border-color var(--motion-fast), background-color var(--motion-fast); }
	.og-md button { min-height: 2.6rem; padding: 0 0.9rem; font-size: 0.925rem; }
	.og button :global(svg) { color: var(--color-muted); flex: none; }
	.og button:hover:not(:disabled) { border-color: color-mix(in oklab, var(--color-primary) 50%, var(--color-base-300)); }
	.og button:focus-visible { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.og button.on { border-color: var(--color-primary); background: var(--color-primary); color: var(--color-primary-content); }
	.og button.on :global(svg) { color: currentColor; }
	/* Inherited: clearly the one that applies, but not a choice made here. */
	.og button.soft { background: color-mix(in oklab, var(--color-primary) 10%, var(--color-base-100)); color: var(--color-base-content); border-style: dashed; }
	.og button.soft :global(svg) { color: var(--color-primary); }
	.og button:disabled { opacity: 0.45; cursor: not-allowed; }
	.dft { font-size: 0.7rem; font-weight: 600; color: var(--color-muted); }
	.og-hint { margin: 0; font-size: 0.825rem; color: var(--color-muted); line-height: 1.4; }
</style>
