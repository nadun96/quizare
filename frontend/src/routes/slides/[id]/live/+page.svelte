<script lang="ts">
	// A student in a live presentation (D-77, PR-FR-11): the teacher's slide,
	// and, when the teacher allows, looking back on one's own with a button to
	// follow again. The page tells the server which slide is on screen and
	// whether the tab is visible; the server counts the time from that.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { liveCanGo, slideAlt, type Live, type LiveView } from '$lib/presentation/presentation';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { LiveSocket } from '$lib/socket';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';

	const id = $derived(appPage.params.id ?? '');
	let v = $state<LiveView | null>(null);
	let live = $state<Live | null>(null);
	let problem = $state('');
	let ended = $state(false);
	let following = $state(true);
	let mine = $state(1); // the slide when looking back
	let connected = $state(false);
	let stage = $state<HTMLElement | null>(null);
	let full = $state(false);
	let socket: LiveSocket | null = null;

	const n = $derived(live ? (following ? live.slide : mine) : 1);
	const total = $derived(v?.slides.length ?? 0);
	const slide = $derived(v?.slides[n - 1]);

	function report() {
		if (!live || ended) return;
		socket?.send({ type: 'state', slide: n, following, visible: document.visibilityState === 'visible' });
	}
	function stillLive() {
		api.post<LiveView>(`/api/presentations/${id}/live/join`).then(
			(r) => (live = r.live),
			(e) => {
				if (e instanceof ApiError && e.status === 404) finish();
			}
		);
	}
	function finish() {
		ended = true;
		socket?.close();
	}
	$effect(() => {
		if (!auth.loaded || v || problem) return;
		if (!auth.user) {
			goto(loginUrl(`/slides/${id}/live`), { replaceState: true });
			return;
		}
		api.post<LiveView>(`/api/presentations/${id}/live/join`).then(
			(r) => {
				v = r;
				live = r.live;
				socket = new LiveSocket('/ws/presentations/' + id);
				socket.onStatus = (c) => {
					connected = c;
					if (c) report();
					else stillLive(); // ended while we were away?
				};
				socket.onMessage = (m) => {
					if (m.type === 'live') {
						live = m.live as Live;
						if (!live.allow_browse) following = true;
					} else if (m.type === 'ended') finish();
				};
				socket.open();
			},
			(e) => (problem = e instanceof ApiError ? (e.code === 'not_live' ? 'This presentation isn’t being presented now.' : e.message) : 'Could not join')
		);
	});
	// Tell the server whenever the slide or following changes, and every 15 s.
	$effect(() => {
		void n, following;
		report();
	});
	onMount(() => {
		const t = setInterval(report, 15_000);
		const vis = () => report();
		document.addEventListener('visibilitychange', vis);
		return () => {
			clearInterval(t);
			document.removeEventListener('visibilitychange', vis);
			socket?.close();
		};
	});

	// Looking back on one's own; reaching the teacher's slide follows again.
	function go(to: number) {
		if (!live || !liveCanGo(live, to)) return;
		if (to === live.slide) return follow();
		mine = to;
		following = false;
	}
	function follow() {
		following = true;
	}
	function key(e: KeyboardEvent) {
		if (!live || (e.target as HTMLElement)?.closest('input, select, textarea')) return;
		if (e.key === 'ArrowLeft') go(n - 1);
		else if (e.key === 'ArrowRight') go(n + 1);
		else return;
		e.preventDefault();
	}
	async function fullscreen() {
		if (document.fullscreenElement) await document.exitFullscreen();
		else await stage?.requestFullscreen().catch(() => {});
	}
</script>

<svelte:head><title>{v?.presentation.title ?? 'Live presentation'}</title></svelte:head>
<svelte:window onkeydown={key} />
<svelte:document onfullscreenchange={() => (full = !!document.fullscreenElement)} />

<div class="page-container viewer">
	{#if problem}
		<div class="card card-border bg-base-100 shadow-sm p-6 text-center vstack" role="alert">
			<h1 class="m-0">Can’t join</h1>
			<p class="muted m-0">{problem}</p>
			<a class="btn mx-auto" href="/my">Back to my page</a>
		</div>
	{:else if ended}
		<div class="card card-border bg-base-100 shadow-sm p-6 text-center vstack" role="status">
			<h1 class="m-0">The presentation has ended</h1>
			{#if v?.presentation.published}<p class="muted m-0">You can go through the slides again at your own pace.</p>
				<a class="btn btn-primary mx-auto" href={'/slides/' + id}>See the slides</a>
			{:else}<a class="btn mx-auto" href="/my">Back to my page</a>{/if}
		</div>
	{:else if !v || !live || !slide}
		<Skeleton lines={8} />
	{:else}
		<header class="flex flex-wrap items-center gap-3">
			<h1 class="m-0">{v.presentation.title}</h1>
			<span class="badge badge-error badge-soft">Live</span>
			{#if !connected}<span class="badge badge-warning badge-sm">Reconnecting…</span>{/if}
		</header>
		<section class="stage" class:full bind:this={stage} aria-label="Slides">
			<img src={`/api/presentations/${id}/slides/${n}?v=${v.presentation.version}`} alt={slideAlt(slide, total)} width={slide.width} height={slide.height} />
			<div class="controls">
				{#if live.allow_browse}
					<button class="btn" disabled={n <= 1} onclick={() => go(n - 1)} aria-label="Previous slide"><Icon name="arrow-left" size={16} /></button>
				{/if}
				<span class="tabular small" aria-live="polite">{n} / {total}</span>
				{#if live.allow_browse && !following}
					{#if n < live.max_shown}<button class="btn" onclick={() => go(n + 1)} aria-label="Next slide"><Icon name="arrow-right" size={16} /></button>{/if}
					<button class="btn btn-primary" onclick={follow}><Icon name="skip-forward" size={16} />Back to the teacher’s slide</button>
				{/if}
				<button class="btn btn-ghost" onclick={fullscreen} aria-label={full ? 'Leave full screen' : 'Full screen'}><Icon name={full ? 'minimize' : 'maximize'} size={16} /></button>
			</div>
		</section>
		<p class="small muted m-0">
			{following ? 'Following your teacher’s slides.' : `You’re looking back; your teacher is on slide ${live.slide}.`}
			Your teacher sees which slide you’re on and for how long.
		</p>
	{/if}
</div>

<style>
	.viewer { max-width: 1100px; display: grid; gap: 1rem; }
	.stage { display: grid; gap: 0.75rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: 1rem; padding: 0.75rem; }
	.stage img { width: 100%; height: auto; max-height: 75vh; object-fit: contain; border-radius: 0.5rem; background: #fff; }
	.controls { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 0.75rem; }
	.stage.full { border-radius: 0; border: 0; padding: 0.5rem; background: #000; grid-template-rows: 1fr auto; height: 100vh; }
	.stage.full img { max-height: calc(100vh - 4rem); height: 100%; border-radius: 0; }
	.stage.full .tabular { color: #fff; }
</style>
