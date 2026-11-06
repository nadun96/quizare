<script lang="ts">
	// A classroom's or topic's polls, meetings and tutoring sessions (D-147),
	// listed and created beside its quizzes, tutorials and presentations.
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../api';
	import type { Meeting } from '../meet/mesh';
	import { DEFAULT_SETTINGS } from '../poll/scoring';
	import type { Poll } from '../poll/types';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import type { TutoringSession } from '../tutoring/types';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';

	let { classroomId, topicId = null, heading = 'h2' }: { classroomId: string; topicId?: string | null; heading?: 'h2' | 'h3' } = $props();
	const client = new TutorClient();
	let polls = $state<Poll[]>([]);
	let meetings = $state<Meeting[]>([]);
	let sessions = $state<TutoringSession[]>([]);
	let tutoring = $state(false);
	let pollTitle = $state('');
	let meetingTitle = $state('');
	let sessionTitle = $state('');
	let busy = $state(false);

	const where = $derived(topicId ? `topic_id=${topicId}` : `classroom_id=${classroomId}`);
	const body = $derived({ classroom_id: classroomId, topic_id: topicId });
	const when = (s?: string | null) => (s ? new Date(s).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' }) : '');

	// Each is its own service or list: one failing leaves the others showing.
	async function load() {
		const q = `${where}&size=50&sort=created&dir=desc`;
		api.get<{ polls: Poll[] }>(`/api/teacher/polls?${q}`).then((r) => (polls = r.polls), () => {});
		api.get<{ meetings: Meeting[] }>(`/api/teacher/meetings?${q}`).then((r) => (meetings = r.meetings), () => {});
		try {
			tutoring = (await tutoringConfig()).enabled;
			if (tutoring) sessions = (await client.get<{ sessions: TutoringSession[] }>(`/api/sessions?${q}`)).sessions;
		} catch {
			/* without tutoring */
		}
	}
	$effect(() => {
		void where;
		load();
	});

	async function make(e: SubmitEvent, run: () => Promise<string>) {
		e.preventDefault();
		busy = true;
		try {
			goto(await run());
		} catch (err) {
			if (err instanceof ApiError) toast(Object.values(err.fields)[0] ?? err.message, 'error');
			else throw err;
		} finally {
			busy = false;
		}
	}
	// Answered by the classroom's students, by name, so marks can reach the class report.
	const makePoll = (e: SubmitEvent) =>
		make(e, async () => '/t/polls/' + (await api.post<Poll>('/api/teacher/polls', { ...body, title: pollTitle, settings: { ...DEFAULT_SETTINGS, audience: 'classroom', identity: 'identified' } })).id);
	const makeMeeting = (e: SubmitEvent) => make(e, async () => '/m/' + (await api.post<Meeting>('/api/teacher/meetings', { ...body, title: meetingTitle })).code);
	const makeSession = (e: SubmitEvent) => make(e, async () => '/tutor/' + (await client.post<TutoringSession>('/api/sessions', { ...body, title: sessionTitle, admit_mode: 'auto', topic_id: topicId ?? '' })).join_code);
	const scheduleLink = $derived(`/t/tutoring?classroom_id=${classroomId}${topicId ? `&topic_id=${topicId}` : ''}`);
</script>

{#snippet title(text: string, icon: 'chart' | 'video' | 'monitor')}
	<svelte:element this={heading} class="mb-0 flex items-center gap-2"><Icon name={icon} size={20} />{text}</svelte:element>
{/snippet}

{@render title('Polls', 'chart')}
<p class="m-0 small muted">Quick questions the classroom’s students answer by name, with results that update live.</p>
{#if polls.length}
	<div class="card card-border bg-base-100 shadow-sm overflow-x-auto">
		<table class="table table-sm">
			<thead><tr><th>Poll</th><th>Answers</th><th>Status</th><th>Created</th></tr></thead>
			<tbody>
				{#each polls as p (p.id)}
					<tr><td><a href={'/t/polls/' + p.id}>{p.title}</a></td><td class="tabular">{p.participants}</td><td>{p.status}</td><td class="whitespace-nowrap">{when(p.created_at)}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 row" onsubmit={makePoll}>
	<input class="input w-full" style="flex:1" bind:value={pollTitle} placeholder="New poll title" required maxlength="200" aria-label="Poll title" />
	<button class="btn btn-primary" disabled={busy}>Create poll</button>
</form>

{@render title('Meetings', 'video')}
<p class="m-0 small muted">A video call for you and the classroom’s students.</p>
{#if meetings.length}
	<div class="card card-border bg-base-100 shadow-sm overflow-x-auto">
		<table class="table table-sm">
			<thead><tr><th>Meeting</th><th>Status</th><th>Created</th></tr></thead>
			<tbody>
				{#each meetings as m (m.id)}
					<tr><td>{#if m.status === 'open'}<a href={'/m/' + m.code}>{m.title}</a>{:else}{m.title}{/if}</td><td>{m.status}</td><td class="whitespace-nowrap">{when(m.created_at)}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 row" onsubmit={makeMeeting}>
	<input class="input w-full" style="flex:1" bind:value={meetingTitle} placeholder="New meeting title" required maxlength="120" aria-label="Meeting title" />
	<button class="btn btn-primary" disabled={busy}>Open meeting</button>
</form>

{#if tutoring}
	{@render title('Tutoring', 'monitor')}
	<p class="m-0 small muted">A live lesson with a shared screen, board and groups, now or scheduled.</p>
	{#if sessions.length}
		<div class="card card-border bg-base-100 shadow-sm overflow-x-auto">
			<table class="table table-sm">
				<thead><tr><th>Session</th><th>Status</th><th>When</th></tr></thead>
				<tbody>
					{#each sessions as s (s.id)}
						<tr><td>{#if s.status === 'ended' || s.status === 'cancelled'}{s.title}{:else}<a href={'/tutor/' + s.join_code}>{s.title}</a>{/if}</td><td>{s.status}</td><td class="whitespace-nowrap">{when(s.scheduled_at ?? s.created_at)}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 row" onsubmit={makeSession}>
		<input class="input w-full" style="flex:1" bind:value={sessionTitle} placeholder="New tutoring session title" required maxlength="200" aria-label="Tutoring session title" />
		<button class="btn btn-primary" disabled={busy}>Start session</button>
		<a class="btn" href={scheduleLink}>Schedule</a>
	</form>
{/if}
