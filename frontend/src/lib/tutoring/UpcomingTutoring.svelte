<script lang="ts">
	// What's next in tutoring, on the student's and the teacher's pages: the
	// reminder in the platform (D-84, TS-FR-87). Sessions live now, or opening
	// within 15 minutes, stand out with a Join button; then the week ahead.
	// Shown only when tutoring is on and something is coming.
	import { onMount } from 'svelte';
	import { TutorClient, tutoringConfig } from './client';
	import { canJoin, happeningSoon, type CalendarEntry } from './types';

	const client = new TutorClient();
	let list = $state<CalendarEntry[]>([]);
	let now = $state(Date.now());

	async function load() {
		if (!(await tutoringConfig()).enabled) return;
		const from = new Date(Date.now() - 60 * 60_000).toISOString();
		const to = new Date(Date.now() + 7 * 24 * 60 * 60_000).toISOString();
		list = (await client.get<{ sessions: CalendarEntry[] }>(`/api/calendar?from=${from}&to=${to}`)).sessions;
	}
	onMount(() => {
		load().catch(() => {});
		const tick = setInterval(() => (now = Date.now()), 15_000);
		const reload = setInterval(() => load().catch(() => {}), 120_000);
		return () => {
			clearInterval(tick);
			clearInterval(reload);
		};
	});
	const soon = $derived(happeningSoon(list, now));
	const later = $derived(list.filter((e) => e.status === 'open' && e.scheduled_at && Date.parse(e.scheduled_at) > now && !soon.includes(e)).slice(0, 5));
	const at = (s: string) => new Date(s).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
	function startsIn(e: CalendarEntry): string {
		if (e.status === 'live') return 'live now';
		const m = Math.round((Date.parse(e.scheduled_at!) - now) / 60_000);
		return m > 0 ? `starts in ${m} min` : 'starting';
	}
</script>

{#if soon.length || later.length}
	<section class="vstack gap-2" aria-labelledby="upcoming-h">
		<div class="row"><h2 id="upcoming-h" class="m-0">Tutoring sessions</h2><span class="spacer"></span><a class="btn btn-sm btn-ghost" href="/tutoring/calendar">Calendar</a></div>
		{#each soon as e (e.id)}
			<div class="alert alert-soft alert-info" role="status">
				<span class="flex-1"><strong>{e.title}</strong> · {e.classroom_name} · {startsIn(e)}</span>
				{#if canJoin(e, now)}<a class="btn btn-sm btn-primary" href={'/tutor/' + e.join_code}>{e.role === 'student' ? 'Join' : 'Open'}</a>{:else}<span class="small">opens {new Date(e.opens_at!).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}</span>{/if}
			</div>
		{/each}
		{#if later.length}
			<ul class="card card-border bg-base-100 shadow-sm p-3 m-0 list-none vstack gap-1">
				{#each later as e (e.id)}
					<li class="row gap-2 small"><span class="tabular w-44 shrink-0">{at(e.scheduled_at!)}</span><span class="flex-1 min-w-0 truncate">{e.title} <span class="muted">· {e.classroom_name}</span></span></li>
				{/each}
			</ul>
		{/if}
	</section>
{/if}
