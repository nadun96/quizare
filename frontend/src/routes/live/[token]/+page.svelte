<script lang="ts">
	// Public live leaderboard (V2-08, D-45). No login: the page polls the
	// link's view at the interval the server suggests, pauses while the tab
	// is hidden, and says plainly when the link has been turned off.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import Leaderboard from '$lib/poll/Leaderboard.svelte';
	import type { Rank } from '$lib/poll/types';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';

	type Person = { key: string; rank: number; label: string; score: number; detail?: string; team?: string; color?: number };
	type Team = { rank: number; name: string; color: number; score: number; detail?: string };
	type View = {
		kind: 'poll' | 'session'; title: string; subtitle?: string; status: string; unit: 'points' | 'percent'; views: string[];
		people?: Person[]; teams?: Team[]; joined: number; finished?: number; scoring: boolean; refresh_ms: number; updated_at: number;
	};

	let v = $state<View | null>(null);
	let gone = $state('');
	let stale = $state(false);
	let timer: ReturnType<typeof setTimeout> | null = null;

	async function load() {
		timer = null;
		try {
			v = await api.get<View>('/api/public/live/' + encodeURIComponent(page.params.token ?? ''));
			stale = false;
		} catch (e) {
			if (e instanceof ApiError && (e.status === 404 || e.status === 410)) {
				gone = e.status === 410 ? 'This live link has been turned off or has expired.' : 'This link is not valid.';
				return;
			}
			stale = true; // offline: keep showing the last view and try again
		}
		schedule();
	}
	function schedule() {
		if (timer || gone) return;
		const wait = document.hidden ? 15000 : (v?.refresh_ms ?? 3000);
		timer = setTimeout(load, wait);
	}
	function visibility() {
		if (!document.hidden && timer) {
			clearTimeout(timer);
			timer = null;
			load();
		}
	}
	$effect(() => {
		load();
	});
	onDestroy(() => timer && clearTimeout(timer));

	const people = $derived<Rank[]>((v?.people ?? []).map((p) => ({ rank: p.rank, key: p.key, name: p.team ? `${p.label}` : p.label, score: p.score, correct: 0, answered: 0, color: p.color, detail: [p.team, p.detail].filter(Boolean).join(' · ') })));
	const teams = $derived<Rank[]>((v?.teams ?? []).map((t) => ({ rank: t.rank, key: t.name, name: t.name, score: t.score, correct: 0, answered: 0, color: t.color, detail: t.detail })));
	const both = $derived(people.length > 0 && teams.length > 0);
	const STATUS: Record<string, string> = { open: 'Live', live: 'Live', closed: 'Finished', ended: 'Finished', draft: 'Not started' };
	const isLive = $derived(v?.status === 'open' || v?.status === 'live');
</script>

<svelte:head>
	<title>{v ? v.title + ' · Live leaderboard' : 'Live leaderboard'}</title>
	<meta name="robots" content="noindex, nofollow" />
</svelte:head>
<svelte:document onvisibilitychange={visibility} />

<div class="stage">
	{#if gone}
		<div class="center-box">
			<span class="icon-box" aria-hidden="true"><Icon name="eye-off" size={28} /></span>
			<h1 class="m-0">Leaderboard unavailable</h1>
			<p class="muted m-0">{gone}</p>
		</div>
	{:else if !v}
		<div class="p-8"><Skeleton lines={6} /></div>
	{:else}
		<header class="bar">
			<div class="min-w-0">
				<h1 class="title m-0 truncate">{v.title}</h1>
				{#if v.subtitle}<p class="muted m-0 truncate">{v.subtitle}</p>{/if}
			</div>
			<span class="spacer"></span>
			<span class="badge badge-soft badge-lg gap-1" class:badge-success={isLive} role="status">
				{#if isLive}<span class="status status-success animate-pulse" aria-hidden="true"></span>{/if}{STATUS[v.status] ?? v.status}
			</span>
			<span class="badge badge-soft badge-lg gap-1 tabular" title={v.kind === 'session' ? 'Finished / joined' : 'Taking part'}>
				<Icon name="users" size={16} />{v.finished !== undefined ? `${v.finished}/${v.joined}` : v.joined}
			</span>
			{#if stale}<span class="badge badge-soft badge-warning gap-1"><Icon name="wifi-off" size={14} />Reconnecting</span>{/if}
		</header>

		<main class="main" aria-live="polite" aria-busy={false}>
			{#if !v.scoring}
				<p class="muted center text-xl">This poll isn't scored, so there's no leaderboard.</p>
			{:else if !people.length && !teams.length}
				<div class="center-box">
					<span class="loading loading-dots loading-lg text-primary"></span>
					<p class="muted m-0 text-xl">{v.kind === 'session' ? 'Results appear as students finish.' : 'Waiting for the first answers…'}</p>
				</div>
			{:else}
				<div class="boards" class:two={both}>
					{#if teams.length}
						<section aria-labelledby="h-teams">
							<h2 id="h-teams" class="board-h"><Icon name="users" size={22} />{v.kind === 'poll' ? 'Groups' : 'Teams'}</h2>
							<Leaderboard ranks={teams} big unit={v.unit} limit={50} />
						</section>
					{/if}
					{#if people.length}
						<section aria-labelledby="h-people">
							<h2 id="h-people" class="board-h"><Icon name="trophy" size={22} />{v.kind === 'poll' ? 'Participants' : 'Students'}</h2>
							<Leaderboard ranks={people} big={!both} unit={v.unit} limit={50} />
						</section>
					{/if}
				</div>
			{/if}
		</main>
		<p class="foot small muted">{v.kind === 'session' ? 'Ranked by percentage of marks.' : 'Ranked by points.'} Updated {new Date(v.updated_at).toLocaleTimeString()}.</p>
	{/if}
</div>

<style>
	.stage { min-height: 100dvh; display: flex; flex-direction: column; background: var(--color-base-200); }
	.bar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 0.75rem; padding: 0.9rem 1.5rem; background: var(--color-base-100); border-bottom: 1px solid var(--color-base-300); }
	.title { font-size: clamp(1.3rem, 1rem + 1.5vw, 2rem); font-weight: 750; }
	.main { flex: 1; width: 100%; max-width: 80rem; margin: 0 auto; padding: 1.5rem; }
	.boards { display: grid; gap: 2rem; }
	@media (min-width: 1000px) { .boards.two { grid-template-columns: 1fr 1fr; } }
	.board-h { display: flex; align-items: center; gap: 0.5rem; margin: 0 0 0.75rem; font-size: 1.4rem; }
	.center-box { margin: auto; display: grid; justify-items: center; gap: 0.75rem; padding: 3rem 1.5rem; text-align: center; }
	.icon-box { width: 3.5rem; height: 3.5rem; border-radius: 1rem; display: grid; place-items: center; background: var(--color-base-100); color: var(--color-muted); }
	.foot { text-align: center; margin: 0 0 1rem; }
	@media (max-width: 520px) { .main { padding: 1rem; } .bar { padding: 0.75rem 1rem; } }
</style>
