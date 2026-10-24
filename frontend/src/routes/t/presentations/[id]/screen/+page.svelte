<script lang="ts">
	// The projector's view of a live presentation (D-77): only the current
	// slide, filling the window, following the presenter. Arrow keys move it
	// too, for a teacher at that computer. Press F or double-click for full screen.
	import { onMount } from 'svelte';
	import { page as appPage } from '$app/state';
	import { api } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { slideAlt, type Live, type Presentation, type Slide } from '$lib/presentation/presentation';
	import { LiveSocket } from '$lib/socket';

	const ready = requireRole('teacher');
	const id = $derived(appPage.params.id ?? '');
	let p = $state<Presentation | null>(null);
	let slides = $state<Slide[]>([]);
	let live = $state<Live | null>(null);
	let ended = $state(false);
	let socket: LiveSocket | null = null;

	$effect(() => {
		if (!ready() || !id || p) return;
		(async () => {
			p = await api.get<Presentation>('/api/teacher/presentations/' + id);
			slides = (await api.get<{ slides: Slide[] }>(`/api/teacher/presentations/${id}/slides`)).slides;
			live = await api.get<Live>(`/api/teacher/presentations/${id}/live`);
			socket = new LiveSocket('/ws/presentations/' + id);
			socket.onMessage = (m) => {
				if (m.type === 'live') live = m.live as Live;
				else if (m.type === 'ended') {
					ended = true;
					socket?.close();
				}
			};
			socket.open();
		})().catch(() => (ended = true));
	});
	onMount(() => () => socket?.close());

	const cur = $derived(live ? slides[live.slide - 1] : undefined);
	// Load the next slide ahead, so it appears at once.
	$effect(() => {
		if (live && p && live.slide < slides.length) new Image().src = `/api/teacher/presentations/${id}/slides/${live.slide + 1}?v=${p.version}`;
	});
	function key(e: KeyboardEvent) {
		if (!live) return;
		let n = 0;
		if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') n = live.slide + 1;
		else if (e.key === 'ArrowLeft' || e.key === 'PageUp') n = live.slide - 1;
		else if (e.key === 'f' || e.key === 'F') return full();
		if (n >= 1 && n <= slides.length) {
			e.preventDefault();
			api.post<Live>(`/api/teacher/presentations/${id}/live/slide`, { slide: n }).then((l) => (live = l), () => {});
		}
	}
	function full() {
		if (document.fullscreenElement) document.exitFullscreen();
		else document.documentElement.requestFullscreen().catch(() => {});
	}
</script>

<svelte:head><title>{p?.title ?? 'Presentation'}</title></svelte:head>
<svelte:window onkeydown={key} />

<div class="screen" role="presentation" ondblclick={full}>
	{#if ended}
		<p class="msg">The presentation has ended.</p>
	{:else if cur && p}
		<img src={`/api/teacher/presentations/${id}/slides/${cur.n}?v=${p.version}`} alt={slideAlt(cur, slides.length)} />
	{:else}
		<p class="msg">Waiting for the presentation…</p>
	{/if}
</div>

<style>
	.screen { position: fixed; inset: 0; z-index: 100; background: #000; display: grid; place-items: center; }
	.screen img { width: 100vw; height: 100vh; object-fit: contain; }
	.msg { color: #ddd; font-size: 1.5rem; }
</style>
