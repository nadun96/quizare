<script lang="ts">
	// A ring that fills as a student nears their next level (D-97), around
	// their avatar in the nav and, large, on the My page. The fill animates
	// only when it changes, and not at all with reduced motion. Square, like
	// everything else (D-106): it runs clockwise from the top-left corner.
	import type { Snippet } from 'svelte';
	import { reduced } from '../ui/motion';

	let { fraction, level, size = 40, stroke = 3, badge = true, children }: { fraction: number; level: number; size?: number; stroke?: number; badge?: boolean; children?: Snippet } = $props();
	const side = $derived(size - stroke);
</script>

<span class="lvl-ring" style:width="{size}px" style:height="{size}px" class:still={reduced()}>
	<svg width={size} height={size} viewBox="0 0 {size} {size}" aria-hidden="true">
		<rect class="track" x={stroke / 2} y={stroke / 2} width={side} height={side} stroke-width={stroke} fill="none" />
		<rect class="fill" x={stroke / 2} y={stroke / 2} width={side} height={side} stroke-width={stroke} fill="none" pathLength="100"
			stroke-dasharray="100" stroke-dashoffset={100 * (1 - Math.max(0.02, fraction))} />
	</svg>
	<span class="inner" style:inset="{stroke + 1}px">{@render children?.()}</span>
	{#if badge}<span class="lvl" aria-hidden="true">{level}</span>{/if}
</span>

<style>
	.lvl-ring { position: relative; display: inline-grid; flex: none; }
	.lvl-ring svg { position: absolute; inset: 0; }
	.track { stroke: color-mix(in oklab, var(--color-accent) 18%, transparent); }
	.fill { stroke: var(--color-accent); stroke-linecap: butt; transition: stroke-dashoffset 900ms cubic-bezier(0.22, 1, 0.36, 1); }
	.still .fill { transition: none; }
	.inner { position: absolute; display: grid; place-items: center; border-radius: 9999px; overflow: hidden; }
	.lvl { position: absolute; right: -4px; bottom: -3px; min-width: 1.15rem; height: 1.15rem; padding: 0 0.25rem; border-radius: 9999px; display: grid; place-items: center;
		background: var(--color-accent); color: var(--color-accent-content); font-size: 0.68rem; font-weight: 700; line-height: 1; box-shadow: 0 0 0 2px var(--color-base-100); }
</style>
