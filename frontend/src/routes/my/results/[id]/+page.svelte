<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { describeKey, describeResponse } from '$lib/answerText';
	import type { Key, Resource, Response, StudentQuestion } from '$lib/types';

	type Q = { question: StudentQuestion; response?: Response; correct_answer?: Key; score: number | null; max_score: number; status: string; correct: boolean | null; feedback?: string; ai_feedback?: string; ai_marked: boolean; feedback_resources?: Resource[] };
	type Result = { session_title: string; score: number; max_score: number; pct: number; pass_mark_pct: number; passed: boolean; complete: boolean; state: string; questions: Q[] };
	let res = $state<Result | null>(null);
	let error = $state('');

	$effect(() => {
		api.get<Result>('/api/my/attempts/' + page.params.id + '/result')
			.then((r) => (res = r))
			.catch((e) => (error = e instanceof ApiError ? e.message : 'Could not load results'));
	});
</script>

<div class="container stack" style="max-width:800px">
	{#if error}<p class="alert">{error}</p>{/if}
	{#if res}
		<h1>{res.session_title}</h1>
		<div class="card row">
			<div><div class="muted small">Score</div><strong style="font-size:1.6rem">{res.score} / {res.max_score}</strong></div>
			<div><div class="muted small">Percent</div><strong style="font-size:1.6rem">{res.pct}%</strong></div>
			<span class="spacer"></span>
			{#if res.state === 'invalidated'}<span class="badge danger">Attempt invalidated</span>
			{:else if !res.complete}<span class="badge warn">Marking in progress</span>
			{:else}<span class="badge {res.passed ? 'ok' : 'danger'}">{res.passed ? 'Passed' : 'Not passed'} (pass mark {res.pass_mark_pct}%)</span>{/if}
		</div>
		{#each res.questions as q, i (q.question.id)}
			<div class="card stack">
				<div class="row">
					<strong>Question {i + 1}</strong>
					<span class="spacer"></span>
					{#if q.ai_marked}<span class="badge" title="Marked by AI; your teacher can review it">AI-marked</span>{/if}
					{#if q.score != null}
						<span class="badge {q.correct ? 'ok' : q.score > 0 ? 'warn' : 'danger'}">{q.score} / {q.max_score}</span>
					{:else}<span class="badge warn">Being marked</span>{/if}
				</div>
				<p style="white-space:pre-wrap">{q.question.text}</p>
				{#if q.response !== undefined}<p><span class="muted small">Your answer</span><br />{describeResponse(q.question.type, q.question.body, q.response)}</p>{/if}
				{#if q.correct_answer}<p><span class="muted small">{q.question.type === 'ESSAY' ? 'Model answer' : 'Correct answer'}</span><br />{describeKey(q.question.type, q.question.body, q.correct_answer)}</p>{/if}
				{#if q.feedback}<p class="alert ok" style="white-space:pre-wrap">{q.feedback}</p>{/if}
				{#if q.ai_feedback}<p class="alert ok" style="white-space:pre-wrap"><strong>Feedback:</strong> {q.ai_feedback}</p>{/if}
				{#each q.feedback_resources ?? [] as r (r.id)}<img src={r.url} alt={r.alt_text} style="max-width:100%" referrerpolicy="no-referrer" />{/each}
			</div>
		{/each}
	{/if}
</div>
