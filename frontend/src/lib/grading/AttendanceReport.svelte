<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
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
	import { attendanceDetailCSV, attendanceScopes, attendanceSummaryCSV, mergeTutoring, scopedAttendance, scopeFrom, scopeValue, summarise, type Attendance, type AttendanceSummary } from './grading';
	import ReportControls from './ReportControls.svelte';
	import ReportPdfButton from './ReportPdfButton.svelte';
	import ReportTh from './ReportTh.svelte';
	import { bandOptions, inBand, matches, newView, shownNote, sortRows, type ViewCol } from './reportView';
	import { DEFAULT_BANDS, toned, toneFor, type Bands } from './reportPdf';
	import ScopePicker from './ScopePicker.svelte';

	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	let bands = $state<Bands>(DEFAULT_BANDS);
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
			error = e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : tr("The report didn’t load");
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
			toast(on ? tr("Attendance now shows in the classroom’s results") : tr("Attendance left out of the results"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		}
	}

	const allRows = $derived(data ? summarise(data) : []);
	const kinds = $derived(data ? (['quiz', 'meeting', 'tutoring'] as const).filter((k) => data!.events.some((e) => e.kind === k)) : []);
	const LABEL = { quiz: 'Quiz sessions', meeting: 'Meetings', tutoring: 'Tutoring' } as const;
	// Search, filter and sort (D-154).
	let view = $state(newView());
	const viewCols = $derived<ViewCol<AttendanceSummary>[]>([
		{ key: 'student', label: 'Student', value: (r) => r.name },
		{ key: 'id', label: 'Student ID', value: (r) => r.number },
		...kinds.map((k) => ({ key: k, label: LABEL[k], value: (r: AttendanceSummary) => r.byKind[k].came, numeric: true })),
		{ key: 'came', label: 'Attended', value: (r) => r.came, numeric: true },
		{ key: 'rate', label: 'Rate', value: (r) => r.rate, numeric: true },
		{ key: 'minutes', label: 'Minutes', value: (r) => r.minutes, numeric: true }
	]);
	const showOptions = $derived([...bandOptions(bands, 'Rate'), { v: 'never', label: 'Never came' }]);
	const rows = $derived(sortRows(
		allRows.filter((r) => matches(`${r.name} ${r.number}`, view.q) && (!view.show || (view.show === 'never' ? r.came === 0 : inBand(r.rate, view.show, bands)))),
		viewCols, view.sort, view.dir));
	const note = $derived(shownNote(rows.length, allRows.length, 'students'));
	const file = (what: string) => `${what}-${classroomName}${scope.kind !== 'all' ? '-' + scopeName : ''}-${from}-to-${to}`.replace(/[\\/:*?"<>|]+/g, '-');

	const pdfTitle = $derived(`Attendance · ${classroomName}${scope.kind !== 'all' ? ' · ' + scopeName : ''}`);
	function table() {
		const head = ['Student', 'ID', ...kinds.map((k) => LABEL[k]), 'Attended', 'Rate', 'Minutes'];
		const body = rows.map((r) => [r.name, r.number, ...kinds.map((k) => `${r.byKind[k].came}/${r.byKind[k].of}`), `${r.came}/${r.of}`, r.rate == null ? '–' : r.rate + '%', r.minutes]);
		const tones = rows.map((r) => [null, null, ...kinds.map(() => null), null, toneFor(r.rate, bands), null]);
		return { head, rows: body, tones };
	}
</script>

<section class="vstack gap-3" aria-label={tr("Attendance")}>
	<div class="flex flex-wrap items-end gap-2">
		<label class="small">{tr("From")} <input class="input input-sm" type="date" bind:value={from} max={to} /></label>
		<label class="small">{tr("To")} <input class="input input-sm" type="date" bind:value={to} min={from} /></label>
		<button class="btn btn-sm" onclick={load} disabled={busy}>{tr("Show")}</button>
		{#if scopes.length > 1}<ScopePicker options={scopes} value={scopeValue(scope)} onchange={(v) => (scope = scopeFrom(v))} />{/if}
		<span class="grow"></span>
		{#if data}
			<button class="btn btn-sm" onclick={() => saveFile(file('attendance') + '.csv', attendanceSummaryCSV(data!, rows))} disabled={!rows.length}><Icon name="download" size={15} />{tr("Summary CSV")}</button>
			<button class="btn btn-sm" onclick={() => saveFile(file('attendance-by-session') + '.csv', attendanceDetailCSV(data!, rows.map((r) => r.user_id)))} disabled={!data.events.length}><Icon name="download" size={15} />{tr("By session CSV")}</button>
			<ReportPdfButton {classroomId} title={pdfTitle} fileName={pdfTitle} sheets={() => [{ heading: `${from} to ${to} · ${data?.events.length ?? 0} sessions${note ? ` · ${note}` : ''}`, ...table(), frozen: 2 }]} disabled={!rows.length} onbands={(b) => (bands = b)} />
		{/if}
	</div>
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>{/if}
	{#if !data}
		{#if !error}<Skeleton lines={5} />{/if}
	{:else}
		<p class="small muted m-0">{tr("{0} {1} from {2} to {3}: {4}.", [data.events.length, data.events.length === 1 ? 'session' : 'sessions', from, to, kinds.map((k) => `${data!.events.filter((e) => e.kind === k).length} ${LABEL[k].toLowerCase()}`).join(', ') || tr("none yet")])} {#if scope.kind !== 'all'}{tr("Only")} <strong>{scopeName}</strong>{tr("’s quiz sessions: meetings and tutoring belong to no module.")} {/if}{tr("A student came to a quiz session when admitted to it, to a meeting when they joined it{0}.", [tutoringOn ? tr(", and to a tutoring session when connected while it was live") : ''])}</p>
		{#if tutoring}
			<label class="flex items-center gap-2 font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={tutoring.counts_in_results} onchange={(e) => setCounts(e.currentTarget.checked)} /> {tr("Show attendance in the classroom’s results")}</label>
		{/if}
		{#if allRows.length}<ReportControls bind:view cols={viewCols} show={showOptions} shown={rows.length} total={allRows.length} noun="students" />{/if}
		<div class="card card-border bg-base-100 p-2 sm:p-4">
			<div class="table-wrap">
				<table class="table table-sm">
					<thead><tr><th class="num">{tr("No.")}</th><ReportTh bind:view cols={viewCols} col="student" label={tr("Student")} />{#each kinds as k (k)}<ReportTh bind:view cols={viewCols} col={k} label={LABEL[k]} class="num" />{/each}<ReportTh bind:view cols={viewCols} col="came" label={tr("Attended")} class="num" /><ReportTh bind:view cols={viewCols} col="rate" label={tr("Rate")} class="num" /><ReportTh bind:view cols={viewCols} col="minutes" label={tr("Minutes")} class="num" /></tr></thead>
					<tbody>
						{#each rows as r, n (r.user_id)}
							<tr>
								<td class="num tabular muted">{n + 1}</td>
								<td>{r.name}{#if r.number}<br /><span class="small muted">{r.number}</span>{/if}</td>
								{#each kinds as k (k)}<td class="num tabular">{r.byKind[k].came} / {r.byKind[k].of}</td>{/each}
								<td class="num tabular">{r.came} / {r.of}</td>
								<td class="num tabular" use:toned={toneFor(r.rate, bands)}>{r.rate == null ? '–' : r.rate + '%'}</td>
								<td class="num tabular">{r.minutes}</td>
							</tr>
						{:else}
							<tr><td colspan={kinds.length + 5} class="muted">{allRows.length ? tr("Nobody matches. Clear the filters to see everyone.") : tr("No students yet.")}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
		{#if data.events.length}
			<details class="card card-border bg-base-100 p-4 small">
				<summary class="font-semibold">{tr("Every session")}</summary>
				<ul class="m-0 mt-2 ps-4">
					{#each data.events as e (e.kind + e.id)}
						<li>{tr("{0} · {1} · {2} · {3} of {4} present", [new Date(e.at).toLocaleString(localeTag(), { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }), LABEL[e.kind], e.title, data.students.filter((s) => s.attended.includes(e.id)).length, data.students.length])}</li>
					{/each}
				</ul>
			</details>
		{/if}
	{/if}
</section>

<style>
	.num { text-align: right; }
</style>
