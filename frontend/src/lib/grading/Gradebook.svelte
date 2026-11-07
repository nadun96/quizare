<script lang="ts">
	// A classroom's results with weights (D-122): every quiz and tutorial,
	// how much of the final result each carries, each student's mark and
	// points (55 % on an item worth 10 % gives 5.5), their weighted total and
	// final result. Weights needn't add up to 100: tests marked elsewhere can
	// make up the rest. Downloads as CSV or prints (Save as PDF).
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import { saveFile, type ClassroomReport } from '../tutoring/types';
	import { classAverage, gradebookCSV, key, marksLine, studentRange, totalAverage, mergeTutoring, scopedGradebook, scopeFrom, scopeLabel, scopeOptions, scopeValue, summarise, weightTotal, type GradeStudent, type Attendance, type AttendanceSummary, type Gradebook } from './grading';
	import ReportControls from './ReportControls.svelte';
	import ReportPdfButton from './ReportPdfButton.svelte';
	import ReportTh from './ReportTh.svelte';
	import { bandOptions, inBand, matches, newView, shownNote, sortRows, type ViewCol } from './reportView';
	import { DEFAULT_BANDS, toned, toneFor, type Bands } from './reportPdf';
	import ScopePicker from './ScopePicker.svelte';

	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	let g = $state<Gradebook | null>(null);
	let bands = $state<Bands>(DEFAULT_BANDS);
	let error = $state('');
	let editing = $state(false);
	let draft = $state<Record<string, number | null>>({});
	let saving = $state(false);
	// Attendance counts for the whole classroom only, so a part of it shows none.
	let attendanceAll = $state<Map<string, AttendanceSummary> | null>(null);
	// The report covers the whole classroom, a module, a topic, or one quiz or tutorial (D-130).
	let scope = $state(scopeFrom(null));
	const full = $derived(g);
	const shown = $derived(full ? scopedGradebook(full, scope) : null);
	const scoped = $derived(scope.kind !== 'all');
	const scopeName = $derived(full ? scopeLabel(full.items, scope) : '');

	async function load() {
		try {
			g = await api.get<Gradebook>(`/api/teacher/classrooms/${classroomId}/gradebook`);
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'The results didn’t load';
		}
	}
	$effect(() => {
		void classroomId;
		load();
		// Attendance joins the report when the teacher counts tutoring attendance in results (TS-FR-82).
		tutoringConfig().then(async (c) => {
			if (!c.enabled) return;
			try {
				const to = new Date(Date.now() + 864e5);
				const from = new Date(to.getTime() - 365 * 864e5);
				const rep = await new TutorClient().get<ClassroomReport>(`/api/classrooms/${classroomId}/attendance?from=${from.toISOString()}&to=${to.toISOString()}`);
				if (!rep.counts_in_results) return;
				const day = (d: Date) => d.toISOString().slice(0, 10);
				const a = await api.get<Attendance>(`/api/teacher/classrooms/${classroomId}/attendance?from=${day(from)}&to=${day(to)}`);
				attendanceAll = new Map(summarise(mergeTutoring(a, rep)).map((s) => [s.user_id, s]));
			} catch {
				/* the results show without it */
			}
		});
	});

	function startEditing() {
		draft = Object.fromEntries((g?.items ?? []).map((it) => [key(it), it.weight]));
		editing = true;
	}
	const check = $derived(weightTotal(draft));
	async function saveWeights() {
		if (!g) return;
		saving = true;
		try {
			g = await api.put<Gradebook>(`/api/teacher/classrooms/${classroomId}/weights`, {
				weights: g.items.map((it) => ({ kind: it.kind, id: it.id, weight: Number(draft[key(it)] ?? 0) || 0 }))
			});
			editing = false;
			toast('Weights saved; the results are recalculated');
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : 'Could not save the weights', 'error');
		} finally {
			saving = false;
		}
	}

	const weighted = $derived(!!shown && shown.weight_total > 0);
	// Search, filter and sort (D-154). The class figures below stay the whole class's.
	let view = $state(newView());
	const attendanceShown = $derived(scoped ? null : attendanceAll);
	const viewCols = $derived<ViewCol<GradeStudent>[]>(shown ? [
		{ key: 'student', label: 'Student', value: (s) => s.name },
		{ key: 'id', label: 'Student ID', value: (s) => s.student_number },
		...shown.items.map((it) => ({ key: key(it), label: it.title, value: (s: GradeStudent) => s.marks[key(it)]?.pct, numeric: true })),
		{ key: 'average', label: 'Average', value: (s) => s.average, numeric: true },
		{ key: 'marks', label: 'Total marks', value: (s) => s.marks_total, numeric: true },
		...(weighted ? [{ key: 'points', label: 'Points', value: (s: GradeStudent) => s.total, numeric: true }, { key: 'final', label: 'Final', value: (s: GradeStudent) => s.final, numeric: true }] : []),
		...(attendanceShown ? [{ key: 'attendance', label: 'Attendance', value: (s: GradeStudent) => attendanceShown.get(s.user_id)?.rate, numeric: true }] : [])
	] : []);
	const measure = (s: GradeStudent) => (weighted ? s.final : s.average);
	const showOptions = $derived([...bandOptions(bands, weighted ? 'Final' : 'Average'), { v: 'missing', label: 'Has work missing' }]);
	const shownStudents = $derived.by(() => {
		if (!shown) return [];
		const kept = shown.students.filter((s) =>
			matches(`${s.name} ${s.student_number ?? ''}`, view.q) &&
			(!view.show || (view.show === 'missing' ? shown.items.some((it) => !s.marks[key(it)]) : inBand(measure(s), view.show, bands))));
		return sortRows(kept, viewCols, view.sort, view.dir);
	});
	const note = $derived(shown ? shownNote(shownStudents.length, shown.students.length, 'students') : '');
	const fmt = (v: number | null | undefined, unit = '') => (v == null ? '–' : `${Math.round(v * 100) / 100}${unit}`);
	const file = (what: string) => `${what}-${classroomName}-${new Date().toISOString().slice(0, 10)}`.replace(/[\\/:*?"<>|]+/g, '-');

	function download() {
		if (shown) saveFile(file(scoped ? `results-${scopeName}` : 'results') + '.csv', gradebookCSV(shown, scoped ? undefined : (attendanceAll ?? undefined), shownStudents));
	}
	const pdfTitle = $derived(`Results · ${classroomName}${scoped ? ' · ' + scopeName : ''}`);
	function table(g: Gradebook) {
		const attendance = scoped ? null : attendanceAll;
		const head = ['Student', 'ID', ...g.items.map((it) => it.title + (it.weight ? ` (${it.weight}%)` : '')), 'Average', 'Total marks', ...(weighted ? ['Points', 'Final'] : []), ...(attendance ? ['Attendance'] : [])];
		const rows = shownStudents.map((s) => [
			s.name,
			s.student_number ?? '',
			...g!.items.map((it) => {
				const m = s.marks[key(it)];
				return m ? (it.weight ? `${m.pct}% · ${m.points}` : `${m.pct}%`) : it.weight ? '– · 0' : '–';
			}),
			s.average == null ? '–' : s.average + '%',
			marksLine(s),
			...(weighted ? [`${s.total} / ${g!.weight_total}`, s.final == null ? '–' : s.final + '%'] : []),
			...(attendance ? [attendance.get(s.user_id)?.rate != null ? attendance.get(s.user_id)!.rate + '%' : '–'] : [])
		]);
		const tones = shownStudents.map((s) => [
			null,
			null,
			...g.items.map((it) => toneFor(s.marks[key(it)]?.pct, bands) ?? (it.weight ? ('missing' as const) : null)),
			toneFor(s.average, bands),
			null,
			...(weighted ? [null, toneFor(s.final, bands)] : []),
			...(attendance ? [toneFor(attendance.get(s.user_id)?.rate, bands)] : [])
		]);
		const heading = (weighted ? `Weighted items count for ${g.weight_total}% of the final result; a mark shows as percent · points.` : 'Each student’s best mark on each quiz and tutorial.') + (note ? ` ${note}.` : '');
		return { heading, head, rows, tones };
	}
</script>

<section class="vstack gap-3" aria-label="Results">
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>{/if}
	{#if !g}
		{#if !error}<Skeleton lines={5} />{/if}
	{:else if shown}
		{@const g = shown}
		{@const attendance = scoped ? null : attendanceAll}
		{@const range = studentRange(g)}
		<div class="flex flex-wrap items-center gap-2">
			{#if full && full.items.length}<ScopePicker options={scopeOptions(full.items)} value={scopeValue(scope)} onchange={(v) => ((scope = scopeFrom(v)), (editing = false))} />{/if}
			<span class="grow"></span>
			{#if !editing && !scoped}<button class="btn btn-sm" onclick={startEditing} disabled={!g.items.length}><Icon name="sliders" size={15} />Weights</button>{/if}
			<button class="btn btn-sm" onclick={download} disabled={!shownStudents.length}><Icon name="download" size={15} />CSV</button>
			<ReportPdfButton {classroomId} title={pdfTitle} fileName={pdfTitle} sheets={() => (shown ? [{ ...table(shown), frozen: 2 }] : [])} disabled={!shownStudents.length} onbands={(b) => (bands = b)} />
		</div>
		{#if g.students.length}<ReportControls bind:view cols={viewCols} show={showOptions} shown={shownStudents.length} total={g.students.length} noun="students" />{/if}
		<div class="flex flex-wrap items-center gap-2">
			<p class="small muted m-0 grow">
				{#if scoped}<strong>{scopeName}</strong>: {g.items.length} {g.items.length === 1 ? 'item' : 'items'}{#if weighted}, worth <strong>{g.weight_total}%</strong> of the final result; the final here is the points as a percentage of that{/if}. Weights are set for the whole classroom.
				{:else if weighted}Weighted items make up <strong>{g.weight_total}%</strong> of the final result; each mark becomes points (55% on an item worth 10% is 5.5), and a weighted item a student hasn’t done counts 0.
				{:else}Each student’s best mark on each quiz and tutorial. Set weights to work out a final result.{/if}
			</p>
		</div>

		{#if editing}
			<div class="card card-border bg-base-100 p-4 vstack gap-3">
				<div>
					<h3 class="m-0 text-base">How much each counts towards the final result</h3>
					<p class="m-0 small muted">A percent of the final result. They don’t need to add up to 100: leave room for tests marked outside the platform. Leave blank or 0 for what shouldn’t count.</p>
				</div>
				<div class="weights">
					{#each g.items as it (key(it))}
						<label class="weight">
							<span class="min-w-0"><span class="block truncate font-semibold">{it.title}</span><span class="small muted">{it.kind === 'quiz' ? 'Quiz' : 'Tutorial'} · {it.topic}</span></span>
							<span class="input input-sm w-24 flex-none"><input type="number" min="0" max="100" step="0.5" bind:value={draft[key(it)]} aria-label="Weight of {it.title}" />%</span>
						</label>
					{/each}
				</div>
				<div class="flex flex-wrap items-center gap-2">
					<span class="grow small" class:text-error={!check.ok}>Total: <strong>{check.total}%</strong> of the final result{#if check.total < 100 && check.ok}; {Math.round((100 - check.total) * 100) / 100}% left for other assessments{/if}{#if !check.ok} · at most 100%, each between 0 and 100{/if}</span>
					<button class="btn btn-sm btn-ghost" onclick={() => (editing = false)}>Cancel</button>
					<button class="btn btn-sm btn-primary" onclick={saveWeights} disabled={!check.ok || saving}>Save weights</button>
				</div>
			</div>
		{/if}

		<div class="card card-border bg-base-100 p-2 sm:p-4">
			<div class="table-wrap">
				<table class="table table-sm">
					<thead>
						<tr>
							<th class="num">No.</th>
							<ReportTh bind:view cols={viewCols} col="student" label="Student" />
							{#each g.items as it (key(it))}
								<ReportTh bind:view cols={viewCols} col={key(it)} label={it.title} class="num" title={it.topic}><span class="small muted font-normal">{it.kind === 'tutorial' ? 'Tutorial' : 'Quiz'}{it.weight ? ` · ${it.weight}%` : weighted ? ' · not counted' : ''}</span></ReportTh>
							{/each}
							<ReportTh bind:view cols={viewCols} col="average" label="Average" class="num" />
							<ReportTh bind:view cols={viewCols} col="marks" label="Total marks" class="num" />
							{#if weighted}<ReportTh bind:view cols={viewCols} col="points" label="Points" class="num"><span class="small muted font-normal">of {g.weight_total}</span></ReportTh><ReportTh bind:view cols={viewCols} col="final" label="Final" class="num" />{/if}
							{#if attendance}<ReportTh bind:view cols={viewCols} col="attendance" label="Attendance" class="num" />{/if}
						</tr>
					</thead>
					<tbody>
						{#each shownStudents as s, n (s.user_id)}
							<tr>
								<td class="num tabular muted">{n + 1}</td>
								<td>{s.name}{#if s.student_number}<br /><span class="small muted">{s.student_number}</span>{/if}</td>
								{#each g.items as it (key(it))}
									{@const m = s.marks[key(it)]}
									<td class="num tabular" use:toned={toneFor(m?.pct, bands)}>{#if m}{m.pct}%{#if m.max}<br /><span class="small muted">{m.score} / {m.max}</span>{/if}{#if m.points != null}<br /><span class="small muted">{m.points} pts</span>{/if}{:else}<span class="muted">–</span>{/if}</td>
								{/each}
								<td class="num tabular">{fmt(s.average, '%')}</td>
								<td class="num tabular whitespace-nowrap">{marksLine(s)}</td>
								{#if weighted}
									<td class="num tabular font-semibold">{fmt(s.total)}{#if s.missing}<br /><span class="small text-warning">{s.missing} missed</span>{/if}</td>
									<td class="num tabular font-semibold" use:toned={toneFor(s.final, bands)}>{fmt(s.final, '%')}</td>
								{/if}
								{#if attendance}<td class="num tabular">{fmt(attendance.get(s.user_id)?.rate, '%')}</td>{/if}
							</tr>
						{:else}
							<tr><td colspan={g.items.length + 6} class="muted">{g.students.length ? 'Nobody matches. Clear the filters to see everyone.' : 'No students yet.'}</td></tr>
						{/each}
					</tbody>
					{#if g.students.length && g.items.length}
						<tfoot>
							<tr>
								<td></td><th>Class average</th>
								{#each g.items as it (key(it))}<td class="num tabular">{fmt(it.avg, '%')}<br /><span class="small muted">{it.taken} took it{#if it.max_marks} · out of {it.max_marks}{/if}</span></td>{/each}
								<td class="num tabular">{fmt(classAverage(g), '%')}</td>
								<td class="num tabular">{fmt(totalAverage(g))}</td>
								<td colspan={(weighted ? 2 : 0) + (attendance ? 1 : 0)}></td>
							</tr>
							<tr class="sub">
								<td></td><th>Highest</th>
								{#each g.items as it (key(it))}<td class="num tabular">{fmt(it.high, '%')}</td>{/each}
								<td class="num tabular">{fmt(range.avgHigh, '%')}</td>
								<td class="num tabular">{fmt(range.totalHigh)}</td>
								<td colspan={(weighted ? 2 : 0) + (attendance ? 1 : 0)}></td>
							</tr>
							<tr class="sub">
								<td></td><th>Lowest</th>
								{#each g.items as it (key(it))}<td class="num tabular">{fmt(it.low, '%')}</td>{/each}
								<td class="num tabular">{fmt(range.avgLow, '%')}</td>
								<td class="num tabular">{fmt(range.totalLow)}</td>
								<td colspan={(weighted ? 2 : 0) + (attendance ? 1 : 0)}></td>
							</tr>
						</tfoot>
					{/if}
				</table>
			</div>
		</div>
		{#if !g.items.length}<p class="muted small m-0">Results appear once quizzes are run in this classroom or tutorials with questions are published.</p>{/if}

		{#if g.sessions?.length}
			<details class="card card-border bg-base-100" open={scope.kind === 'item'}>
				<summary class="p-3 sm:px-4 cursor-pointer font-semibold">Each session’s report <span class="badge badge-soft badge-sm">{g.sessions.length}</span></summary>
				<div class="table-wrap px-2 pb-2 sm:px-4">
					<table class="table table-sm">
						<thead><tr><th class="num">No.</th><th>Session</th><th>When</th><th class="num">Took part</th><th class="num">Results</th><th class="num">Average</th><th><span class="sr-only">Reports</span></th></tr></thead>
						<tbody>
							{#each g.sessions as x, n (x.id)}
								<tr>
									<td class="num tabular muted">{n + 1}</td>
									<td>{x.title}{#if x.status !== 'ended'} <span class="badge badge-soft badge-sm">{x.status}</span>{/if}</td>
									<td class="small">{new Date(x.at).toLocaleString()}</td>
									<td class="num tabular">{x.attempts}</td>
									<td class="num tabular">{x.results}</td>
									<td class="num tabular">{fmt(x.avg, '%')}</td>
									<td class="text-right whitespace-nowrap"><a class="btn btn-xs" href={'/t/sessions/' + x.id + '/results'}>Results</a> <a class="btn btn-xs btn-ghost" href={'/api/teacher/sessions/' + x.id + '/export.csv'} download><Icon name="download" size={13} />CSV</a></td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</details>
		{/if}
	{/if}
</section>

<style>
	.num { text-align: right; }
	.weights { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(min(100%, 20rem), 1fr)); }
	.weight { display: flex; align-items: center; gap: 0.75rem; padding: 0.5rem; border: 1px solid var(--color-base-300); font-weight: 400; }
	.weight > span:first-child { flex: 1; }
	tfoot tr:first-child th, tfoot tr:first-child td { border-top: 2px solid var(--color-base-300); }
</style>
