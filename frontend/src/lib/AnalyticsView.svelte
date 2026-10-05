<script lang="ts">
	// Class, question and student analytics (BA §11).
	import { toPlain } from './richtext/render';
	import type { TextFormat } from './types';
	type Class = { joined: number; finished: number; marked: number; mean_pct: number; median_pct: number; pass_rate: number; pass_mark_pct: number; completion_rate: number; invalidation_rate: number; distribution: number[]; pending: number };
	type QRow = { question_id: string; code: string; text: string; format?: TextFormat; type: string; answered: number; pct_correct: number; avg_score: number; max_score: number; option_counts?: Record<string, number>; option_labels?: Record<string, string>; common_wrong?: { answer: string; count: number }[]; discrimination: number | null };
	type SRow = { attempt_id: string; name: string; student_number: string | null; state: string; score: number; max_score: number; pct: number; passed: boolean; complete: boolean; time_taken_sec: number | null; violations: number; extension_sec: number; pauses: number };
	let { cls, questions, students }: { cls: Class; questions: QRow[]; students: SRow[] } = $props();
	let view = $state<'class' | 'questions' | 'students'>('class');
	const maxBin = $derived(Math.max(1, ...cls.distribution));
	const mins = (s: number | null) => (s == null ? '—' : `${Math.floor(s / 60)}m ${s % 60}s`);
</script>

<div class="tabs">
	<button class:active={view === 'class'} onclick={() => (view = 'class')}>Class</button>
	<button class:active={view === 'questions'} onclick={() => (view = 'questions')}>Questions</button>
	<button class:active={view === 'students'} onclick={() => (view = 'students')}>Students</button>
</div>

{#if view === 'class'}
	{#if cls.pending}<p class="alert small">{cls.pending} attempt(s) still awaiting LLM or manual marks; figures will update.</p>{/if}
	<div class="grid">
		{#each [['Mean', cls.mean_pct + '%'], ['Median', cls.median_pct + '%'], ['Pass rate', cls.pass_rate + '%'], ['Completion', cls.completion_rate + '%'], ['Invalidated', cls.invalidation_rate + '%'], ['Finished / joined', cls.finished + ' / ' + cls.joined]] as [l, v] (l)}
			<div class="card"><div class="muted small">{l}</div><div style="font-size:1.5rem;font-weight:700">{v}</div></div>
		{/each}
	</div>
	<div class="card">
		<strong>Score distribution</strong> <span class="small muted">(pass mark {cls.pass_mark_pct}%)</span>
		<div class="hist" role="img" aria-label="Score distribution histogram">
			{#each cls.distribution as n, i (i)}
				<div class="bin"><div class="bar" class:pass={i * 10 >= cls.pass_mark_pct} style="height:{(n / maxBin) * 100}%" title={`${i * 10}-${i * 10 + 9}%: ${n}`}></div><span class="small">{i * 10}</span></div>
			{/each}
		</div>
	</div>
{:else if view === 'questions'}
	<div class="card table-wrap">
		<table>
			<thead><tr><th>Question</th><th>% correct</th><th>Avg score</th><th>Answered</th><th>Discrimination</th><th>Most common wrong / options</th></tr></thead>
			<tbody>
				{#each questions as q (q.question_id)}
					<tr>
						<td><strong>{q.code}</strong> <span class="small muted">{q.type}</span><br /><span class="small">{toPlain(q.text, q.format).slice(0, 90)}</span></td>
						<td><div class="pbar"><div style="width:{q.pct_correct}%"></div></div>{q.pct_correct}%</td>
						<td>{q.avg_score} / {q.max_score}</td>
						<td>{q.answered}</td>
						<td title="Correct rate of the top 27% minus the bottom 27%">{q.discrimination ?? '—'}</td>
						<td class="small">
							{#if q.option_counts}{Object.entries(q.option_counts).map(([k, n]) => `${q.option_labels?.[k]}: ${n}`).join(' · ')}<br />{/if}
							{(q.common_wrong ?? []).map((w) => `"${w.answer}" ×${w.count}`).join(', ')}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{:else}
	<div class="card table-wrap">
		<table>
			<thead><tr><th>Student</th><th>Score</th><th>Result</th><th>Time</th><th>Violations</th><th>Extra time</th><th>Pauses</th></tr></thead>
			<tbody>
				{#each students as s (s.attempt_id)}
					<tr>
						<td>{s.name}<br /><span class="small muted">{s.student_number ?? ''}</span></td>
						<td>{s.score}/{s.max_score} ({s.pct}%)</td>
						<td>{#if s.state === 'invalidated'}<span class="badge danger">invalidated</span>{:else if !s.complete}<span class="badge warn">marking</span>{:else}<span class="badge {s.passed ? 'ok' : 'danger'}">{s.passed ? 'pass' : 'fail'}</span>{/if}</td>
						<td>{mins(s.time_taken_sec)}</td><td>{s.violations}</td><td>{s.extension_sec ? Math.round(s.extension_sec / 60) + ' min' : '—'}</td><td>{s.pauses}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<style>
	.hist { display: flex; align-items: flex-end; gap: 4px; height: 160px; margin-top: 0.75rem; }
	.bin { flex: 1; display: flex; flex-direction: column; align-items: center; height: 100%; justify-content: flex-end; }
	.bar { width: 100%; background: var(--danger); opacity: 0.75; border-radius: 4px 4px 0 0; min-height: 2px; }
	.bar.pass { background: var(--ok); }
	.pbar { height: 6px; background: var(--border); border-radius: 3px; width: 80px; }
	.pbar div { height: 100%; background: var(--primary); border-radius: 3px; }
</style>
