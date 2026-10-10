<script lang="ts">
	// One video track, labelled, with full screen (TS-FR-14). Screens fit
	// whole ("contain"), so text is never cut off; cameras fill their tile.
	import type { Tile } from './media.svelte';
	import IconBtn from '../ui/IconBtn.svelte';

	let { tile, big = false, onstop }: { tile: Tile; big?: boolean; onstop?: () => void } = $props();
	let video = $state<HTMLVideoElement>();
	let box = $state<HTMLElement>();

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
</script>

<div class="tile" class:big class:screen={tile.source === 'screen_share'} bind:this={box}>
	<!-- svelte-ignore a11y_media_has_caption -->
	<video bind:this={video} autoplay playsinline muted={tile.local} aria-label="{tile.label} from {tile.name}"></video>
	<div class="cap small">
		<span class="truncate">{tile.label} · {tile.local ? 'you' : tile.name}</span>
		<span class="spacer"></span>
		{#if tile.local && onstop}<button class="btn btn-xs" onclick={onstop}>Stop</button>{/if}
		<IconBtn icon="maximize" label="Full screen" class="btn-xs btn-ghost" onclick={fullscreen} />
	</div>
</div>

<style>
	.tile { position: relative; margin: 0; background: #000; border-radius: 0.75rem; overflow: hidden; aspect-ratio: 16 / 9; display: flex; }
	.tile video { width: 100%; height: 100%; object-fit: cover; }
	.tile.screen video { object-fit: contain; }
	.tile:fullscreen { border-radius: 0; aspect-ratio: auto; }
	.cap { position: absolute; left: 0; right: 0; bottom: 0; display: flex; align-items: center; gap: 0.25rem; padding: 0.25rem 0.5rem; color: #fff; background: linear-gradient(transparent, rgb(0 0 0 / 0.65)); }
	.cap :global(.btn-ghost) { color: #fff; }
</style>
