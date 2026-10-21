<script lang="ts">
	// Live word cloud (D-40). Size encodes how many people gave a word; each
	// word has its own colour from the categorical palette, made readable on
	// the current theme (D-67). Layout is a deterministic
	// spiral, so the same counts always give the same picture, and words glide
	// to their new place as counts change. A list is always available for
	// screen readers (and as the "Table" view).
	// A condensed face (D-68), like printed word clouds: more words fit, and
	// it loads only on pages that show a cloud. Latin only; other scripts
	// fall back to the system font.
	import '@fontsource/fira-sans-extra-condensed/latin-600.css';
	import { reduced } from '../ui/motion';
	import type { WordCount } from './types';
	import { layoutCloud } from './wordcloud';
	import { cloudPalette, wordColor, type CloudPalette } from './cloudColors';

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
	const FONT = `"Fira Sans Extra Condensed", system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`;
	// Words are measured with the font, so the cloud is laid out again once it has loaded.
	let fontReady = $state(false);
	$effect(() => {
		document.fonts?.load(`600 40px "Fira Sans Extra Condensed"`).then(() => (fontReady = true), () => (fontReady = true));
	});
	let ctx: CanvasRenderingContext2D | null = null;
	function measure(text: string, size: number): number {
		if (!ctx && typeof document !== 'undefined') ctx = document.createElement('canvas').getContext('2d');
		if (!ctx) return text.length * size * 0.45;
		ctx.font = `600 ${size}px ${FONT}`;
		return ctx.measureText(text).width;
	}
	const placed = $derived((void fontReady, layoutCloud(words, width, height, measure, big ? { maxWords: 140, minSize: 18, maxSize: 96 } : { maxWords: 90 })));
	const total = $derived(words.reduce((s, w) => s + (w.hidden ? 0 : w.count), 0));

	// The palette and the backgrounds come from the theme's CSS variables, so
	// they're read in the browser and again whenever the theme changes.
	let el = $state<HTMLElement>();
	let palette = $state<CloudPalette>({ large: [], small: [] });
	function readPalette() {
		if (!el) return;
		const css = getComputedStyle(el);
		const v = (name: string) => css.getPropertyValue(name).trim();
		try {
			const cats = [1, 2, 3, 4, 5, 6, 7, 8].map((i) => v('--cat-' + i));
			palette = cloudPalette(cats, [v('--color-base-100'), v('--color-base-200')].filter(Boolean));
		} catch {
			palette = { large: [], small: [] }; // an unreadable colour: words fall back to the ink colour
		}
	}
	$effect(() => {
		if (!el) return;
		readPalette();
		const mo = new MutationObserver(readPalette);
		mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'style', 'class'] });
		const mq = matchMedia('(prefers-color-scheme: dark)');
		mq.addEventListener('change', readPalette);
		return () => {
			mo.disconnect();
			mq.removeEventListener('change', readPalette);
		};
	});
</script>

<figure class="cloud m-0" class:animate={!reduced()} bind:clientWidth={width} bind:this={el} aria-label="Word cloud of {words.length} words">
	{#if !words.length}
		<div class="empty muted small" style:height="{Math.min(height, 200)}px">Words appear here as people answer.</div>
	{:else}
		<svg viewBox="0 0 {width} {height}" width="100%" height={height} role="img" aria-hidden="true">
			{#each placed as p (p.w.word)}
				<g class="word" style:transform="translate({p.x}px, {p.y}px)">
					<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
					<text
						class:hidden-word={p.w.hidden}
						style:fill={p.w.hidden ? undefined : wordColor(palette, p.w.word, p.size)}
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
	text { font-weight: 600; fill: var(--color-base-content); font-family: 'Fira Sans Extra Condensed', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif; letter-spacing: 0.005em; }
	text.hidden-word { fill: var(--color-muted); text-decoration: line-through; opacity: 0.55; }
	text[role='button'] { cursor: pointer; }
	text[role='button']:hover, text[role='button']:focus-visible { opacity: 0.7; outline: none; }
	.animate .word { transition: transform 600ms var(--ease-out); }
	.animate text { animation: pop-in 450ms var(--ease-out); transition: font-size 600ms var(--ease-out), fill 300ms; }
	@keyframes pop-in { from { opacity: 0; } to { opacity: 1; } }
</style>
