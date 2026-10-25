<script lang="ts">
	// The teacher's tutoring sessions (TS-FR-01, TS-FR-90, TS-FR-91): create one
	// for a classroom, and every session one page at a time.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import SortHeader from '$lib/ui/SortHeader.svelte';
	import { TutorClient, tutoringConfig } from '$lib/tutoring/client';
	import type { TutoringSession } from '$lib/tutoring/types';
	import MyRecordings from '$lib/tutoring/MyRecordings.svelte';
	import { auth } from '$lib/session.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import ScheduleFields, { type ScheduleValue } from '$lib/tutoring/ScheduleFields.svelte';
	import UpcomingTutoring from '$lib/tutoring/UpcomingTutoring.svelte';
	import { localInput, myZone } from '$lib/tutoring/types';

	const ready = requireRole('teacher');
	const client = new TutorClient();
	let enabled = $state<boolean | null>(null);
	let classrooms = $state<{ id: string; name: string }[]>([]);
	let classroom = $state(Paged.fromUrl(urlState, 'classroom_id'));
	let status = $state(Paged.fromUrl(urlState, 'status'));
	const list = new Paged<TutoringSession>(() => '/api/sessions', 'sessions', {
		url: urlState,
		sort: 'created',
		desc: true,
		filters: () => ({ classroom_id: classroom, status }),
		get: (url) => client.get(url)
	});
	let title = $state('');
	let pick = $state('');
	let manual = $state(false);
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);

	onMount(async () => {
		enabled = (await tutoringConfig()).enabled;
	});
	$effect(() => {
		if (!ready() || !enabled) return;
		list.load();
		api.get<{ classrooms: { id: string; name: string }[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => {
			classrooms = r.classrooms;
			pick ||= classroom || r.classrooms[0]?.id || '';
		});
	});

	// Now, or scheduled for later, once or repeating (D-84).
	let later = $state(false);
	const nextHour = new Date(Date.now() + 60 * 60_000);
	nextHour.setMinutes(0, 0, 0);
	let schedule = $state<ScheduleValue>({
		start: localInput(nextHour, myZone()),
		time_zone: myZone(),
		duration_min: 60,
		open_early_min: 10,
		reminders: [1440, 15],
		repeat: { freq: '', interval: 1, count: 10 },
		ends: 'count'
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		busy = true;
		try {
			if (!later) {
				const s = await client.post<TutoringSession>('/api/sessions', { classroom_id: pick, title, admit_mode: manual ? 'manual' : 'auto' });
				goto('/tutor/' + s.join_code);
				return;
			}
			const r = schedule.repeat;
			const repeat = r.freq ? { freq: r.freq, interval: r.interval || 1, days: r.freq === 'weekly' ? r.days : undefined, ...(schedule.ends === 'count' ? { count: r.count } : { until: r.until }) } : { freq: '' };
			const out = await client.post<{ sessions: TutoringSession[] }>('/api/schedule', {
				classroom_id: pick, title, admit_mode: manual ? 'manual' : 'auto', start: schedule.start, time_zone: schedule.time_zone,
				duration_min: schedule.duration_min, open_early_min: schedule.open_early_min, reminders: schedule.reminders, repeat
			});
			toast(out.sessions.length === 1 ? 'Scheduled. The classroom’s students are reminded by email.' : `${out.sessions.length} sessions scheduled. Students are reminded before each.`);
			title = '';
			list.load();
		} catch (err) {
			if (err instanceof ApiError) errors = Object.keys(err.fields).length ? err.fields : { _: err.message };
			else throw err;
		} finally {
			busy = false;
		}
	}
	const when = (s?: string) => (s ? new Date(s).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '');
	const label = { open: 'not started', live: 'live', ended: 'ended', cancelled: 'cancelled' };
</script>

<div class="page-container vstack">
	<h1>Tutoring</h1>
	{#if enabled === false}
		<p class="alert alert-soft alert-info">Tutoring isn't switched on for this platform.</p>
	{:else if enabled}
		<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={create}>
			<h2 class="m-0 text-base">New session</h2>
			<div class="grid gap-3 sm:grid-cols-2">
				<div><label for="ts-title">Title</label><input id="ts-title" class="input w-full" bind:value={title} required maxlength="200" aria-invalid={!!errors.title} />{#if errors.title}<p class="field-error">{errors.title}</p>{/if}</div>
				<div>
					<label for="ts-class">Classroom</label>
					<select id="ts-class" class="select w-full" bind:value={pick} required>
						{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{:else}<option value="">No classrooms yet</option>{/each}
					</select>
				</div>
			</div>
			<div class="join" role="group" aria-label="When">
				<button type="button" class="btn btn-sm join-item" class:btn-active={!later} aria-pressed={!later} onclick={() => (later = false)}>Start now</button>
				<button type="button" class="btn btn-sm join-item" class:btn-active={later} aria-pressed={later} onclick={() => (later = true)}>Schedule</button>
			</div>
			{#if later}<ScheduleFields bind:value={schedule} {errors} />{/if}
			<label class="row font-normal"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={manual} /> Students wait until I admit them</label>
			<p class="small muted m-0">Students in the classroom join with the session's code or link, on a computer, or from their calendar. They can't share anything until you allow it.</p>
			{#if errors._}<p class="alert alert-soft alert-error small m-0">{errors._}</p>{/if}
			<div class="row gap-2">
				<button class="btn btn-primary" disabled={busy || !pick}>{later ? 'Schedule' : 'Create and open'}</button>
				<a class="btn btn-ghost" href="/tutoring/calendar">Calendar</a>
			</div>
		</form>
		<UpcomingTutoring />


		<div class="row">
			<ListSearch {list} placeholder="Search titles" label="Search sessions" />
			<select class="select select-sm w-auto" bind:value={classroom} onchange={() => list.refilter()} aria-label="Classroom"><option value="">All classrooms</option>{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{/each}</select>
			<select class="select select-sm w-auto" bind:value={status} onchange={() => list.refilter()} aria-label="Status"><option value="">Any status</option><option value="open">Not started</option><option value="live">Live</option><option value="ended">Ended</option><option value="cancelled">Cancelled</option></select>
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader {list} key="title" label="Session" /></th><th>Classroom</th><th>Status</th><th><SortHeader {list} key="scheduled" label="When" /></th><th><span class="sr-only">Open</span></th></tr></thead>
				<tbody>
					{#each list.rows as s (s.id)}
						<tr>
							<td>{s.title}<br /><span class="small muted tabular">{s.join_code}</span></td>
							<td class="small">{s.classroom_name}</td>
							<td><span class="badge badge-soft {s.status === 'live' ? 'danger' : s.status === 'ended' || s.status === 'cancelled' ? '' : 'warn'}">{label[s.status]}</span></td>
							<td class="small tabular">{#if s.scheduled_at}{when(s.scheduled_at)}{#if s.series_id}{' '}<span class="badge badge-soft badge-xs">series</span>{/if}{:else}<span class="muted">created {when(s.created_at)}</span>{/if}</td>
							<td>{#if s.status === 'open' || s.status === 'live'}<a class="btn btn-sm" href={'/tutor/' + s.join_code}>Open</a>{/if}</td>
						</tr>
					{:else}
						<tr><td colspan="5" class="muted">{list.loaded ? (list.error ? list.error : 'No tutoring sessions yet.') : 'Loading…'}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pager {list} label="Tutoring sessions" />
		<MyRecordings meId={auth.user?.id ?? ''} heading="Recordings" />
	{/if}
</div>
