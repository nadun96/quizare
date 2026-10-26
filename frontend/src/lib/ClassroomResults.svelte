<script lang="ts">
	// A classroom's results (D-87, TS-FR-82): each active student's best mark
	// on each quiz run in the classroom, their average, and, when the teacher
	// counts it, their attendance at tutoring sessions over the last year.
	// The CSV is made here, so it carries both.
	import { api, ApiError } from './api';
	import { TutorClient, tutoringConfig } from './tutoring/client';
	import { loadRoster } from './tutoring/ClassroomAttendance.svelte';
	import { attendanceRows, saveFile, toCSV, type AttendanceRow, type ClassroomReport } from './tutoring/types';

	type Results = {
		quizzes: { id: string; title: string }[];
		students: { user_id: string; name: string; student_number?: string | null; scores: Record<string, number>; average?: number }[];
	};
	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	let res = $state<Results | null>(null);
	let attendance = $state<Map<string, AttendanceRow> | null>(null);
	let error = $state('');

	$effect(() => {
		api.get<Results>(`/api/teacher/classrooms/${classroomId}/results`).then((r) => (res = r), (e) => (error = e instanceof ApiError ? e.message : 'Results didn’t load'));
		// Attendance, if tutoring is on and the teacher counts it.
		tutoringConfig().then(async (c) => {
			if (!c.enabled) return;
			const to = new Date(Date.now() + 864e5);
			const from = new Date(to.getTime() - 365 * 864e5);
			try {
				const rep = await new TutorClient().get<ClassroomReport>(`/api/classrooms/${classroomId}/attendance?from=${from.toISOString()}&to=${to.toISOString()}`);
				if (rep.counts_in_results) attendance = new Map(attendanceRows(rep, await loadRoster(classroomId)).map((r) => [r.user_id, r]));
			} catch {
				/* the results still show without it */
			}
		});
	});
	const pct = (v: number | undefined) => (v == null ? '–' : `${Math.round(v * 10) / 10}%`);
	function download() {
		if (!res) return;
		const r = res;
		saveFile(`results-${classroomName}.csv`, toCSV([
			['name', 'student_id', ...r.quizzes.map((q) => q.title + ' (%)'), 'average (%)', ...(attendance ? ['tutoring attended', 'tutoring sessions', 'attendance (%)'] : [])],
			...r.students.map((s) => {
				const a = attendance?.get(s.user_id);
				return [s.name, s.student_number ?? '', ...r.quizzes.map((q) => s.scores[q.id] ?? ''), s.average ?? '', ...(attendance ? [a?.attended ?? 0, a?.of ?? 0, a?.rate ?? ''] : [])];
			})
		]));
	}
</script>

<section class="vstack gap-3" aria-label="Results">
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>{/if}
	{#if res}
		<div class="row gap-2">
			<p class="small muted m-0 flex-1">Each student’s best attempt on each quiz run in this classroom{attendance ? ', and their attendance at tutoring sessions over the last year' : ''}. Averages are over the quizzes each student took.</p>
			<button class="btn btn-sm" onclick={download} disabled={!res.students.length}>Download CSV</button>
		</div>
		<div class="table-wrap">
			<table class="table table-sm">
				<thead>
					<tr>
						<th>Student</th>
						{#each res.quizzes as q (q.id)}<th class="text-right">{q.title}</th>{/each}
						<th class="text-right">Average</th>
						{#if attendance}<th class="text-right">Attendance</th>{/if}
					</tr>
				</thead>
				<tbody>
					{#each res.students as s (s.user_id)}
						{@const a = attendance?.get(s.user_id)}
						<tr>
							<td>{s.name}{#if s.student_number}<span class="small muted">{' · '}{s.student_number}</span>{/if}</td>
							{#each res.quizzes as q (q.id)}<td class="tabular text-right">{pct(s.scores[q.id])}</td>{/each}
							<td class="tabular text-right font-semibold">{pct(s.average)}</td>
							{#if attendance}<td class="tabular text-right">{a?.rate == null ? '–' : `${a.rate}% (${a.attended}/${a.of})`}</td>{/if}
						</tr>
					{:else}
						<tr><td colspan={res.quizzes.length + 3} class="muted">No students yet.</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if !res.quizzes.length}<p class="small muted m-0">No quiz has been run in this classroom yet.</p>{/if}
	{:else if !error}
		<p class="muted">Loading…</p>
	{/if}
</section>
