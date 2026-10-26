<script lang="ts">
	// Polls inside a tutoring session, for the teachers (D-86, TS-FR-68): run
	// one of their polls without leaving the session, pace it, see the
	// results, and show results and the leaderboard to everyone. The polls
	// are the platform's (D-40), driven through its own API; the session
	// only records which one is on, so the students' pages show it.
	import { api, ApiError } from '../api';
	import PollResults from '../poll/PollResults.svelte';
	import Leaderboard from '../poll/Leaderboard.svelte';
	import type { Poll, PollQuestion, PollResult, PollSettings, Rank } from '../poll/types';
	import { toast } from '../ui/toast.svelte';
	import type { TutorClient } from './client';
	import type { TutoringView } from './types';

	let { client, view }: { client: TutorClient; view: TutoringView } = $props();
	const s = $derived(view.session);
	type Results = { poll: Poll; participants: number; results: Record<string, PollResult>; leaderboard?: Rank[] };

	let polls = $state<Poll[]>([]);
	let questions = $state<PollQuestion[]>([]);
	let res = $state<Results | null>(null);
	const p = $derived(res?.poll);

	async function act(f: () => Promise<unknown>) {
		try {
			await f();
			await refresh();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	// The teacher's polls, until one is shown.
	$effect(() => {
		if (s.poll_id) return;
		api.get<{ polls: Poll[] }>('/api/teacher/polls?size=50&sort=created&desc=1').then((r) => (polls = r.polls.filter((x) => x.status !== 'closed')), () => {});
	});
	// The shown poll: its questions once, its results every few seconds.
	async function refresh() {
		if (!s.poll_id) return;
		res = await api.get<Results>(`/api/teacher/polls/${s.poll_id}/results`);
	}
	$effect(() => {
		const id = s.poll_id;
		res = null;
		if (!id) return;
		api.get<Poll>(`/api/teacher/polls/${id}`).then((x) => (questions = x.questions ?? []), () => {});
		refresh().catch(() => {});
		const t = setInterval(() => refresh().catch(() => {}), 3000);
		return () => clearInterval(t);
	});

	async function run(poll: Poll) {
		await act(async () => {
			if (poll.status === 'draft') await api.post(`/api/teacher/polls/${poll.id}/status`, { status: 'open' });
			await client.put(`/api/sessions/${s.id}/poll`, { poll_id: poll.id, code: poll.join_code, title: poll.title });
		});
	}
	const stop = () => act(() => client.put(`/api/sessions/${s.id}/poll`, {}));
	const setStatus = (status: 'open' | 'closed') => act(() => api.post(`/api/teacher/polls/${s.poll_id}/status`, { status }));
	const present = (index: number, revealed: boolean) => act(() => api.post(`/api/teacher/polls/${s.poll_id}/present`, { index, revealed, answers_revealed: p?.answers_revealed ?? false }));
	// Settings are replaced whole: send the poll's own with one change.
	function settingsOf(x: Poll): PollSettings {
		const { identity, audience, pacing, show_results, allow_edit, scoring, speed_bonus, leaderboard, show_answers, names, groups, group_acceptance, group_calc } = x;
		return { identity, audience, pacing, show_results, allow_edit, scoring, speed_bonus, leaderboard, show_answers, names, groups, group_acceptance, group_calc };
	}
	const change = (patch: Partial<PollSettings>) => p && act(() => api.put(`/api/teacher/polls/${s.poll_id}`, { settings: { ...settingsOf(p), ...patch } }));
	const current = $derived(p && p.pacing === 'presenter' ? questions[p.current_index] : undefined);
	const shownQuestions = $derived(current ? [current] : questions);
</script>

<section class="vstack gap-3" aria-label="Polls">
	{#if !s.poll_id}
		<p class="small muted m-0">Run one of your polls here: students answer beside the broadcast, and you see the results as they come in.</p>
		{#each polls as x (x.id)}
			<div class="row gap-2">
				<span class="flex-1 min-w-0"><span class="block truncate">{x.title}</span><span class="small muted">{x.status === 'open' ? 'open' : 'draft'} · {x.pacing === 'presenter' ? 'you pace it' : 'students’ own pace'}{x.scoring ? ' · scored' : ''}</span></span>
				<button class="btn btn-sm" onclick={() => run(x)}>Run</button>
			</div>
		{:else}
			<p class="small m-0">No polls yet. <a href="/t/polls" target="_blank" rel="noopener">Make one</a>, then come back.</p>
		{/each}
	{:else}
		<div class="row gap-2 flex-wrap">
			<strong class="flex-1 min-w-0 truncate">{s.poll_title}</strong>
			{#if p}<span class="badge badge-soft">{p.status}</span><span class="small muted tabular">{res?.participants ?? 0} answering</span>{/if}
		</div>
		{#if p}
			<div class="row gap-2 flex-wrap">
				{#if p.status === 'open'}<button class="btn btn-sm" onclick={() => setStatus('closed')}>Close the poll</button>{:else}<button class="btn btn-sm" onclick={() => setStatus('open')}>Open it</button>{/if}
				<button class="btn btn-sm btn-ghost" onclick={stop}>Stop showing</button>
				<a class="btn btn-sm btn-ghost" href={`/t/polls/${p.id}/present`} target="_blank" rel="noopener">Presenter view</a>
			</div>
			{#if p.pacing === 'presenter' && questions.length}
				<div class="row gap-2">
					<button class="btn btn-sm" disabled={p.current_index === 0} onclick={() => present(p.current_index - 1, false)} aria-label="Previous question">‹</button>
					<span class="small tabular">Question {p.current_index + 1} of {questions.length}</span>
					<button class="btn btn-sm" disabled={p.current_index >= questions.length - 1} onclick={() => present(p.current_index + 1, false)} aria-label="Next question">›</button>
					<button class="btn btn-sm" class:btn-primary={p.revealed} onclick={() => present(p.current_index, !p.revealed)}>{p.revealed ? 'Hide results' : 'Show results to everyone'}</button>
				</div>
			{:else}
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={p.show_results === 'live'} onchange={(e) => change({ show_results: e.currentTarget.checked ? 'live' : 'after_answer' })} /> Everyone sees the results as they come in</label>
			{/if}
			{#if p.scoring}
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={p.leaderboard === 'everyone'} onchange={(e) => change({ leaderboard: e.currentTarget.checked ? 'everyone' : 'presenter' })} /> Show the leaderboard to everyone</label>
			{/if}
			{#each shownQuestions as q (q.id)}
				<div class="vstack gap-1">
					<p class="small font-semibold m-0">{q.text}</p>
					{#if res?.results[q.id]}<PollResults question={q} result={res.results[q.id]} />{:else}<p class="small muted m-0">No answers yet.</p>{/if}
				</div>
			{/each}
			{#if p.scoring && res?.leaderboard?.length}
				<div class="vstack gap-1"><p class="small font-semibold m-0">Leaderboard</p><Leaderboard ranks={res.leaderboard} teacher limit={5} /></div>
			{/if}
		{:else}
			<p class="small muted m-0">Loading…</p>
		{/if}
	{/if}
</section>
