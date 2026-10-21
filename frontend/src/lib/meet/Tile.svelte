<script lang="ts">
	// One person's camera (or a shared screen) in a meeting. With the camera
	// off it shows their initials; the video element stays, since it also
	// plays their sound.
	import type { Snippet } from 'svelte';
	import Icon from '../ui/Icon.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { initials } from './mesh';

	let {
		name,
		stream,
		rev = 0,
		video = true,
		self = false,
		screen = false,
		mic = true,
		hand = false,
		host = false,
		status = '',
		actions
	}: {
		name: string;
		stream: MediaStream | null;
		/** Bumped when the stream's tracks change. */
		rev?: number;
		video?: boolean;
		self?: boolean;
		screen?: boolean;
		mic?: boolean;
		hand?: boolean;
		host?: boolean;
		/** "Connecting…" and the like, while the link isn't up. */
		status?: string;
		actions?: Snippet;
	} = $props();
	let el = $state<HTMLVideoElement>();
	let box = $state<HTMLElement>();

	$effect(() => {
		void rev;
		const v = el;
		if (!v) return;
		if (v.srcObject !== stream) v.srcObject = stream;
		// Some browsers stop playing when the tracks under a stream change.
		if (stream) v.play?.().catch(() => {});
	});

	function fullscreen() {
		if (document.fullscreenElement) document.exitFullscreen();
		else box?.requestFullscreen?.().catch(() => {});
	}
</script>

<div class="tile" class:screen class:self bind:this={box}>
	<!-- svelte-ignore a11y_media_has_caption -->
	<video bind:this={el} autoplay playsinline muted={self} class:hidden={!video} aria-label={screen ? `${name}'s screen` : `${name}'s camera`}></video>
	{#if !video}
		<div class="face" aria-hidden="true"><span>{initials(name)}</span></div>
	{/if}
	{#if status}<span class="status badge badge-sm">{status}</span>{/if}
	<div class="cap small">
		{#if hand}<span class="raised" title="Hand raised"><Icon name="hand" size={14} label="Hand raised" /></span>{/if}
		{#if !screen && !mic}<Icon name="mic-off" size={14} label="Muted" />{/if}
		<span class="truncate">{screen ? `${self ? 'Your' : name + "'s"} screen` : self ? `${name} (you)` : name}</span>
		{#if host && !screen}<span class="badge badge-xs">Host</span>{/if}
		<span class="spacer"></span>
		{@render actions?.()}
		{#if video}<IconBtn icon="maximize" label="Full screen" class="btn-xs btn-ghost" onclick={fullscreen} />{/if}
	</div>
</div>

<style>
	.tile { position: relative; background: #111; border-radius: 0.75rem; overflow: hidden; aspect-ratio: 16 / 9; display: flex; min-width: 0; }
	.tile video { width: 100%; height: 100%; object-fit: cover; }
	.tile video.hidden { position: absolute; opacity: 0; pointer-events: none; }
	.tile.self:not(.screen) video { transform: scaleX(-1); }
	.tile.screen video { object-fit: contain; }
	.tile:fullscreen { border-radius: 0; aspect-ratio: auto; }
	.face { flex: 1; display: grid; place-items: center; }
	.face span { display: grid; place-items: center; width: clamp(3rem, 22%, 6rem); aspect-ratio: 1; border-radius: 50%; background: var(--color-primary); color: var(--color-primary-content); font-size: clamp(1.1rem, 4vw, 2rem); font-weight: 600; }
	.status { position: absolute; top: 0.5rem; left: 0.5rem; }
	.cap { position: absolute; left: 0; right: 0; bottom: 0; display: flex; align-items: center; gap: 0.35rem; padding: 0.3rem 0.5rem; color: #fff; background: linear-gradient(transparent, rgb(0 0 0 / 0.7)); }
	.cap :global(.btn-ghost) { color: #fff; }
	.raised { display: inline-grid; place-items: center; padding: 0.15rem; border-radius: 0.35rem; background: var(--color-warning); color: var(--color-warning-content); }
</style>
