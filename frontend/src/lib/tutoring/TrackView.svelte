<script lang="ts">
	// One video track, labelled, with full screen (TS-FR-14). Screens fit
	// whole ("contain"), so text is never cut off; cameras fill their tile.
	// A shared screen can be zoomed: the buttons, Ctrl + wheel, a pinch or a
	// double click, and dragged around while zoomed.
	import type { Tile } from './media.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { NONE, panBy, percent, zoomAt, type Zoom } from './zoom';

	let { tile, big = false, onstop }: { tile: Tile; big?: boolean; onstop?: () => void } = $props();
	let video = $state<HTMLVideoElement>();
	let box = $state<HTMLElement>();
	let z = $state<Zoom>(NONE);
	const screen = $derived(tile.source === 'screen_share');

	$effect(() => {
		const v = video;
		const t = tile.track;
		if (!v) return;
		t.attach(v);
		return () => {
			t.detach(v);
		};
	});

	function fullscreen() {
		if (document.fullscreenElement) document.exitFullscreen();
		else box?.requestFullscreen?.().catch(() => {});
	}

	const size = () => ({ w: box?.clientWidth ?? 1, h: box?.clientHeight ?? 1 });
	function zoomBy(factor: number, px?: number, py?: number) {
		const { w, h } = size();
		z = zoomAt(z, factor, px ?? w / 2, py ?? h / 2, w, h);
	}
	function point(e: { clientX: number; clientY: number }) {
		const r = box!.getBoundingClientRect();
		return { x: e.clientX - r.left, y: e.clientY - r.top };
	}

	function wheel(e: WheelEvent) {
		// Plain scrolling keeps scrolling the page, unless the screen is zoomed.
		if (!screen || (!e.ctrlKey && !e.metaKey && z.scale === 1)) return;
		e.preventDefault();
		const p = point(e);
		zoomBy(Math.exp(-e.deltaY / 300), p.x, p.y);
	}
	function dblclick(e: MouseEvent) {
		if (!screen) return;
		const p = point(e);
		if (z.scale > 1) z = NONE;
		else zoomBy(2.5, p.x, p.y);
	}

	// Dragging pans; two pointers pinch.
	const pointers = new Map<number, { x: number; y: number }>();
	let pinch = 0;
	function down(e: PointerEvent) {
		if (!screen || (e.target as HTMLElement).closest('button')) return;
		pointers.set(e.pointerId, point(e));
		(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
		if (pointers.size === 2) pinch = spread();
	}
	function spread() {
		const [a, b] = [...pointers.values()];
		return Math.hypot(a.x - b.x, a.y - b.y);
	}
	function move(e: PointerEvent) {
		const last = pointers.get(e.pointerId);
		if (!last) return;
		const p = point(e);
		pointers.set(e.pointerId, p);
		const { w, h } = size();
		if (pointers.size === 2) {
			const d = spread();
			if (pinch > 0 && d > 0) {
				const [a, b] = [...pointers.values()];
				z = zoomAt(z, d / pinch, (a.x + b.x) / 2, (a.y + b.y) / 2, w, h);
			}
			pinch = d;
		} else if (z.scale > 1) {
			z = panBy(z, p.x - last.x, p.y - last.y, w, h);
		}
	}
	function up(e: PointerEvent) {
		pointers.delete(e.pointerId);
		pinch = pointers.size === 2 ? spread() : 0;
	}
	function key(e: KeyboardEvent) {
		if (!screen) return;
		if (e.key === '+' || e.key === '=') zoomBy(1.25);
		else if (e.key === '-') zoomBy(0.8);
		else if (e.key === '0') z = NONE;
		else return;
		e.preventDefault();
	}
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="tile" class:big class:screen class:zoomed={z.scale > 1} bind:this={box}
	tabindex={screen ? 0 : undefined} role={screen ? 'group' : undefined} aria-label={screen ? `${tile.label} from ${tile.name}: + and − zoom, 0 resets` : undefined}
	onwheel={wheel} ondblclick={dblclick} onpointerdown={down} onpointermove={move} onpointerup={up} onpointercancel={up} onkeydown={key}>
	<div class="pan" style:transform={z.scale > 1 ? `translate(${z.x}px, ${z.y}px) scale(${z.scale})` : undefined}>
		<!-- svelte-ignore a11y_media_has_caption -->
		<video bind:this={video} autoplay playsinline muted={tile.local} aria-label="{tile.label} from {tile.name}"></video>
	</div>
	<div class="cap small">
		<span class="truncate">{tile.label} · {tile.local ? 'you' : tile.name}</span>
		<span class="spacer"></span>
		{#if tile.local && onstop}<button class="btn btn-xs" onclick={onstop}>Stop</button>{/if}
		{#if screen && !tile.local}
			<IconBtn icon="minus" label="Zoom out" class="btn-xs btn-ghost" disabled={z.scale <= 1} onclick={() => zoomBy(0.8)} />
			<button class="btn btn-xs btn-ghost zoom-level" onclick={() => (z = NONE)} title="Back to the whole screen" aria-label="Zoom {percent(z)}: back to the whole screen">{percent(z)}</button>
			<IconBtn icon="plus" label="Zoom in" class="btn-xs btn-ghost" onclick={() => zoomBy(1.25)} />
		{/if}
		<IconBtn icon="maximize" label="Full screen" class="btn-xs btn-ghost" onclick={fullscreen} />
	</div>
</div>

<style>
	.tile { position: relative; margin: 0; background: #000; border-radius: 0.75rem; overflow: hidden; aspect-ratio: 16 / 9; display: flex; }
	.pan { width: 100%; height: 100%; transform-origin: 0 0; }
	.tile video { width: 100%; height: 100%; object-fit: cover; display: block; }
	.tile.screen video { object-fit: contain; }
	.tile.screen { touch-action: pinch-zoom; }
	.tile.zoomed { cursor: grab; touch-action: none; }
	.tile.zoomed:active { cursor: grabbing; }
	.tile:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
	.tile:fullscreen { border-radius: 0; aspect-ratio: auto; }
	.cap { position: absolute; left: 0; right: 0; bottom: 0; display: flex; align-items: center; gap: 0.25rem; padding: 0.25rem 0.5rem; color: #fff; background: linear-gradient(transparent, rgb(0 0 0 / 0.65)); }
	.cap :global(.btn-ghost) { color: #fff; }
	.zoom-level { min-width: 3.5rem; font-variant-numeric: tabular-nums; }
</style>
