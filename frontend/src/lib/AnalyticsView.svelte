<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Class, question and student analytics (BA §11).
	import { toPlain } from './richtext/render';
	import type { TextFormat } from './types';
	type Class = { joined: number; finished: number; marked: number; mean_pct: number; median_pct: number; pass_rate: number; pass_mark_pct: number; completion_rate: number; invalidation_rate: number; distribution: number[]; pending: number };
	type QRow = { question_id: string; code: string; text: string; format?: TextFormat; type: string; answered: number; shown?: number; pct_correct: number; avg_score: number; max_score: number; option_counts?: Record<string, number>; option_labels?: Record<string, string>; common_wrong?: { answer: string; count: number }[]; discrimination: number | null };
	type SRow = { attempt_id: string; name: string; student_number: string | null; state: string; score: number; max_score: number; pct: number; passed: boolean; complete: boolean; time_taken_sec: number | null; violations: number; extension_sec: number; pauses: number };
	let { cls, questions, students }: { cls: Class; questions: QRow[]; students: SRow[] } = $props();
	let view = $state<'class' | 'questions' | 'students'>('class');
	const maxBin = $derived(Math.max(1, ...cls.distribution));
	const mins = (s: number | null) => (s == null ? '—' : `${Math.floor(s / 60)}m ${s % 60}s`);
</script>

<div class="tabs tabs-border tabs-scroll" role="tablist">
	<button class="tab" role="tab" aria-selected={view === 'class'} class:tab-active={view === 'class'} onclick={() => (view = 'class')}>{tr("Class")}</button>
	<button class="tab" role="tab" aria-selected={view === 'questions'} class:tab-active={view === 'questions'} onclick={() => (view = 'questions')}>{tr("Questions")}</button>
	<button class="tab" role="tab" aria-selected={view === 'students'} class:tab-active={view === 'students'} onclick={() => (view = 'students')}>{tr("Students")}</button>
</div>

{#if view === 'class'}
	{#if cls.pending}<p class="alert alert-soft alert-warning small">{tr("{0} attempt(s) still awaiting LLM or manual marks; figures will update.", [cls.pending])}</p>{/if}
	<div class="auto-grid">
		{#each [['Mean', cls.mean_pct + '%'], ['Median', cls.median_pct + '%'], ['Pass rate', cls.pass_rate + '%'], ['Completion', cls.completion_rate + '%'], ['Invalidated', cls.invalidation_rate + '%'], ['Finished / joined', cls.finished + ' / ' + cls.joined]] as [l, v] (l)}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><div class="muted small">{l}</div><div style="font-size:1.5rem;font-weight:700">{v}</div></div>
		{/each}
	</div>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		<strong>{tr("Score distribution")}</strong> <span class="small muted">{tr("(pass mark {0}%)", [cls.pass_mark_pct])}</span>
		<div class="hist" role="img" aria-label={tr("Score distribution histogram")}>
			{#each cls.distribution as n, i (i)}
				<div class="bin"><div class="bar" class:pass={i * 10 >= cls.pass_mark_pct} style="height:{(n / maxBin) * 100}%" title={`${i * 10}-${i * 10 + 9}%: ${n}`}></div><span class="small">{i * 10}</span></div>
			{/each}
		</div>
	</div>
{:else if view === 'questions'}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
		<table class="table">
			<thead><tr><th>{tr("Question")}</th><th>{tr("% correct")}</th><th>{tr("Avg score")}</th><th>{tr("Answered")}</th><th>{tr("Discrimination")}</th><th>{tr("Most common wrong / options")}</th></tr></thead>
			<tbody>
				{#each questions as q (q.question_id)}
					<tr>
						<td><strong>{q.code}</strong> <span class="small muted">{q.type}</span><br /><span class="small">{toPlain(q.text, q.format).slice(0, 90)}</span></td>
						<td><div class="pbar"><div style="width:{q.pct_correct}%"></div></div>{q.pct_correct}%</td>
						<td>{q.avg_score} / {q.max_score}</td>
						<td>{q.answered}{#if q.shown != null && q.shown < cls.marked}{' '}<span class="small muted block">{tr("of {0} who got it", [q.shown])}</span>{/if}</td>
						<td><span class="tooltip" data-tip={tr("Correct rate of the top 27% minus the bottom 27%")}>{q.discrimination ?? '—'}</span></td>
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
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
		<table class="table">
			<thead><tr><th>{tr("Student")}</th><th>{tr("Score")}</th><th>{tr("Result")}</th><th>{tr("Time")}</th><th>{tr("Violations")}</th><th>{tr("Extra time")}</th><th>{tr("Pauses")}</th></tr></thead>
			<tbody>
				{#each students as s (s.attempt_id)}
					<tr>
						<td>{s.name}<br /><span class="small muted">{s.student_number ?? ''}</span></td>
						<td>{s.score}/{s.max_score} ({s.pct}%)</td>
						<td>{#if s.state === 'invalidated'}<span class="badge badge-soft badge-error">{tr("invalidated")}</span>{:else if !s.complete}<span class="badge badge-soft badge-warning">{tr("marking")}</span>{:else}<span class="badge badge-soft {s.passed ? 'ok' : 'danger'}">{s.passed ? 'pass' : 'fail'}</span>{/if}</td>
						<td>{mins(s.time_taken_sec)}</td><td>{s.violations}</td><td>{s.extension_sec ? tr("{0} min", [Math.round(s.extension_sec / 60)]) : '—'}</td><td>{s.pauses}</td>
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
	.pbar { height: 6px; background: var(--color-base-300); border-radius: 3px; width: 80px; }
	.pbar div { height: 100%; background: var(--primary); border-radius: 3px; }
</style>
