<script lang="ts">
	// Presenting live (D-77, PR-FR-13): the current and next slides, a timer,
	// the code to join, and the class's attention. Arrow keys move the slides.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { elapsed, personState, slideAlt, type Counts, type Live, type Person, type Presentation, type Slide } from '$lib/presentation/presentation';
	import QrCode from '$lib/QrCode.svelte';
	import { LiveSocket } from '$lib/socket';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';

	const ready = requireRole('teacher');
	const id = $derived(appPage.params.id ?? '');
	let p = $state<Presentation | null>(null);
	let slides = $state<Slide[]>([]);
	let live = $state<Live | null>(null);
	let people = $state<Person[]>([]);
	let counts = $state<Counts>({ joined: 0, following: 0, looking_back: 0, away: 0 });
	let connected = $state(false);
	let showQr = $state(false);
	let now = $state(Date.now());
	let socket: LiveSocket | null = null;

	$effect(() => {
		if (!ready() || !id || p) return;
		(async () => {
			p = await api.get<Presentation>('/api/teacher/presentations/' + id);
			slides = (await api.get<{ slides: Slide[] }>(`/api/teacher/presentations/${id}/slides`)).slides;
			live = await api.post<Live>(`/api/teacher/presentations/${id}/live`); // starts it, or picks it up
			socket = new LiveSocket('/ws/presentations/' + id);
			socket.onStatus = (c) => (connected = c);
			socket.onMessage = (m) => {
				if (m.type === 'live') live = m.live as Live;
				else if (m.type === 'audience') {
					people = m.people as Person[];
					counts = m.counts as Counts;
				} else if (m.type === 'ended') {
					socket?.close();
					goto('/t/presentations/' + id);
				}
			};
			socket.open();
		})().catch((e) => toast(e instanceof ApiError ? e.message : 'Could not start presenting', 'error'));
	});
	onMount(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => {
			clearInterval(t);
			socket?.close();
		};
	});

	const thumb = (n: number) => `/api/teacher/presentations/${id}/slides/${n}?v=${p?.version}`;
	const total = $derived(slides.length);
	const joinUrl = $derived(live && typeof location !== 'undefined' ? `${location.origin}/s/${live.code}` : '');
	let busy = false;
	async function show(n: number) {
		if (!live || busy || n < 1 || n > total || n === live.slide) return;
		busy = true;
		try {
			live = await api.post<Live>(`/api/teacher/presentations/${id}/live/slide`, { slide: n });
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not change the slide', 'error');
		} finally {
			busy = false;
		}
	}
	async function browse(allow: boolean) {
		live = await api.put<Live>(`/api/teacher/presentations/${id}/live`, { allow_browse: allow });
	}
	async function end() {
		if (!(await confirmDialog({ title: 'End presenting?', body: 'Students’ screens stop following you. What they saw, and for how long, stays in the report.', confirm: 'End' }))) return;
		await api.post(`/api/teacher/presentations/${id}/live/end`);
		socket?.close();
		goto('/t/presentations/' + id);
	}
	function key(e: KeyboardEvent) {
		if (!live || (e.target as HTMLElement)?.closest('input, select, textarea, button')) return;
		if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') show(live.slide + 1);
		else if (e.key === 'ArrowLeft' || e.key === 'PageUp') show(live.slide - 1);
		else return;
		e.preventDefault();
	}
	async function copy() {
		await navigator.clipboard?.writeText(joinUrl).catch(() => {});
		toast('Link copied');
	}
</script>

<svelte:head><title>Presenting: {p?.title ?? ''}</title></svelte:head>
<svelte:window onkeydown={key} />

