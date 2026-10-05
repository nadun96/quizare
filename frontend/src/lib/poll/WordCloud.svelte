<script lang="ts">
	// Live word cloud (D-40). Size encodes how many people gave a word; the most
	// frequent words also take the primary colour. Layout is a deterministic
	// spiral, so the same counts always give the same picture, and words glide
	// to their new place as counts change. A list is always available for
	// screen readers (and as the "Table" view).
	import { reduced } from '../ui/motion';
	import type { WordCount } from './types';
	import { layoutCloud } from './wordcloud';

	let { words, teacher = false, big = false, onhide }: { words: WordCount[]; teacher?: boolean; big?: boolean; onhide?: (w: WordCount) => void } = $props();

	let width = $state(0);
	// Wide but never taller than the room: capped on cards, and on the presenter
	// screen to just over half the window so the controls stay in view.
	let vh = $state(800);
	$effect(() => {
		vh = window.innerHeight;
		const r = () => (vh = window.innerHeight);
		addEventListener('resize', r);
		return () => removeEventListener('resize', r);
	});
	const height = $derived(big ? Math.round(Math.max(280, Math.min(width * 0.5, vh * 0.55))) : Math.round(Math.max(200, Math.min(width * 0.55, 360))));
	let ctx: CanvasRenderingContext2D | null = null;
	function measure(text: string, size: number): number {
		if (!ctx && typeof document !== 'undefined') ctx = document.createElement('canvas').getContext('2d');
		if (!ctx) return text.length * size * 0.55;
		ctx.font = `700 ${size}px system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`;
		return ctx.measureText(text).width;
	}
	const placed = $derived(layoutCloud(words, width, height, measure, big ? { maxWords: 120, minSize: 18, maxSize: 88 } : {}));
	const total = $derived(words.reduce((s, w) => s + (w.hidden ? 0 : w.count), 0));
</script>

<figure class="cloud m-0" class:animate={!reduced()} bind:clientWidth={width} aria-label="Word cloud of {words.length} words">
	{#if !words.length}
		<div class="empty muted small" style:height="{Math.min(height, 200)}px">Words appear here as people answer.</div>
	{:else}
		<svg viewBox="0 0 {width} {height}" width="100%" height={height} role="img" aria-hidden="true">
			{#each placed as p (p.w.word)}
				<g class="word" style:transform="translate({p.x}px, {p.y}px)">
					<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
					<text
						class:top={p.rank < 3}
						class:hidden-word={p.w.hidden}
						font-size={p.size}
						text-anchor="middle"
						dominant-baseline="central"
						role={teacher ? 'button' : undefined}
						tabindex={teacher ? 0 : undefined}
						onclick={() => teacher && onhide?.(p.w)}
						onkeydown={(e) => teacher && (e.key === 'Enter' || e.key === ' ') && (e.preventDefault(), onhide?.(p.w))}
					><title>{p.w.word}: {p.w.count}{p.w.hidden ? ' (hidden from participants)' : ''}{teacher ? ' — click to ' + (p.w.hidden ? 'show' : 'hide') : ''}</title>{p.w.word}</text>
				</g>
			{/each}
		</svg>
	{/if}
	<ul class="sr-only">
		{#each words as w (w.word)}<li>{w.word}: {w.count} of {total}{w.hidden ? ', hidden' : ''}</li>{/each}
	</ul>
</figure>

<style>
	.cloud { width: 100%; }
	.empty { display: grid; place-items: center; border: 2px dashed var(--color-base-300); border-radius: var(--radius-box); }
	text { font-weight: 700; fill: var(--color-base-content); font-family: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif; }
	text.top { fill: var(--color-primary); }
	text.hidden-word { fill: var(--color-muted); text-decoration: line-through; opacity: 0.55; }
	text[role='button'] { cursor: pointer; }
	text[role='button']:hover, text[role='button']:focus-visible { opacity: 0.7; outline: none; }
	.animate .word { transition: transform 600ms var(--ease-out); }
	.animate text { animation: pop-in 450ms var(--ease-out); transition: font-size 600ms var(--ease-out); }
	@keyframes pop-in { from { opacity: 0; } to { opacity: 1; } }
</style>
