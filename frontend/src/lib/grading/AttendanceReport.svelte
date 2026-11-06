<script lang="ts">
	// A classroom's attendance (D-122): its quiz sessions, meetings and, with
	// tutoring on, tutoring sessions between two dates; each student's count
	// by kind and in all, and session by session. Downloads as a summary or a
	// session-by-session CSV, or prints.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import { saveFile, type ClassroomReport } from '../tutoring/types';
	import { attendanceDetailCSV, attendanceScopes, attendanceSummaryCSV, mergeTutoring, scopedAttendance, scopeFrom, scopeValue, summarise, type Attendance } from './grading';
	import ReportPdfButton from './ReportPdfButton.svelte';
	import { toneClass, toneFor } from './reportPdf';
	import ScopePicker from './ScopePicker.svelte';

	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	const day = (d: Date) => d.toISOString().slice(0, 10);
	let from = $state(day(new Date(Date.now() - 90 * 864e5)));
	let to = $state(day(new Date()));
	let all = $state<Attendance | null>(null);
	// A module, topic or quiz counts only its quiz sessions (D-130).
	let scope = $state(scopeFrom(null));
	const scopes = $derived(all ? attendanceScopes(all) : []);
	const scopeName = $derived(scopes.find((o) => o.value === scopeValue(scope))?.label.replace(/ · quiz$/, '') ?? '');
	const data = $derived(all ? scopedAttendance(all, scope) : null);
	let tutoring = $state<ClassroomReport | null>(null);
	let tutoringOn = $state(false);
	let error = $state('');
	let busy = $state(false);

	async function load() {
		error = '';
		busy = true;
		try {
			const a = await api.get<Attendance>(`/api/teacher/classrooms/${classroomId}/attendance?from=${from}&to=${to}`);
			tutoring = null;
			if (tutoringOn) {
				const start = new Date(from + 'T00:00:00');
				const end = new Date(to + 'T00:00:00');
				end.setDate(end.getDate() + 1);
				tutoring = await new TutorClient().get<ClassroomReport>(`/api/classrooms/${classroomId}/attendance?from=${start.toISOString()}&to=${end.toISOString()}`).catch(() => null);
			}
			all = mergeTutoring(a, tutoring);
		} catch (e) {
			error = e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : 'The report didn’t load';
		} finally {
			busy = false;
		}
	}
	$effect(() => {
		void classroomId;
		tutoringConfig().then((c) => {
			tutoringOn = c.enabled;
			load();
		}, load);
	});

	async function setCounts(on: boolean) {
		try {
			await new TutorClient().put(`/api/classrooms/${classroomId}/attendance/settings`, { counts_in_results: on });
			if (tutoring) tutoring.counts_in_results = on;
			toast(on ? 'Attendance now shows in the classroom’s results' : 'Attendance left out of the results');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}

	const rows = $derived(data ? summarise(data) : []);
	const kinds = $derived(data ? (['quiz', 'meeting', 'tutoring'] as const).filter((k) => data!.events.some((e) => e.kind === k)) : []);
	const LABEL = { quiz: 'Quiz sessions', meeting: 'Meetings', tutoring: 'Tutoring' } as const;
	const file = (what: string) => `${what}-${classroomName}${scope.kind !== 'all' ? '-' + scopeName : ''}-${from}-to-${to}`.replace(/[\\/:*?"<>|]+/g, '-');

	const pdfTitle = $derived(`Attendance · ${classroomName}${scope.kind !== 'all' ? ' · ' + scopeName : ''}`);
	function table() {
		const head = ['Student', 'ID', ...kinds.map((k) => LABEL[k]), 'Attended', 'Rate', 'Minutes'];
		const body = rows.map((r) => [r.name, r.number, ...kinds.map((k) => `${r.byKind[k].came}/${r.byKind[k].of}`), `${r.came}/${r.of}`, r.rate == null ? '–' : r.rate + '%', r.minutes]);
		const tones = rows.map((r) => [null, null, ...kinds.map(() => null), null, toneFor(r.rate), null]);
		return { head, rows: body, tones };
	}
</script>

<section class="vstack gap-3" aria-label="Attendance">
	<div class="flex flex-wrap items-end gap-2">
		<label class="small">From <input class="input input-sm" type="date" bind:value={from} max={to} /></label>
		<label class="small">To <input class="input input-sm" type="date" bind:value={to} min={from} /></label>
		<button class="btn btn-sm" onclick={load} disabled={busy}>Show</button>
		{#if scopes.length > 1}<ScopePicker options={scopes} value={scopeValue(scope)} onchange={(v) => (scope = scopeFrom(v))} />{/if}
		<span class="grow"></span>
		{#if data}
			<button class="btn btn-sm" onclick={() => saveFile(file('attendance') + '.csv', attendanceSummaryCSV(data!))} disabled={!rows.length}><Icon name="download" size={15} />Summary CSV</button>
			<button class="btn btn-sm" onclick={() => saveFile(file('attendance-by-session') + '.csv', attendanceDetailCSV(data!))} disabled={!data.events.length}><Icon name="download" size={15} />By session CSV</button>
			<ReportPdfButton {classroomId} title={pdfTitle} fileName={pdfTitle} sheets={() => [{ heading: `${from} to ${to} · ${data?.events.length ?? 0} sessions`, ...table(), frozen: 2 }]} disabled={!rows.length} />
		{/if}
	</div>
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>{/if}
	{#if !data}
		{#if !error}<Skeleton lines={5} />{/if}
	{:else}
		<p class="small muted m-0">{data.events.length} {data.events.length === 1 ? 'session' : 'sessions'} from {from} to {to}: {kinds.map((k) => `${data!.events.filter((e) => e.kind === k).length} ${LABEL[k].toLowerCase()}`).join(', ') || 'none yet'}. {#if scope.kind !== 'all'}Only <strong>{scopeName}</strong>’s quiz sessions: meetings and tutoring belong to no module. {/if}A student came to a quiz session when admitted to it, to a meeting when they joined it{tutoringOn ? ', and to a tutoring session when connected while it was live' : ''}.</p>
		{#if tutoring}
			<label class="flex items-center gap-2 font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={tutoring.counts_in_results} onchange={(e) => setCounts(e.currentTarget.checked)} /> Show attendance in the classroom’s results</label>
		{/if}
		<div class="card card-border bg-base-100 p-2 sm:p-4">
			<div class="table-wrap">
				<table class="table table-sm">
					<thead><tr><th>Student</th>{#each kinds as k (k)}<th class="num">{LABEL[k]}</th>{/each}<th class="num">Attended</th><th class="num">Rate</th><th class="num">Minutes</th></tr></thead>
					<tbody>
						{#each rows as r (r.user_id)}
							<tr>
								<td>{r.name}{#if r.number}<br /><span class="small muted">{r.number}</span>{/if}</td>
								{#each kinds as k (k)}<td class="num tabular">{r.byKind[k].came} / {r.byKind[k].of}</td>{/each}
								<td class="num tabular">{r.came} / {r.of}</td>
								<td class="num tabular {toneClass(toneFor(r.rate))}">{r.rate == null ? '–' : r.rate + '%'}</td>
								<td class="num tabular">{r.minutes}</td>
							</tr>
						{:else}
							<tr><td colspan={kinds.length + 4} class="muted">No students yet.</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
		{#if data.events.length}
			<details class="card card-border bg-base-100 p-4 small">
				<summary class="font-semibold">Every session</summary>
				<ul class="m-0 mt-2 ps-4">
					{#each data.events as e (e.kind + e.id)}
						<li>{new Date(e.at).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })} · {LABEL[e.kind]} · {e.title} · {data.students.filter((s) => s.attended.includes(e.id)).length} of {data.students.length} present</li>
					{/each}
				</ul>
			</details>
		{/if}
	{/if}
</section>

<style>
	.num { text-align: right; }
</style>
