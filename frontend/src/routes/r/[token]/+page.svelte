<script lang="ts">
	// Public, read-only results page (FR-RS-01/02, BR-13). Rendered from JSON.
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';

	type View = {
		title: string;
		quiz_title: string;
		pass_rate?: { pass_rate: number; pass_mark_pct: number; finished: number; mean_pct: number };
		questions?: { code: string; text: string; pct_correct: number; answered: number }[];
		students?: { label: string; score: number; max_score: number; pct: number; passed: boolean }[];
	};
	let v = $state<View | null>(null);
	let error = $state('');
	$effect(() => {
		api.get<View>('/api/public/results/' + page.params.token)
			.then((x) => (v = x))
			.catch((e) => (error = e instanceof ApiError && e.status === 410 ? 'This link has expired or been turned off.' : 'This link is not valid.'));
	});
</script>

<svelte:head><meta name="robots" content="noindex, nofollow" /></svelte:head>

<div class="container stack" style="max-width:860px">
	{#if error}<div class="card"><h1>Results</h1><p>{error}</p></div>{/if}
	{#if v}
		<h1>{v.title}</h1>
		{#if v.pass_rate}
			<div class="grid">
				<div class="card"><div class="small muted">Pass rate</div><div style="font-size:2rem;font-weight:700">{v.pass_rate.pass_rate}%</div><div class="small muted">pass mark {v.pass_rate.pass_mark_pct}%</div></div>
				<div class="card"><div class="small muted">Average score</div><div style="font-size:2rem;font-weight:700">{v.pass_rate.mean_pct}%</div></div>
				<div class="card"><div class="small muted">Students</div><div style="font-size:2rem;font-weight:700">{v.pass_rate.finished}</div></div>
			</div>
		{/if}
		{#if v.questions}
			<div class="card"><h2 style="margin-top:0">Question-wise correct</h2>
				{#each v.questions as q (q.code)}
					<div class="qrow"><span><strong>{q.code}</strong> {q.text.slice(0, 80)}</span><div class="pbar"><div style="width:{q.pct_correct}%"></div></div><strong>{q.pct_correct}%</strong></div>
				{/each}
			</div>
		{/if}
		{#if v.students}
			<div class="card table-wrap"><h2 style="margin-top:0">Results</h2>
				<table><thead><tr><th>Student</th><th>Score</th><th>Result</th></tr></thead><tbody>
					{#each v.students as s, i (i)}<tr><td>{s.label}</td><td>{s.score}/{s.max_score} ({s.pct}%)</td><td><span class="badge {s.passed ? 'ok' : 'danger'}">{s.passed ? 'pass' : 'fail'}</span></td></tr>{/each}
				</tbody></table>
			</div>
		{/if}
	{/if}
</div>

<style>
	.qrow { display: grid; grid-template-columns: 1fr 120px 3rem; gap: 0.75rem; align-items: center; padding: 0.4rem 0; border-bottom: 1px solid var(--border); }
	.pbar { height: 8px; background: var(--border); border-radius: 4px; }
	.pbar div { height: 100%; background: var(--primary); border-radius: 4px; }
</style>
