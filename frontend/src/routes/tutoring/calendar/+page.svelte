<script lang="ts">
	// The tutoring calendar (D-84, TS-FR-86, TS-FR-88): teachers see the
	// sessions they teach or co-teach, students those of their classrooms, by
	// month, week or as a list. Each session can be added to one's own
	// calendar, and a private feed link keeps a calendar app up to date.
	import { ApiError } from '$lib/api';
	import { requireLogin } from '$lib/guard.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { TutorClient, tutoringConfig } from '$lib/tutoring/client';
	import { canJoin, monthDays, sameDay, WEEKDAYS, type CalendarEntry } from '$lib/tutoring/types';

	const ready = requireLogin();
	const client = new TutorClient();
	let enabled = $state<boolean | null>(null);
	let view = $state<'month' | 'week' | 'list'>('month');
	let anchor = $state(new Date()); // a day in the month or week shown
	let list = $state<CalendarEntry[]>([]);
	let picked = $state<CalendarEntry | null>(null);
	let now = $state(Date.now());

	const days = $derived.by(() => {
		if (view === 'month') return monthDays(anchor.getFullYear(), anchor.getMonth());
		const start = new Date(anchor.getFullYear(), anchor.getMonth(), anchor.getDate() - anchor.getDay());
		return Array.from({ length: 7 }, (_, i) => new Date(start.getFullYear(), start.getMonth(), start.getDate() + i));
	});
	const range = $derived.by(() => {
		if (view === 'list') {
			const from = new Date();
			from.setHours(0, 0, 0, 0);
			return [from, new Date(from.getFullYear(), from.getMonth(), from.getDate() + 60)];
		}
		const last = days[days.length - 1];
		return [days[0], new Date(last.getFullYear(), last.getMonth(), last.getDate() + 1)];
	});
	const title = $derived(
		view === 'list' ? 'The next 60 days'
		: view === 'month' ? anchor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
		: `${days[0].toLocaleDateString(undefined, { day: 'numeric', month: 'short' })} – ${days[6].toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}`
	);

	$effect(() => {
		if (!ready()) return;
		tutoringConfig().then((c) => (enabled = c.enabled));
	});
	$effect(() => {
		if (!enabled) return;
		const [from, to] = range;
		client.get<{ sessions: CalendarEntry[] }>(`/api/calendar?from=${from.toISOString()}&to=${to.toISOString()}`).then(
			(r) => (list = r.sessions),
			(e) => toast(e instanceof ApiError ? e.message : 'The calendar didn’t load', 'error')
		);
	});
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 30_000);
		return () => clearInterval(t);
	});

	function move(step: number) {
		const a = anchor;
		anchor = view === 'month' ? new Date(a.getFullYear(), a.getMonth() + step, 1) : new Date(a.getFullYear(), a.getMonth(), a.getDate() + 7 * step);
	}
	const on = (d: Date) => list.filter((e) => e.scheduled_at && sameDay(new Date(e.scheduled_at), d));
	const time = (s: string) => new Date(s).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
	const short = (s: string) => new Date(s).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
	const full = (s: string) => new Date(s).toLocaleString(undefined, { weekday: 'long', day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
	const statusText: Record<CalendarEntry['status'], string> = { open: '', live: 'live', ended: 'ended', cancelled: 'cancelled' };
	const today = new Date();

	async function addToCalendar(e: CalendarEntry) {
		try {
			await client.download(`/api/sessions/${e.id}/calendar.ics`, `tutoring-${e.join_code}.ics`);
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'Could not download', 'error');
		}
	}

	// The private feed (TS-FR-88): the link is shown once, when it's made.
	let feed = $state<{ active: boolean; created_at?: string } | null>(null);
	let feedLink = $state('');
	$effect(() => {
		if (enabled) client.get<{ active: boolean; created_at?: string }>('/api/my/feed').then((f) => (feed = f), () => {});
	});
	async function newFeed() {
		if (feed?.active && !(await confirmDialog({ title: 'Make a new link?', body: 'The old link stops working; calendars using it stop updating.', confirm: 'Make a new link' }))) return;
		const r = await client.post<{ path: string }>('/api/my/feed');
		feedLink = client.base + r.path;
		feed = { active: true, created_at: new Date().toISOString() };
	}
	async function revokeFeed() {
		if (!(await confirmDialog({ title: 'Turn off the calendar link?', body: 'Calendars using it stop updating.', confirm: 'Turn off', danger: true }))) return;
		await client.del('/api/my/feed');
		feed = { active: false };
		feedLink = '';
	}
	async function copy() {
		await navigator.clipboard.writeText(feedLink);
		toast('Link copied');
	}
</script>

<svelte:head><title>Tutoring calendar · Classroom Quiz</title></svelte:head>

<div class="page-container vstack">
	<h1 class="m-0">Tutoring calendar</h1>
	{#if enabled === false}
		<p class="alert alert-soft alert-info">Tutoring isn't switched on for this platform.</p>
	{:else if enabled}
		<div class="row gap-2 flex-wrap">
			<div class="join" role="group" aria-label="View">
				{#each [['month', 'Month'], ['week', 'Week'], ['list', 'List']] as [k, l] (k)}
					<button class="btn btn-sm join-item" class:btn-active={view === k} aria-pressed={view === k} onclick={() => (view = k as typeof view)}>{l}</button>
				{/each}
			</div>
			{#if view !== 'list'}
				<div class="join">
					<button class="btn btn-sm join-item" onclick={() => move(-1)} aria-label="Earlier">‹</button>
					<button class="btn btn-sm join-item" onclick={() => (anchor = new Date())}>Today</button>
					<button class="btn btn-sm join-item" onclick={() => move(1)} aria-label="Later">›</button>
				</div>
			{/if}
			<h2 class="m-0 text-base" aria-live="polite">{title}</h2>
		</div>

		{#if view === 'list'}
			<div class="card card-border bg-base-100 shadow-sm p-2 sm:p-4">
				<ul class="m-0 p-0 list-none">
					{#each list.filter((e) => e.scheduled_at) as e (e.id)}
						<li><button class="entry-row" class:cancelled={e.status === 'cancelled'} onclick={() => (picked = e)}>
							<span class="tabular w-48 shrink-0 text-left">{short(e.scheduled_at!)}</span>
							<span class="flex-1 text-left">{e.title} <span class="muted">· {e.classroom_name}</span></span>
							{#if statusText[e.status]}<span class="badge badge-soft badge-sm">{statusText[e.status]}</span>{/if}
						</button></li>
					{:else}
						<li class="muted p-2">Nothing scheduled in the next 60 days.</li>
					{/each}
				</ul>
			</div>
		{:else}
			<div class="grid-cal" class:week={view === 'week'} role="grid" aria-label={title}>
				{#each WEEKDAYS as d (d)}<div class="dow" role="columnheader">{d}</div>{/each}
				{#each days as d (d.toISOString())}
					<div class="day" class:other={view === 'month' && d.getMonth() !== anchor.getMonth()} class:today={sameDay(d, today)} role="gridcell">
						<div class="num">{d.getDate()}</div>
						{#each on(d) as e (e.id)}
							<button class="chip" class:cancelled={e.status === 'cancelled'} class:live={e.status === 'live'} onclick={() => (picked = e)} title={e.title}>
								<span class="tabular">{time(e.scheduled_at!)}</span> {e.title}
							</button>
						{/each}
					</div>
				{/each}
			</div>
		{/if}

		<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-2" aria-labelledby="feed-h">
			<h2 id="feed-h" class="m-0 text-base">In your own calendar</h2>
			<p class="small muted m-0">A private link your calendar app (Google Calendar, Outlook, Apple Calendar) can subscribe to: it keeps your tutoring sessions up to date. Anyone with the link can see them, so keep it to yourself.</p>
			{#if feedLink}
				<div class="join w-full"><input class="input join-item flex-1" readonly value={feedLink} aria-label="Your calendar link" /><button class="btn join-item" onclick={copy}>Copy</button></div>
				<p class="small m-0">Copy it now: it isn't shown again.</p>
			{/if}
			<div class="row gap-2">
				<button class="btn btn-sm" onclick={newFeed}>{feed?.active ? 'Make a new link' : 'Make a calendar link'}</button>
				{#if feed?.active}<button class="btn btn-sm btn-ghost" onclick={revokeFeed}>Turn off</button>{/if}
				{#if feed?.active && feed.created_at && !feedLink}<span class="small muted">A link is on (since {new Date(feed.created_at).toLocaleDateString()}).</span>{/if}
			</div>
		</section>
	{/if}
</div>

<dialog class="modal" open={!!picked} aria-labelledby="ev-h">
	{#if picked}
		{@const e = picked}
		<div class="modal-box vstack gap-2">
			<h2 id="ev-h" class="m-0 text-lg">{e.title}</h2>
			<p class="m-0">{full(e.scheduled_at ?? e.started_at ?? '')}{e.scheduled_at ? `, ${e.duration_min} minutes` : ''}</p>
			<p class="small muted m-0">{e.classroom_name} · {e.teacher_name}{e.role === 'coteacher' ? ' · you co-teach' : ''} · times in your time zone (planned in {e.time_zone})</p>
			{#if e.status === 'cancelled'}<p class="alert alert-soft alert-warning py-2 m-0">This session was cancelled.</p>
			{:else if e.status === 'ended'}<p class="small m-0">This session has ended.</p>
			{:else if e.role === 'student' && !canJoin(e, now) && e.opens_at}<p class="small m-0">You can join from {time(e.opens_at)}.</p>{/if}
			<div class="modal-action">
				{#if e.scheduled_at && e.status !== 'cancelled'}<button class="btn btn-ghost" onclick={() => addToCalendar(e)}>Add to my calendar</button>{/if}
				<button class="btn" onclick={() => (picked = null)}>Close</button>
				{#if canJoin(e, now)}<a class="btn btn-primary" href={'/tutor/' + e.join_code}>{e.role === 'student' ? 'Join' : 'Open'}</a>{/if}
			</div>
		</div>
		<button class="modal-backdrop" aria-label="Close" onclick={() => (picked = null)}></button>
	{/if}
</dialog>

<style>
	.grid-cal { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); border: 1px solid var(--color-base-300); border-radius: 0.75rem; overflow: hidden; background: var(--color-base-100); }
	.dow { padding: 0.35rem 0.5rem; font-size: 0.75rem; font-weight: 600; color: var(--color-muted, inherit); border-bottom: 1px solid var(--color-base-300); }
	.day { min-height: 6.5rem; padding: 0.3rem; border-top: 1px solid var(--color-base-300); border-left: 1px solid var(--color-base-300); display: flex; flex-direction: column; gap: 0.2rem; min-width: 0; }
	.week .day { min-height: 14rem; }
	.day:nth-child(7n + 1) { border-left: 0; }
	.day.other { background: var(--color-base-200); opacity: 0.7; }
	.num { font-size: 0.8rem; font-weight: 600; }
	.day.today .num { color: var(--color-primary); }
	.chip { text-align: left; font-size: 0.75rem; line-height: 1.2; padding: 0.15rem 0.35rem; border-radius: 0.35rem; background: color-mix(in oklab, var(--color-primary) 14%, transparent); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; cursor: pointer; }
	.chip.live { background: color-mix(in oklab, var(--color-error) 18%, transparent); }
	.chip.cancelled, .entry-row.cancelled { text-decoration: line-through; opacity: 0.6; }
	.entry-row { display: flex; gap: 0.75rem; align-items: center; width: 100%; padding: 0.5rem; border-radius: 0.5rem; cursor: pointer; }
	.entry-row:hover, .chip:hover { filter: brightness(0.95); }
	@media (max-width: 640px) { .day { min-height: 4rem; } .chip { font-size: 0.65rem; } }
</style>
