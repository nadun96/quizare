<script lang="ts">
	// One student's months in a classroom, beside the class average (D-119).
	import { api } from '../api';
	import TrendChart from '../dashboard/TrendChart.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { fromFirst, monthLabel, points, type StudentTrends, type TrendStudent } from './trends';

	let { classroomId, student, months, onclose }: { classroomId: string; student: TrendStudent; months: number; onclose: () => void } = $props();
	let data = $state<StudentTrends | null>(null);
	let dialog = $state<HTMLDialogElement>();
	$effect(() => {
		dialog?.showModal();
		api.get<StudentTrends>(`/api/teacher/classrooms/${classroomId}/students/${student.user_id}/trends?months=${months}`).then((d) => (data = d), () => onclose());
	});
	const shown = $derived(data ? fromFirst(data.months) : []);
</script>

<dialog bind:this={dialog} class="modal" aria-labelledby="st-h" onclose={onclose}>
	<div class="modal-box max-w-3xl vstack">
		<h2 id="st-h" class="m-0 text-lg">{student.name}{#if student.student_number}{' '}<span class="small muted">{student.student_number}</span>{/if}</h2>
		{#if !data}
			<Skeleton lines={5} />
		{:else}
			{@const pts = points(shown, (m) => m.avg, (m) => m.class_avg, (m) => `${m.quizzes} results`)}
			{#if pts.length}
				<TrendChart title="{student.name}’s average each month" name="Their average" contextName="Class average" rowName="Month" noun="months" points={pts} />
			{:else}
				<p class="m-0 muted small">No quiz results in this span.</p>
			{/if}
			<div class="table-wrap">
				<table class="table table-sm">
					<thead><tr><th>Month</th><th>Results</th><th>Average</th><th>Class average</th><th>Passed</th><th>Tutorials</th><th>Active days</th></tr></thead>
					<tbody>
						{#each [...shown].reverse() as m (m.month)}
							<tr><td>{monthLabel(m.month)}</td><td class="tabular">{m.quizzes}</td><td class="tabular">{m.avg ?? '—'}{m.avg !== null ? '%' : ''}</td><td class="tabular">{m.class_avg ?? '—'}{m.class_avg != null ? '%' : ''}</td><td class="tabular">{m.pass_rate ?? '—'}{m.pass_rate !== null ? '%' : ''}</td><td class="tabular">{m.tutorials}</td><td class="tabular">{m.active}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
		<div class="modal-action"><button class="btn" onclick={() => dialog?.close()}>Close</button></div>
	</div>
	<form method="dialog" class="modal-backdrop"><button>close</button></form>
</dialog>
