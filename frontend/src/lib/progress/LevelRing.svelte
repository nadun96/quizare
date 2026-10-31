<script lang="ts">
	// A ring that fills as a student nears their next level (D-97), around
	// their avatar in the nav and, large, on the My page. The fill animates
	// only when it changes, and not at all with reduced motion. Round, like
	// the picture it frames: profile pictures stay round (D-110).
	import type { Snippet } from 'svelte';
	import { reduced } from '../ui/motion';

	let { fraction, level, size = 40, stroke = 3, badge = true, children }: { fraction: number; level: number; size?: number; stroke?: number; badge?: boolean; children?: Snippet } = $props();
	const r = $derived((size - stroke) / 2);
	const c = $derived(2 * Math.PI * r);
</script>

<span class="lvl-ring round" style:width="{size}px" style:height="{size}px" class:still={reduced()}>
	<svg width={size} height={size} viewBox="0 0 {size} {size}" aria-hidden="true">
		<circle class="track" cx={size / 2} cy={size / 2} {r} stroke-width={stroke} fill="none" />
		<circle class="fill" cx={size / 2} cy={size / 2} {r} stroke-width={stroke} fill="none"
			stroke-dasharray={c} stroke-dashoffset={c * (1 - Math.max(0.02, fraction))} transform="rotate(-90 {size / 2} {size / 2})" />
	</svg>
	<span class="inner round" style:inset="{stroke + 1}px">{@render children?.()}</span>
	{#if badge}<span class="lvl" aria-hidden="true">{level}</span>{/if}
</span>

<style>
	.lvl-ring { position: relative; display: inline-grid; flex: none; }
	.lvl-ring svg { position: absolute; inset: 0; }
	.track { stroke: color-mix(in oklab, var(--color-accent) 18%, transparent); }
	.fill { stroke: var(--color-accent); stroke-linecap: round; transition: stroke-dashoffset 900ms cubic-bezier(0.22, 1, 0.36, 1); }
	.still .fill { transition: none; }
	.inner { position: absolute; display: grid; place-items: center; border-radius: 9999px; overflow: hidden; }
	.lvl { position: absolute; right: -4px; bottom: -3px; min-width: 1.15rem; height: 1.15rem; padding: 0 0.25rem; border-radius: 9999px; display: grid; place-items: center;
		background: var(--color-accent); color: var(--color-accent-content); font-size: 0.68rem; font-weight: 700; line-height: 1; box-shadow: 0 0 0 2px var(--color-base-100); }
</style>
