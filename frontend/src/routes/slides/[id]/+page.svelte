<script lang="ts">
	// A student going through a presentation at their own pace (D-76). The
	// next slide loads ahead; arrow keys move; time on each slide is recorded
	// while it's on screen, and the student is told so (PR-FR-34).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { canOpen, slideAlt, type View } from '$lib/presentation/presentation';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { PageWatch } from '$lib/tutorial/watch';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';

	const id = $derived(appPage.params.id ?? '');
	let v = $state<View | null>(null);
	let problem = $state('');
	let busy = $state(false);
	let stage = $state<HTMLElement | null>(null);

	$effect(() => {
		if (!auth.loaded || v || problem) return;
		if (!auth.user) {
			goto(loginUrl('/slides/' + id), { replaceState: true });
			return;
		}
		api.post<View>(`/api/presentations/${id}/open`).then((r) => (v = r), (e) => (problem = e instanceof ApiError ? e.message : 'Could not open the presentation'));
	});

	const src = (n: number) => `/api/presentations/${id}/slides/${n}?v=${v?.presentation.version}`;
	const total = $derived(v?.slides.length ?? 0);
	const slide = $derived(v?.slides[(v?.current ?? 1) - 1]);
	// Load the next slide ahead, so turning to it is instant (PR-NFR-01).
	$effect(() => {
		if (v && v.current < total && canOpen(v, v.current + 1)) new Image().src = src(v.current + 1);
	});

	// ---- time on the slide ----
	let watch: PageWatch | null = null;
	function sendTime(seconds: number, keepalive: boolean) {
		if (!v) return;
		fetch(`/api/presentations/${id}/time`, {
			method: 'POST', keepalive, credentials: 'same-origin',
			headers: { 'X-Requested-With': 'fetch', 'Content-Type': 'application/json' },
			body: JSON.stringify({ slide: v.current, seconds })
		}).catch(() => {});
	}
	onMount(() => {
		watch = new PageWatch({ time: sendTime, leave: () => {}, active: () => !!v });
		watch.start();
		return () => watch?.stop();
	});

	async function go(n: number) {
		if (!v || busy || n === v.current || !canOpen(v, n)) return;
		watch?.flush(); // the time so far belongs to the slide being left
		busy = true;
		try {
			v = await api.post<View>(`/api/presentations/${id}/go`, { to: n });
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Something went wrong; try again', 'error');
		} finally {
			busy = false;
		}
	}
	function key(e: KeyboardEvent) {
		if (!v || (e.target as HTMLElement)?.closest('input, select, textarea')) return;
		if (e.key === 'ArrowRight' || e.key === 'PageDown') go(v.current + 1);
		else if (e.key === 'ArrowLeft' || e.key === 'PageUp') go(v.current - 1);
		else if (e.key === 'Home') go(1);
		else return;
		e.preventDefault();
	}
	let full = $state(false);
	async function fullscreen() {
		if (document.fullscreenElement) await document.exitFullscreen();
		else await stage?.requestFullscreen().catch(() => {});
	}
</script>

<svelte:head><title>{v?.presentation.title ?? 'Presentation'}</title></svelte:head>
<svelte:window onkeydown={key} />
<svelte:document onfullscreenchange={() => (full = !!document.fullscreenElement)} />

<div class="page-container viewer">
	{#if problem}
		<div class="card card-border bg-base-100 shadow-sm p-6 text-center vstack" role="alert">
			<h1 class="m-0">Can’t open this presentation</h1>
			<p class="muted m-0">{problem}</p>
			<a class="btn mx-auto" href="/my">Back to my page</a>
		</div>
	{:else if !v || !slide}
		<Skeleton lines={8} />
	{:else}
		<header class="vstack gap-2">
			<p class="small m-0"><a href="/my">← My page</a></p>
			<div class="flex flex-wrap items-baseline gap-3">
				<h1 class="m-0">{v.presentation.title}</h1>
				{#if v.presentation.allow_download}<a class="btn btn-sm btn-ghost" href={`/api/presentations/${id}/pdf`} download><Icon name="download" size={14} />PDF</a>{/if}
			</div>
			{#if v.presentation.description}<p class="muted m-0">{v.presentation.description}</p>{/if}
		</header>

		<section class="stage" class:full bind:this={stage} aria-label="Slides">
			<img src={src(v.current)} alt={slideAlt(slide, total)} width={slide.width} height={slide.height} />
			<div class="controls">
				<button class="btn" disabled={busy || v.current === 1} onclick={() => go(v!.current - 1)} aria-label="Previous slide"><Icon name="arrow-left" size={16} /><span class="hide-sm">Back</span></button>
				{#if v.presentation.allow_jump}
					<select class="select select-sm w-auto" aria-label="Go to slide" value={v.current} onchange={(e) => go(Number(e.currentTarget.value))}>
						{#each v.slides as s (s.n)}<option value={s.n}>{s.n} / {total}</option>{/each}
					</select>
				{:else}
					<span class="tabular small" aria-live="polite">{v.current} / {total}</span>
				{/if}
				<button class="btn btn-primary" disabled={busy || v.current === total} onclick={() => go(v!.current + 1)} aria-label="Next slide"><span class="hide-sm">Next</span><Icon name="arrow-right" size={16} /></button>
				<button class="btn btn-ghost" onclick={fullscreen} aria-label={full ? 'Leave full screen' : 'Full screen'}><Icon name={full ? 'minimize' : 'maximize'} size={16} /></button>
			</div>
		</section>
		<progress class="progress progress-primary w-full" value={v.reached} max={total} aria-label="Seen {v.reached} of {total} slides"></progress>
		<p class="small muted m-0">
			{#if v.finished}You’ve seen every slide.{:else if !v.presentation.allow_jump}Go through the slides in order.{/if}
			Your teacher sees which slides you view and for how long. Arrow keys move between slides.
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
	@media (max-width: 30rem) { .hide-sm { display: none; } }
</style>