<div class="presenter">
	{#if !p || !live}
		<Skeleton lines={8} />
	{:else}
		{@const cur = slides[live.slide - 1]}
		<header class="bar">
			<a class="btn btn-sm btn-ghost" href={'/t/presentations/' + id}><Icon name="arrow-left" size={14} />Back</a>
			<h1 class="m-0 truncate text-lg">{p.title}</h1>
			<span class="badge badge-error badge-soft gap-1"><span class="dot" aria-hidden="true"></span>Live</span>
			<span class="tabular small" title="Time since you started">{elapsed(live.started_at, now)}</span>
			{#if !connected}<span class="badge badge-warning badge-sm">Reconnecting…</span>{/if}
			<span class="spacer"></span>
			<span class="small">Code <strong class="code">{live.code}</strong></span>
			<button class="btn btn-sm" onclick={() => (showQr = !showQr)} aria-pressed={showQr}><Icon name="qr" size={14} />QR</button>
			<button class="btn btn-sm" onclick={copy}><Icon name="copy" size={14} />Link</button>
			<a class="btn btn-sm" href={`/t/presentations/${id}/screen`} target="_blank" rel="noopener"><Icon name="monitor" size={14} />Screen view</a>
			<button class="btn btn-sm btn-error btn-outline" onclick={end}>End</button>
		</header>

		<div class="grid-main">
			<section class="current" aria-label="Current slide">
				{#if cur}<img src={thumb(cur.n)} alt={slideAlt(cur, total)} width={cur.width} height={cur.height} />{/if}
				<div class="controls">
					<button class="btn" disabled={live.slide === 1} onclick={() => show(live!.slide - 1)}><Icon name="arrow-left" size={16} />Back</button>
					<span class="tabular font-semibold" aria-live="polite">{live.slide} / {total}</span>
					<button class="btn btn-primary" disabled={live.slide === total} onclick={() => show(live!.slide + 1)}>Next<Icon name="arrow-right" size={16} /></button>
				</div>
			</section>

			<aside class="side">
				{#if showQr}
					<div class="card card-border bg-base-100 p-3 text-center vstack gap-1">
						<QrCode text={joinUrl} size={220} />
						<p class="small m-0">{joinUrl}</p>
					</div>
				{/if}
				<div class="card card-border bg-base-100 p-3 vstack gap-2">
					<p class="small muted m-0">Next</p>
					{#if live.slide < total}
						{@const nx = slides[live.slide]}
						<img class="next" src={thumb(nx.n)} alt={slideAlt(nx, total)} width={nx.width} height={nx.height} loading="lazy" />
					{:else}<p class="small m-0">That’s the last slide.</p>{/if}
				</div>
				<div class="card card-border bg-base-100 p-3 vstack gap-2">
					<div class="counts" role="status">
						<span><strong class="tabular">{counts.joined}</strong> joined</span>
						<span class="ok"><strong class="tabular">{counts.following}</strong> following</span>
						<span><strong class="tabular">{counts.looking_back}</strong> looking back</span>
						<span class="warn"><strong class="tabular">{counts.away}</strong> away</span>
					</div>
					<label class="small flex items-center gap-2"><input type="checkbox" class="toggle toggle-sm toggle-primary" checked={live.allow_browse} onchange={(e) => browse(e.currentTarget.checked)} />Students may look back on their own</label>
					<ul class="people" aria-label="Students">
						{#each people as person (person.user_id)}
							<li class:away={!person.visible} class:off={person.visible && !person.following}><span class="name truncate">{person.name}</span><span class="small muted">{personState(person, live.slide)}</span></li>
						{:else}<li class="small muted">Nobody has joined yet. Share the code, link or QR code.</li>{/each}
					</ul>
				</div>
			</aside>
		</div>
	{/if}
</div>

<style>
	.presenter { padding: 0.75rem 1rem 1rem; display: grid; gap: 0.75rem; max-width: 1600px; margin: 0 auto; }
	.bar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.6rem; }
	.code { font-family: ui-monospace, monospace; letter-spacing: 0.1em; font-size: 1.05rem; }
	.dot { width: 0.5rem; height: 0.5rem; border-radius: 999px; background: currentColor; }
	.grid-main { display: grid; grid-template-columns: minmax(0, 1fr) 20rem; gap: 1rem; align-items: start; }
	@media (max-width: 900px) { .grid-main { grid-template-columns: minmax(0, 1fr); } }
	.current { display: grid; gap: 0.75rem; }
	.current img { width: 100%; height: auto; max-height: calc(100vh - 11rem); object-fit: contain; background: #fff; border-radius: 0.75rem; border: 1px solid var(--color-base-300); }
	.controls { display: flex; align-items: center; justify-content: center; gap: 1rem; }
	.side { display: grid; gap: 0.75rem; }
	.next { width: 100%; height: auto; border-radius: 0.5rem; border: 1px solid var(--color-base-300); background: #fff; }
	.counts { display: grid; grid-template-columns: 1fr 1fr; gap: 0.25rem 0.75rem; font-size: 0.9rem; }
	.counts .ok strong { color: var(--color-success); }
	.counts .warn strong { color: var(--color-warning); }
	.people { list-style: none; margin: 0; padding: 0; max-height: 40vh; overflow-y: auto; display: grid; }
	.people li { display: flex; justify-content: space-between; gap: 0.5rem; padding: 0.3rem 0.4rem; border-left: 3px solid var(--color-success); }
	.people li.off { border-left-color: var(--color-info); }
	.people li.away { border-left-color: var(--color-warning); }
	.name { min-width: 0; }
</style>
