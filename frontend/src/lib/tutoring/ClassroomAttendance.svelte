<script lang="ts" module>
	import { api } from '../api';
	import type { RosterEntry } from './types';
	/** The classroom's active students, every page of them. */
	export async function loadRoster(classroomId: string): Promise<RosterEntry[]> {
		const out: RosterEntry[] = [];
		for (let p = 1; p <= 50; p++) {
			const r = await api.get<{ enrolments: RosterEntry[]; total: number }>(`/api/teacher/classrooms/${classroomId}/enrolments?status=active&size=100&page=${p}`);
			out.push(...r.enrolments);
			if (out.length >= r.total || !r.enrolments.length) break;
		}
		return out;
	}
</script>

<script lang="ts">
	// A classroom's attendance at its tutoring sessions (D-87, TS-FR-81,
	// TS-FR-83): each student's sessions attended, rate and time present
	// between two dates, and each session; downloads as CSV. The teacher
	// also decides whether attendance counts in the classroom's results
	// (TS-FR-82).
	import { ApiError } from '../api';
	import { toast } from '../ui/toast.svelte';
	import { TutorClient } from './client';
	import { attendanceRows, saveFile, toCSV, type ClassroomReport } from './types';

	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	const client = new TutorClient();
	const day = (d: Date) => d.toISOString().slice(0, 10);
	let from = $state(day(new Date(Date.now() - 90 * 864e5)));
	let to = $state(day(new Date()));
	let report = $state<ClassroomReport | null>(null);
	let roster = $state<RosterEntry[]>([]);
	let error = $state('');
	const rows = $derived(report ? attendanceRows(report, roster) : []);

	async function load() {
		error = '';
		try {
			const start = new Date(from + 'T00:00:00');
			const end = new Date(to + 'T00:00:00');
			end.setDate(end.getDate() + 1); // the whole last day
			[report, roster] = await Promise.all([
				client.get<ClassroomReport>(`/api/classrooms/${classroomId}/attendance?from=${start.toISOString()}&to=${end.toISOString()}`),
				loadRoster(classroomId)
			]);
		} catch (e) {
			error = e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : 'The report didn’t load';
		}
	}
	$effect(() => {
		load();
	});
	async function setCounts(on: boolean) {
		try {
			await client.put(`/api/classrooms/${classroomId}/attendance/settings`, { counts_in_results: on });
			if (report) report.counts_in_results = on;
			toast(on ? 'Attendance now shows in the classroom’s results' : 'Attendance left out of the results');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	function downloadStudents() {
		saveFile(`attendance-${classroomName}-${from}-to-${to}.csv`, toCSV([
			['name', 'student_id', 'sessions_attended', 'sessions', 'rate_percent', 'minutes'],
			...rows.map((r) => [r.name, r.number, r.attended, r.of, r.rate ?? '', r.minutes])
		]));
	}
	function downloadSessions() {
		if (!report) return;
		const r = report;
		const attendedBy = new Map(r.students.map((s) => [s.user_id, new Set(s.attended)]));
		saveFile(`attendance-by-session-${classroomName}-${from}-to-${to}.csv`, toCSV([
			['name', 'student_id', ...r.sessions.map((s) => `${s.title} (${s.started_at.slice(0, 10)})`)],
			...rows.map((row) => [row.name, row.number, ...r.sessions.map((s) => (attendedBy.get(row.user_id)?.has(s.id) ? 'present' : 'absent'))])
		]));
	}
	const mins = (s: number) => Math.round(s / 60);
</script>

<section class="vstack gap-3" aria-label="Attendance">
	<div class="row gap-2 flex-wrap items-end">
		<label class="small">From <input class="input input-sm" type="date" bind:value={from} max={to} /></label>
		<label class="small">To <input class="input input-sm" type="date" bind:value={to} min={from} /></label>
		<button class="btn btn-sm" onclick={load}>Show</button>
		<span class="spacer"></span>
		{#if report}
			<button class="btn btn-sm" onclick={downloadStudents} disabled={!rows.length}>Download CSV</button>
			<button class="btn btn-sm btn-ghost" onclick={downloadSessions} disabled={!report.sessions.length}>By session (CSV)</button>
		{/if}
	</div>
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>{/if}
	{#if report}
		<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={report.counts_in_results} onchange={(e) => setCounts(e.currentTarget.checked)} /> Count attendance in the classroom’s results</label>
		<p class="small muted m-0">{report.sessions.length} {report.sessions.length === 1 ? 'session' : 'sessions'} from {from} to {to}. A student attended a session when they were connected while it was live.</p>
		<div class="table-wrap">
			<table class="table table-sm">
				<thead><tr><th>Student</th><th>ID</th><th class="text-right">Attended</th><th class="text-right">Rate</th><th class="text-right">Minutes</th></tr></thead>
				<tbody>
					{#each rows as r (r.user_id)}
						<tr><td>{r.name}</td><td class="small tabular">{r.number}</td><td class="tabular text-right">{r.attended} / {r.of}</td><td class="tabular text-right" class:text-warning={r.rate !== null && r.rate < 50}>{r.rate === null ? '–' : r.rate + '%'}</td><td class="tabular text-right">{r.minutes}</td></tr>
					{:else}
						<tr><td colspan="5" class="muted">No students yet.</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if report.sessions.length}
			<details class="small">
				<summary>Sessions</summary>
				<ul class="m-0 mt-2 ps-4">
					{#each report.sessions as s (s.id)}
						<li>{new Date(s.started_at).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })} · {s.title} · {mins(s.seconds)} min · {report.students.filter((x) => x.attended.includes(s.id)).length} present{#if !s.ended_at}{' '}<span class="badge badge-soft badge-xs">live</span>{/if}</li>
					{/each}
				</ul>
			</details>
		{/if}
		<p class="small muted m-0">Attendance is kept for a year after each session.</p>
	{:else if !error}
		<p class="muted">Loading…</p>
	{/if}
</section>
