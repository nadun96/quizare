<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// The class report (D-140): the teacher chooses which quizzes and tutorials
	// are in it, each one's weight (0: shown, not in the total) and the mark it
	// uses; the same setup says what students see of their results, for how long.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import type { ClassroomReport as TutorReport } from '../tutoring/types';
	import { mergeTutoring, summarise, type Attendance, type AttendanceSummary } from './grading';
	import { saveFile } from '../tutoring/types';
	import GroupsEditor from './GroupsEditor.svelte';
	import ReportControls from './ReportControls.svelte';
	import ReportPdfButton from './ReportPdfButton.svelte';
	import ReportTh from './ReportTh.svelte';
	import { bandOptions, inBand, matches, newView, numberSheet, shownNote, sortRows, type ViewCol } from './reportView';
	import { DEFAULT_BANDS, toned, toneFor, type Bands } from './reportPdf';
	import { ATTENDANCE_SPAN, attendanceText, dateText, optionsOf, violationsText, classReportCSV, columns, DETAIL, draftFrom, draftProblem, draftWeightTotal, finalRange, GROUP_RULES, groupMarkText, groupSheets, groupsCSV, individualSheet, itemKey, KIND_LABEL, markText, markTone, memberNames, setAllPicks, setupPayload, ungrouped, weightText, type ClassReport, type Draft, type GroupRow, type ReportStudent, type Unit } from './classReport';

	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	let rep = $state<ClassReport | null>(null);
	let draft = $state<Draft | null>(null);
	let error = $state('');
	let saving = $state(false);
	let setupOpen = $state(false);
	let view = $state<'individual' | 'groups'>('individual');
	let bands = $state<Bands>(DEFAULT_BANDS);
	let managing = $state(false);
	const url = $derived(`/api/teacher/classrooms/${classroomId}/class-report`);
	const cols = $derived(rep ? columns(rep) : []);
	const range = $derived(rep ? finalRange(rep) : null);
	const problem = $derived(draft ? draftProblem(draft) : '');
	const units: { v: Unit; label: string }[] = [{ v: 'minutes', label: 'minutes' }, { v: 'hours', label: 'hours' }, { v: 'days', label: 'days' }];

	async function load() {
		error = '';
		try {
			const r = await api.get<ClassReport>(url);
			rep = r;
			draft = draftFrom(r);
		} catch (e) {
			error = e instanceof ApiError ? e.message : tr("The report didn’t load");
		}
	}
	$effect(() => {
		void url;
		load();
	});

	async function save() {
		if (!draft || problem) return;
		saving = true;
		try {
			const r = await api.put<ClassReport>(url, setupPayload(draft));
			rep = r;
			draft = draftFrom(r);
			setupOpen = false;
			toast(tr("Report saved"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("The report didn’t save"), 'error');
		} finally {
			saving = false;
		}
	}
	// What else it shows (D-156). Attendance comes from the Attendance report's figures,
	// so the two agree; it's fetched only when asked for.
	const options = $derived(rep ? optionsOf(rep) : null);
	let attendance = $state<Map<string, AttendanceSummary> | null>(null);
	$effect(() => {
		if (!options?.attendance) {
			attendance = null;
			return;
		}
		loadAttendance(classroomId);
	});
	async function loadAttendance(id: string) {
		const to = new Date(Date.now() + 864e5);
		const from = new Date(to.getTime() - 365 * 864e5);
		const day = (d: Date) => d.toISOString().slice(0, 10);
		try {
			const a = await api.get<Attendance>(`/api/teacher/classrooms/${id}/attendance?from=${day(from)}&to=${day(to)}`);
			const tutoring = (await tutoringConfig()).enabled
				? await new TutorClient().get<TutorReport>(`/api/classrooms/${id}/attendance?from=${from.toISOString()}&to=${to.toISOString()}`).catch(() => null)
				: null;
			attendance = new Map(summarise(mergeTutoring(a, tutoring)).map((s) => [s.user_id, s]));
		} catch {
			toast(tr("The attendance didn’t load"), 'error');
		}
	}

	// Search, filter and sort (D-154). The class figures below stay the whole class's.
	let peopleView = $state(newView());
	let groupsView = $state(newView('group'));
	const peopleCols = $derived<ViewCol<ReportStudent>[]>([
		{ key: 'student', label: 'Student', value: (s) => s.name },
		{ key: 'id', label: 'Student ID', value: (s) => s.student_number },
		...cols.map((it) => ({ key: itemKey(it), label: it.title, value: (s: ReportStudent) => s.marks[itemKey(it)]?.pct, numeric: true })),
		{ key: 'total', label: 'Total', value: (s) => s.total, numeric: true },
		{ key: 'final', label: 'Final', value: (s) => s.final, numeric: true },
		...(options?.violations ? [{ key: 'violations', label: 'Violations', value: (s: ReportStudent) => s.violations_total ?? 0, numeric: true }] : []),
		...(attendance ? [{ key: 'attendance', label: 'Attendance', value: (s: ReportStudent) => attendance?.get(s.user_id)?.rate, numeric: true }] : [])
	]);
	const groupCols = $derived<ViewCol<GroupRow>[]>([
		{ key: 'group', label: 'Group', value: (g) => g.name },
		...cols.map((it) => ({ key: itemKey(it), label: it.title, value: (g: GroupRow) => g.marks[itemKey(it)]?.pct, numeric: true })),
		{ key: 'total', label: 'Total', value: (g) => g.total, numeric: true },
		{ key: 'final', label: 'Final', value: (g) => g.final, numeric: true }
	]);
	const peopleShow = $derived([...bandOptions(bands, 'Final'), { v: 'missing', label: 'Has work missing' }, ...(options?.violations ? [{ v: 'violations', label: 'Has violations' }] : [])]);
	const shownStudents = $derived.by(() => {
		if (!rep) return [];
		const r = rep;
		const inGroup = view === 'individual' && peopleView.group ? new Set(r.class_groups.find((g) => g.id === peopleView.group)?.members ?? []) : null;
		const kept = r.students.filter((s) =>
			matches(`${s.name} ${s.student_number ?? ''}`, peopleView.q) &&
			(!inGroup || inGroup.has(s.user_id)) &&
			(!peopleView.show || (peopleView.show === 'missing' ? cols.some((it) => !s.marks[itemKey(it)]) : peopleView.show === 'violations' ? (s.violations_total ?? 0) > 0 : inBand(s.final, peopleView.show, bands))));
		return sortRows(kept, peopleCols, peopleView.sort, peopleView.dir);
	});
	const groupMatch = (g: GroupRow) => !!rep && matches(`${g.name} ${memberNames(rep, g)}`, groupsView.q);
	const shownGroups = $derived.by(() => {
		if (!rep) return [];
		const kept = rep.class_groups.filter((g) => groupMatch(g) && (!groupsView.show || inBand(g.final, groupsView.show, bands)));
		return sortRows(kept, groupCols, groupsView.sort, groupsView.dir);
	});
	// The report as filtered: what its sheets and files are made from.
	const groupsRep = $derived(rep ? { ...rep, class_groups: shownGroups, item_groups: rep.item_groups.map((o) => ({ ...o, groups: o.groups.filter(groupMatch) })) } : null);
	const peopleNote = $derived(rep ? shownNote(shownStudents.length, rep.students.length, 'students') : '');
	const groupsNote = $derived(rep ? shownNote(shownGroups.length, rep.class_groups.length, 'groups') : '');
	function csv() {
		if (!rep || !groupsRep) return;
		if (view === 'groups') saveFile(`${classroomName} group marks.csv`, groupsCSV(groupsRep));
		else saveFile(`${classroomName} class report.csv`, classReportCSV(rep, shownStudents, attendance));
	}
	const noted = (heading: string | undefined, note: string) => (note ? `${heading ?? ''}. ${note}` : heading);
	const sheets = () => {
		if (!rep || !groupsRep) return [];
		if (view === 'groups') return groupSheets(groupsRep, bands).map((s, i) => (i === 0 && rep?.class_groups.length ? { ...s, heading: noted(s.heading, groupsNote) } : s));
		const sheet = individualSheet({ ...rep, students: shownStudents }, bands, attendance);
		return [{ ...sheet, heading: noted(sheet.heading, peopleNote) }];
	};
	function addException() {
		draft?.exceptions.push({ item: '', user_id: '', detail: '', reviewN: null, reviewUnit: 'hours' });
	}
</script>

{#if error}
	<p class="alert alert-soft alert-warning">{error}</p>
{:else if !rep || !draft}
	<Skeleton lines={6} />
{:else if !rep.items.length}
	<p class="muted">{tr("Run a quiz or publish a tutorial with questions in this classroom, and it appears here.")}</p>
{:else}
	<div class="vstack">
		<div class="flex flex-wrap items-center gap-2">
			<p class="m-0 small muted grow">{tr("{0} of {1} in the report · total out of {2}", [cols.length, rep.items.length, rep.out_of])}</p>
			<button class="btn btn-sm" class:btn-primary={!setupOpen} aria-expanded={setupOpen} onclick={() => (setupOpen = !setupOpen)}><Icon name="settings" size={16} />{tr("Set up")}</button>
			<button class="btn btn-sm" onclick={csv}><Icon name="download" size={16} />{tr("CSV")}</button>
			<ReportPdfButton {classroomId} title={`${classroomName}: ${view === 'groups' ? 'group marks' : 'class report'}`} fileName={`${classroomName} ${view === 'groups' ? 'group marks' : 'class report'}`} {sheets} disabled={!cols.length || (view === 'groups' ? !sheets().length : !shownStudents.length)} onbands={(b) => (bands = b)} />
		</div>

		{#if setupOpen}
			<section class="card card-border bg-base-100 p-4 vstack" aria-label={tr("Set up the report")}>
				<div class="flex flex-wrap items-center gap-2">
					<h2 class="m-0 text-base grow">{tr("Set up the report")}</h2>
					<span class="small muted">{tr("Use on every quiz:")}</span>
					<button class="btn btn-xs" onclick={() => draft && setAllPicks(draft, 'high')}>{tr("Highest mark")}</button>
					<button class="btn btn-xs" onclick={() => draft && setAllPicks(draft, 'low')}>{tr("Lowest mark")}</button>
				</div>
				<div class="overflow-x-auto">
					<table class="table table-sm">
						<thead><tr><th>{tr("In report")}</th><th>{tr("Quiz, tutorial or poll")}</th><th>{tr("Mark used")}</th><th>{tr("Weight")}</th><th>{tr("Group mark")}</th><th>{tr("Students see")}</th><th>{tr("Review time")}</th></tr></thead>
						<tbody>
							{#each rep.items as it (itemKey(it))}
								{@const d = draft.items[itemKey(it)]}
								<tr>
									<td><input class="checkbox checkbox-sm" type="checkbox" bind:checked={d.included} aria-label={tr("Include {0}", [it.title])} /></td>
									<td class="min-w-48"><span class="font-semibold">{it.title}</span><br /><span class="text-xs muted">{KIND_LABEL[it.kind]}{#if it.topic}{' · '}{it.topic}{/if}</span></td>
									<td>
										{#if it.kind === 'quiz'}
											<select class="select select-sm w-28" bind:value={d.pick} aria-label={tr("Mark used for {0}", [it.title])}><option value="high">{tr("Highest")}</option><option value="low">{tr("Lowest")}</option></select>
										{:else}<span class="small muted">{tr("One submission")}</span>{/if}
									</td>
									<td><input class="input input-sm w-20 tabular" type="number" min="0" max="1" step="any" placeholder="0.4" bind:value={d.weight} aria-label={tr("Weight of {0}", [it.title])} /></td>
									<td><select class="select select-sm w-28" bind:value={d.groupRule} aria-label={tr("Group mark for {0}", [it.title])}>{#each GROUP_RULES as o (o.v)}<option value={o.v}>{tr(o.label)}</option>{/each}</select></td>
									{#if it.kind === 'poll'}
										<td colspan="2"><span class="small muted">{tr("The poll’s own settings")}</span></td>
									{:else}
										<td><select class="select select-sm w-52" bind:value={d.detail} aria-label={tr("What students see of {0}", [it.title])}>{#each DETAIL as o (o.v)}<option value={o.v} title={tr(o.hint)}>{tr(o.label)}</option>{/each}</select></td>
										<td class="whitespace-nowrap">
											<input class="input input-sm w-20 tabular" type="number" min="0" step="any" placeholder={tr("None")} bind:value={d.reviewN} aria-label={tr("Review time for {0}", [it.title])} />
											<select class="select select-sm w-24" bind:value={d.reviewUnit} aria-label={tr("Review time unit for {0}", [it.title])}>{#each units as u (u.v)}<option value={u.v}>{tr(u.label)}</option>{/each}</select>
										</td>
									{/if}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<p class="m-0 small muted">
					{tr("A weight is between 0 and 1, so 0.1 is 10%: a mark of 30 / 40 is 75%, and at weight 0.1 it adds a final mark of 7.5 to the total. Weights add up to")} <strong class="tabular">{draftWeightTotal(draft)}</strong>{tr(", so the total is out of")} <strong class="tabular">{Math.round(draftWeightTotal(draft) * 10000) / 100}</strong>{tr(". Weight 0 shows an item but leaves it out of the total. A group’s mark comes from its members’; a member with no mark counts 0. A review time runs from when a student gets their results. When it ends the score stays and the question detail closes; blank means no limit.")}
				</p>

				<fieldset class="flex flex-wrap items-center gap-x-5 gap-y-2 small">
					<legend class="mb-1 font-semibold">{tr("Also show in the report")}</legend>
					<label class="flex items-center gap-2 font-normal"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={draft.options.dates} />{tr("Date of each item")}</label>
					<label class="flex items-center gap-2 font-normal"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={draft.options.violations} />{tr("Violations")}</label>
					<label class="flex items-center gap-2 font-normal"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={draft.options.attendance} />{tr("Attendance ({0})", [ATTENDANCE_SPAN])}</label>
				</fieldset>

				<h3 class="m-0 text-sm">{tr("Exceptions for one student")}</h3>
				{#if draft.exceptions.length}
					<ul class="vstack gap-2 list-none m-0 p-0">
						{#each draft.exceptions as x, i (i)}
							<li class="flex flex-wrap items-center gap-2">
								<select class="select select-sm w-48" bind:value={x.user_id} aria-label={tr("Student")}><option value="">{tr("Student…")}</option>{#each rep.students as s (s.user_id)}<option value={s.user_id}>{s.name}</option>{/each}</select>
								<select class="select select-sm w-48" bind:value={x.item} aria-label={tr("Quiz or tutorial")}><option value="">{tr("Quiz or tutorial…")}</option>{#each rep.items.filter((x) => x.kind !== 'poll') as it (itemKey(it))}<option value={itemKey(it)}>{it.title}</option>{/each}</select>
								<select class="select select-sm w-52" bind:value={x.detail} aria-label={tr("What this student sees")}><option value="">{tr("Same as the class")}</option>{#each DETAIL.slice(1) as o (o.v)}<option value={o.v}>{tr(o.label)}</option>{/each}</select>
								<input class="input input-sm w-20 tabular" type="number" min="0" step="any" placeholder={tr("Same")} bind:value={x.reviewN} aria-label={tr("This student's review time; 0 is no limit")} title={tr("Blank: same as the class. 0: no limit.")} />
								<select class="select select-sm w-24" bind:value={x.reviewUnit} aria-label={tr("Review time unit")}>{#each units as u (u.v)}<option value={u.v}>{tr(u.label)}</option>{/each}</select>
								<button class="btn btn-ghost btn-sm" aria-label={tr("Remove exception")} onclick={() => draft?.exceptions.splice(i, 1)}><Icon name="x" size={16} /></button>
							</li>
						{/each}
					</ul>
				{/if}
				<div><button class="btn btn-sm" onclick={addException}>{tr("Add an exception")}</button></div>

				{#if problem}<p class="m-0 small text-error" role="alert">{problem}</p>{/if}
				<div class="flex gap-2">
					<button class="btn btn-primary btn-sm" disabled={saving || !!problem} onclick={save}>{saving ? tr("Saving…") : tr("Save")}</button>
					<button class="btn btn-sm" onclick={() => { if (rep) draft = draftFrom(rep); setupOpen = false; }}>{tr("Cancel")}</button>
				</div>
			</section>
		{/if}

		<div class="flex flex-wrap items-center gap-2">
			<div class="join" role="group" aria-label={tr("Mark sheet")}>
				<button class="btn btn-sm join-item" class:btn-primary={view === 'individual'} aria-pressed={view === 'individual'} onclick={() => (view = 'individual')}>{tr("Individual")}</button>
				<button class="btn btn-sm join-item" class:btn-primary={view === 'groups'} aria-pressed={view === 'groups'} onclick={() => (view = 'groups')}>{tr("Groups")}</button>
			</div>
			{#if view === 'groups'}<button class="btn btn-sm" aria-expanded={managing} onclick={() => (managing = !managing)}><Icon name="users" size={16} />{tr("Manage groups")}</button>{/if}
		</div>
		{#if view === 'individual' && cols.length && rep.students.length}
			<ReportControls bind:view={peopleView} cols={peopleCols} show={peopleShow} groups={rep.class_groups} shown={shownStudents.length} total={rep.students.length} noun="students" />
		{:else if view === 'groups' && rep.class_groups.length}
			<ReportControls bind:view={groupsView} cols={groupCols} show={bandOptions(bands, 'Final')} shown={shownGroups.length} total={rep.class_groups.length} noun="groups" placeholder={tr("Group or member")} />
		{/if}

		{#if view === 'groups'}
			{#if managing}<GroupsEditor {classroomId} report={rep} onsaved={(r) => { rep = r; draft = draftFrom(r); }} />{/if}
			{@const list = groupsRep ? groupSheets(groupsRep, bands) : []}
			{#if list.length}
				{#if rep.class_groups.length}
					<h2 class="m-0 text-base">{tr("By class group")}</h2>
					<div class="card card-border bg-base-100 overflow-x-auto">
						<table class="table table-sm">
							<thead><tr><th>{tr("No.")}</th><ReportTh bind:view={groupsView} cols={groupCols} col="group" label={tr("Group")} />{#each cols as it (itemKey(it))}<ReportTh bind:view={groupsView} cols={groupCols} col={itemKey(it)} label={it.title} class="min-w-32"><span class="font-normal text-xs muted normal-case">{weightText(it)} · {it.group_rule}{#if options?.dates && it.date}<br />{dateText(it)}{/if}</span></ReportTh>{/each}<ReportTh bind:view={groupsView} cols={groupCols} col="total" label={tr("Total")} /><ReportTh bind:view={groupsView} cols={groupCols} col="final" label={tr("Final")} /></tr></thead>
							<tbody>
								{#each shownGroups as g, n (g.id)}
									<tr>
										<td class="tabular muted">{n + 1}</td>
										<td class="min-w-40"><span class="font-semibold">{g.name}</span><br /><span class="text-xs muted">{memberNames(rep, g) || tr("No students")}</span></td>
										{#each cols as it (itemKey(it))}
											{@const m = g.marks[itemKey(it)]}
											<td class="tabular whitespace-nowrap" use:toned={markTone(m, 0, bands)}>{#if m}{groupMarkText(m)}{#if m.points != null}<br /><span class="text-xs muted">{tr("final mark {0}", [m.points])}</span>{/if}<br /><span class="text-xs muted">{tr("{0} of {1} marked", [m.marked, g.members.length])}</span>{:else}<span class="muted">—</span>{/if}</td>
										{/each}
										<td class="tabular whitespace-nowrap font-semibold">{g.total} / {rep.out_of}</td>
										<td class="tabular font-semibold" use:toned={toneFor(g.final, bands)}>{g.final == null ? '—' : `${g.final}%`}</td>
									</tr>
								{:else}
									<tr><td colspan={cols.length + 4} class="muted">{tr("No group matches. Clear the filters to see them all.")}</td></tr>
								{/each}
							</tbody>
						</table>
					</div>
					{#if ungrouped(rep).length}<p class="m-0 small text-warning">{tr("Not in a group: {0}", [ungrouped(rep).join(', ')])}</p>{/if}
				{/if}
				{#each list.filter((s) => s.heading !== 'Marks by class group').map(numberSheet) as sh (sh.heading)}
					<h2 class="m-0 text-base">{sh.heading}</h2>
					<div class="card card-border bg-base-100 overflow-x-auto">
						<table class="table table-sm">
							<thead><tr>{#each sh.head as h (h)}<th>{h}</th>{/each}</tr></thead>
							<tbody>{#each sh.rows as r, i (i)}<tr>{#each r as c, j (j)}<td class="tabular" use:toned={sh.tones?.[i]?.[j]} class:font-semibold={j === 0}>{c}</td>{/each}</tr>{/each}</tbody>
						</table>
					</div>
				{/each}
			{:else}
				<p class="muted">{tr("No groups yet. Use Manage groups to split the class into groups.")}</p>
			{/if}
		{:else if cols.length}
			<div class="card card-border bg-base-100 overflow-x-auto">
				<table class="table table-sm">
					<thead>
						<tr>
							<th>{tr("No.")}</th>
							<ReportTh bind:view={peopleView} cols={peopleCols} col="student" label={tr("Student")} />
							{#each cols as it (itemKey(it))}
								<ReportTh bind:view={peopleView} cols={peopleCols} col={itemKey(it)} label={it.title} class="min-w-32"><span class="font-normal text-xs muted normal-case">{weightText(it)}{#if it.kind === 'quiz'}{' · '}{it.pick === 'low' ? 'lowest' : 'highest'}{/if}{#if options?.dates && it.date}<br />{dateText(it)}{/if}</span></ReportTh>
							{/each}
							<ReportTh bind:view={peopleView} cols={peopleCols} col="total" label={tr("Total")} /><ReportTh bind:view={peopleView} cols={peopleCols} col="final" label={tr("Final")} />
							{#if options?.violations}<ReportTh bind:view={peopleView} cols={peopleCols} col="violations" label={tr("Violations")} />{/if}
							{#if attendance}<ReportTh bind:view={peopleView} cols={peopleCols} col="attendance" label={tr("Attendance")}><span class="font-normal text-xs muted normal-case">{ATTENDANCE_SPAN}</span></ReportTh>{/if}
						</tr>
					</thead>
					<tbody>
						{#each shownStudents as s, n (s.user_id)}
							<tr>
								<td class="tabular muted">{n + 1}</td>
								<td class="whitespace-nowrap"><span class="font-semibold">{s.name}</span>{#if s.student_number}<br /><span class="text-xs muted tabular">{s.student_number}</span>{/if}</td>
								{#each cols as it (itemKey(it))}
									{@const m = s.marks[itemKey(it)]}
									<td class="tabular whitespace-nowrap" use:toned={markTone(m, it.weight, bands)}>
										{#if m}
											{markText(m)}
											{#if m.points != null}<br /><span class="text-xs muted">{tr("final mark {0}", [m.points])}</span>{/if}
											{#if m.submissions > 1}<br /><span class="text-xs muted">{tr("of {0} submissions", [m.submissions])}</span>{/if}
											{#if m.pending}<br /><span class="text-xs text-warning">{tr("still being marked")}</span>{/if}
										{:else if it.weight > 0}<span class="text-xs text-warning">{tr("Missing: counts 0")}</span>
										{:else}<span class="muted">—</span>{/if}
										{#if options?.violations && s.violations?.[itemKey(it)]}<br /><span class="text-xs text-error"><Icon name="flag" size={11} /> {violationsText(s.violations[itemKey(it)])}</span>{/if}
									</td>
								{/each}
								<td class="tabular whitespace-nowrap font-semibold">{s.total} / {rep.out_of}</td>
								<td class="tabular font-semibold" use:toned={toneFor(s.final, bands)}>{s.final == null ? '—' : `${s.final}%`}</td>
								{#if options?.violations}<td class="tabular" class:text-error={!!s.violations_total}>{s.violations_total ?? 0}</td>{/if}
								{#if attendance}<td class="tabular whitespace-nowrap" use:toned={toneFor(attendance.get(s.user_id)?.rate, bands)}>{attendanceText(attendance.get(s.user_id))}</td>{/if}
							</tr>
						{:else}
							<tr><td colspan={cols.length + 4 + (options?.violations ? 1 : 0) + (attendance ? 1 : 0)} class="muted">{rep.students.length ? tr("Nobody matches. Clear the filters to see everyone.") : tr("No students are active in this classroom yet.")}</td></tr>
						{/each}
					</tbody>
					<tfoot>
						<tr><td></td><th>{tr("Class average")}</th>{#each cols as it (itemKey(it))}<td class="tabular">{it.avg == null ? '—' : `${it.avg}%`}</td>{/each}<td></td><td class="tabular">{range?.avg == null ? '—' : `${range.avg}%`}</td>{#if options?.violations}<td></td>{/if}{#if attendance}<td></td>{/if}</tr>
						<tr><td></td><th>{tr("Highest")}</th>{#each cols as it (itemKey(it))}<td class="tabular">{it.high == null ? '—' : `${it.high}%`}</td>{/each}<td></td><td class="tabular">{range?.high == null ? '—' : `${range.high}%`}</td>{#if options?.violations}<td></td>{/if}{#if attendance}<td></td>{/if}</tr>
						<tr><td></td><th>{tr("Lowest")}</th>{#each cols as it (itemKey(it))}<td class="tabular">{it.low == null ? '—' : `${it.low}%`}</td>{/each}<td></td><td class="tabular">{range?.low == null ? '—' : `${range.low}%`}</td>{#if options?.violations}<td></td>{/if}{#if attendance}<td></td>{/if}</tr>
					</tfoot>
				</table>
			</div>
		{:else}
			<p class="muted">{tr("Nothing is in the report. Set it up to choose what’s in it.")}</p>
		{/if}
	</div>
{/if}
