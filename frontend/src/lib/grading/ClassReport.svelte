<script lang="ts">
	// The class report (D-140): the teacher chooses which quizzes and tutorials
	// are in it, each one's weight (0: shown, not in the total) and the mark it
	// uses; the same setup says what students see of their results, for how long.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { saveFile } from '../tutoring/types';
	import GroupsEditor from './GroupsEditor.svelte';
	import ReportControls from './ReportControls.svelte';
	import ReportPdfButton from './ReportPdfButton.svelte';
	import ReportTh from './ReportTh.svelte';
	import { bandOptions, inBand, matches, newView, numberSheet, shownNote, sortRows, type ViewCol } from './reportView';
	import { DEFAULT_BANDS, toned, toneFor, type Bands } from './reportPdf';
	import { classReportCSV, columns, DETAIL, draftFrom, draftProblem, draftWeightTotal, finalRange, GROUP_RULES, groupMarkText, groupSheets, groupsCSV, individualSheet, itemKey, KIND_LABEL, markText, markTone, memberNames, setAllPicks, setupPayload, ungrouped, weightText, type ClassReport, type Draft, type GroupRow, type ReportStudent, type Unit } from './classReport';

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
			error = e instanceof ApiError ? e.message : 'The report didn’t load';
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
			toast('Report saved');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The report didn’t save', 'error');
		} finally {
			saving = false;
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
		{ key: 'final', label: 'Final', value: (s) => s.final, numeric: true }
	]);
	const groupCols = $derived<ViewCol<GroupRow>[]>([
		{ key: 'group', label: 'Group', value: (g) => g.name },
		...cols.map((it) => ({ key: itemKey(it), label: it.title, value: (g: GroupRow) => g.marks[itemKey(it)]?.pct, numeric: true })),
		{ key: 'total', label: 'Total', value: (g) => g.total, numeric: true },
		{ key: 'final', label: 'Final', value: (g) => g.final, numeric: true }
	]);
	const peopleShow = $derived([...bandOptions(bands, 'Final'), { v: 'missing', label: 'Has work missing' }]);
	const shownStudents = $derived.by(() => {
		if (!rep) return [];
		const r = rep;
		const inGroup = view === 'individual' && peopleView.group ? new Set(r.class_groups.find((g) => g.id === peopleView.group)?.members ?? []) : null;
		const kept = r.students.filter((s) =>
			matches(`${s.name} ${s.student_number ?? ''}`, peopleView.q) &&
			(!inGroup || inGroup.has(s.user_id)) &&
			(!peopleView.show || (peopleView.show === 'missing' ? cols.some((it) => !s.marks[itemKey(it)]) : inBand(s.final, peopleView.show, bands))));
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
		else saveFile(`${classroomName} class report.csv`, classReportCSV(rep, shownStudents));
	}
	const noted = (heading: string | undefined, note: string) => (note ? `${heading ?? ''}. ${note}` : heading);
	const sheets = () => {
		if (!rep || !groupsRep) return [];
		if (view === 'groups') return groupSheets(groupsRep, bands).map((s, i) => (i === 0 && rep?.class_groups.length ? { ...s, heading: noted(s.heading, groupsNote) } : s));
		const sheet = individualSheet({ ...rep, students: shownStudents }, bands);
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
	<p class="muted">Run a quiz or publish a tutorial with questions in this classroom, and it appears here.</p>
{:else}
	<div class="vstack">
		<div class="flex flex-wrap items-center gap-2">
			<p class="m-0 small muted grow">{cols.length} of {rep.items.length} in the report · total out of {rep.out_of}</p>
			<button class="btn btn-sm" class:btn-primary={!setupOpen} aria-expanded={setupOpen} onclick={() => (setupOpen = !setupOpen)}><Icon name="settings" size={16} />Set up</button>
			<button class="btn btn-sm" onclick={csv}><Icon name="download" size={16} />CSV</button>
			<ReportPdfButton {classroomId} title={`${classroomName}: ${view === 'groups' ? 'group marks' : 'class report'}`} fileName={`${classroomName} ${view === 'groups' ? 'group marks' : 'class report'}`} {sheets} disabled={!cols.length || (view === 'groups' ? !sheets().length : !shownStudents.length)} onbands={(b) => (bands = b)} />
		</div>

		{#if setupOpen}
			<section class="card card-border bg-base-100 p-4 vstack" aria-label="Set up the report">
				<div class="flex flex-wrap items-center gap-2">
					<h2 class="m-0 text-base grow">Set up the report</h2>
					<span class="small muted">Use on every quiz:</span>
					<button class="btn btn-xs" onclick={() => draft && setAllPicks(draft, 'high')}>Highest mark</button>
					<button class="btn btn-xs" onclick={() => draft && setAllPicks(draft, 'low')}>Lowest mark</button>
				</div>
				<div class="overflow-x-auto">
					<table class="table table-sm">
						<thead><tr><th>In report</th><th>Quiz, tutorial or poll</th><th>Mark used</th><th>Weight</th><th>Group mark</th><th>Students see</th><th>Review time</th></tr></thead>
						<tbody>
							{#each rep.items as it (itemKey(it))}
								{@const d = draft.items[itemKey(it)]}
								<tr>
									<td><input class="checkbox checkbox-sm" type="checkbox" bind:checked={d.included} aria-label={'Include ' + it.title} /></td>
									<td class="min-w-48"><span class="font-semibold">{it.title}</span><br /><span class="text-xs muted">{KIND_LABEL[it.kind]}{#if it.topic}{' · '}{it.topic}{/if}</span></td>
									<td>
										{#if it.kind === 'quiz'}
											<select class="select select-sm w-28" bind:value={d.pick} aria-label={'Mark used for ' + it.title}><option value="high">Highest</option><option value="low">Lowest</option></select>
										{:else}<span class="small muted">One submission</span>{/if}
									</td>
									<td><input class="input input-sm w-20 tabular" type="number" min="0" max="1" step="any" placeholder="0.4" bind:value={d.weight} aria-label={'Weight of ' + it.title} /></td>
									<td><select class="select select-sm w-28" bind:value={d.groupRule} aria-label={'Group mark for ' + it.title}>{#each GROUP_RULES as o (o.v)}<option value={o.v}>{o.label}</option>{/each}</select></td>
									{#if it.kind === 'poll'}
										<td colspan="2"><span class="small muted">The poll’s own settings</span></td>
									{:else}
										<td><select class="select select-sm w-52" bind:value={d.detail} aria-label={'What students see of ' + it.title}>{#each DETAIL as o (o.v)}<option value={o.v} title={o.hint}>{o.label}</option>{/each}</select></td>
										<td class="whitespace-nowrap">
											<input class="input input-sm w-20 tabular" type="number" min="0" step="any" placeholder="None" bind:value={d.reviewN} aria-label={'Review time for ' + it.title} />
											<select class="select select-sm w-24" bind:value={d.reviewUnit} aria-label={'Review time unit for ' + it.title}>{#each units as u (u.v)}<option value={u.v}>{u.label}</option>{/each}</select>
										</td>
									{/if}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<p class="m-0 small muted">
					A weight is between 0 and 1, so 0.1 is 10%: a mark of 30 / 40 is 75%, and at weight 0.1 it adds a final mark of 7.5 to the total. Weights add up to <strong class="tabular">{draftWeightTotal(draft)}</strong>, so the total is out of <strong class="tabular">{Math.round(draftWeightTotal(draft) * 10000) / 100}</strong>. Weight 0 shows an item but leaves it out of the total. A group’s mark comes from its members’; a member with no mark counts 0.
					A review time runs from when a student gets their results. When it ends the score stays and the question detail closes; blank means no limit.
				</p>

				<h3 class="m-0 text-sm">Exceptions for one student</h3>
				{#if draft.exceptions.length}
					<ul class="vstack gap-2 list-none m-0 p-0">
						{#each draft.exceptions as x, i (i)}
							<li class="flex flex-wrap items-center gap-2">
								<select class="select select-sm w-48" bind:value={x.user_id} aria-label="Student"><option value="">Student…</option>{#each rep.students as s (s.user_id)}<option value={s.user_id}>{s.name}</option>{/each}</select>
								<select class="select select-sm w-48" bind:value={x.item} aria-label="Quiz or tutorial"><option value="">Quiz or tutorial…</option>{#each rep.items.filter((x) => x.kind !== 'poll') as it (itemKey(it))}<option value={itemKey(it)}>{it.title}</option>{/each}</select>
								<select class="select select-sm w-52" bind:value={x.detail} aria-label="What this student sees"><option value="">Same as the class</option>{#each DETAIL.slice(1) as o (o.v)}<option value={o.v}>{o.label}</option>{/each}</select>
								<input class="input input-sm w-20 tabular" type="number" min="0" step="any" placeholder="Same" bind:value={x.reviewN} aria-label="This student's review time; 0 is no limit" title="Blank: same as the class. 0: no limit." />
								<select class="select select-sm w-24" bind:value={x.reviewUnit} aria-label="Review time unit">{#each units as u (u.v)}<option value={u.v}>{u.label}</option>{/each}</select>
								<button class="btn btn-ghost btn-sm" aria-label="Remove exception" onclick={() => draft?.exceptions.splice(i, 1)}><Icon name="x" size={16} /></button>
							</li>
						{/each}
					</ul>
				{/if}
				<div><button class="btn btn-sm" onclick={addException}>Add an exception</button></div>

				{#if problem}<p class="m-0 small text-error" role="alert">{problem}</p>{/if}
				<div class="flex gap-2">
					<button class="btn btn-primary btn-sm" disabled={saving || !!problem} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>
					<button class="btn btn-sm" onclick={() => { if (rep) draft = draftFrom(rep); setupOpen = false; }}>Cancel</button>
				</div>
			</section>
		{/if}

		<div class="flex flex-wrap items-center gap-2">
			<div class="join" role="group" aria-label="Mark sheet">
				<button class="btn btn-sm join-item" class:btn-primary={view === 'individual'} aria-pressed={view === 'individual'} onclick={() => (view = 'individual')}>Individual</button>
				<button class="btn btn-sm join-item" class:btn-primary={view === 'groups'} aria-pressed={view === 'groups'} onclick={() => (view = 'groups')}>Groups</button>
			</div>
			{#if view === 'groups'}<button class="btn btn-sm" aria-expanded={managing} onclick={() => (managing = !managing)}><Icon name="users" size={16} />Manage groups</button>{/if}
		</div>
		{#if view === 'individual' && cols.length && rep.students.length}
			<ReportControls bind:view={peopleView} cols={peopleCols} show={peopleShow} groups={rep.class_groups} shown={shownStudents.length} total={rep.students.length} noun="students" />
		{:else if view === 'groups' && rep.class_groups.length}
			<ReportControls bind:view={groupsView} cols={groupCols} show={bandOptions(bands, 'Final')} shown={shownGroups.length} total={rep.class_groups.length} noun="groups" placeholder="Group or member" />
		{/if}

		{#if view === 'groups'}
			{#if managing}<GroupsEditor {classroomId} report={rep} onsaved={(r) => { rep = r; draft = draftFrom(r); }} />{/if}
			{@const list = groupsRep ? groupSheets(groupsRep, bands) : []}
			{#if list.length}
				{#if rep.class_groups.length}
					<h2 class="m-0 text-base">By class group</h2>
					<div class="card card-border bg-base-100 overflow-x-auto">
						<table class="table table-sm">
							<thead><tr><th>No.</th><ReportTh bind:view={groupsView} cols={groupCols} col="group" label="Group" />{#each cols as it (itemKey(it))}<ReportTh bind:view={groupsView} cols={groupCols} col={itemKey(it)} label={it.title} class="min-w-32"><span class="font-normal text-xs muted normal-case">{weightText(it)} · {it.group_rule}</span></ReportTh>{/each}<ReportTh bind:view={groupsView} cols={groupCols} col="total" label="Total" /><ReportTh bind:view={groupsView} cols={groupCols} col="final" label="Final" /></tr></thead>
							<tbody>
								{#each shownGroups as g, n (g.id)}
									<tr>
										<td class="tabular muted">{n + 1}</td>
										<td class="min-w-40"><span class="font-semibold">{g.name}</span><br /><span class="text-xs muted">{memberNames(rep, g) || 'No students'}</span></td>
										{#each cols as it (itemKey(it))}
											{@const m = g.marks[itemKey(it)]}
											<td class="tabular whitespace-nowrap" use:toned={markTone(m, 0, bands)}>{#if m}{groupMarkText(m)}{#if m.points != null}<br /><span class="text-xs muted">final mark {m.points}</span>{/if}<br /><span class="text-xs muted">{m.marked} of {g.members.length} marked</span>{:else}<span class="muted">—</span>{/if}</td>
										{/each}
										<td class="tabular whitespace-nowrap font-semibold">{g.total} / {rep.out_of}</td>
										<td class="tabular font-semibold" use:toned={toneFor(g.final, bands)}>{g.final == null ? '—' : `${g.final}%`}</td>
									</tr>
								{:else}
									<tr><td colspan={cols.length + 4} class="muted">No group matches. Clear the filters to see them all.</td></tr>
								{/each}
							</tbody>
						</table>
					</div>
					{#if ungrouped(rep).length}<p class="m-0 small text-warning">Not in a group: {ungrouped(rep).join(', ')}</p>{/if}
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
				<p class="muted">No groups yet. Use Manage groups to split the class into groups.</p>
			{/if}
		{:else if cols.length}
			<div class="card card-border bg-base-100 overflow-x-auto">
				<table class="table table-sm">
					<thead>
						<tr>
							<th>No.</th>
							<ReportTh bind:view={peopleView} cols={peopleCols} col="student" label="Student" />
							{#each cols as it (itemKey(it))}
								<ReportTh bind:view={peopleView} cols={peopleCols} col={itemKey(it)} label={it.title} class="min-w-32"><span class="font-normal text-xs muted normal-case">{weightText(it)}{#if it.kind === 'quiz'}{' · '}{it.pick === 'low' ? 'lowest' : 'highest'}{/if}</span></ReportTh>
							{/each}
							<ReportTh bind:view={peopleView} cols={peopleCols} col="total" label="Total" /><ReportTh bind:view={peopleView} cols={peopleCols} col="final" label="Final" />
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
											{#if m.points != null}<br /><span class="text-xs muted">final mark {m.points}</span>{/if}
											{#if m.submissions > 1}<br /><span class="text-xs muted">of {m.submissions} submissions</span>{/if}
											{#if m.pending}<br /><span class="text-xs text-warning">still being marked</span>{/if}
										{:else if it.weight > 0}<span class="text-xs text-warning">Missing: counts 0</span>
										{:else}<span class="muted">—</span>{/if}
									</td>
								{/each}
								<td class="tabular whitespace-nowrap font-semibold">{s.total} / {rep.out_of}</td>
								<td class="tabular font-semibold" use:toned={toneFor(s.final, bands)}>{s.final == null ? '—' : `${s.final}%`}</td>
							</tr>
						{:else}
							<tr><td colspan={cols.length + 4} class="muted">{rep.students.length ? 'Nobody matches. Clear the filters to see everyone.' : 'No students are active in this classroom yet.'}</td></tr>
						{/each}
					</tbody>
					<tfoot>
						<tr><td></td><th>Class average</th>{#each cols as it (itemKey(it))}<td class="tabular">{it.avg == null ? '—' : `${it.avg}%`}</td>{/each}<td></td><td class="tabular">{range?.avg == null ? '—' : `${range.avg}%`}</td></tr>
						<tr><td></td><th>Highest</th>{#each cols as it (itemKey(it))}<td class="tabular">{it.high == null ? '—' : `${it.high}%`}</td>{/each}<td></td><td class="tabular">{range?.high == null ? '—' : `${range.high}%`}</td></tr>
						<tr><td></td><th>Lowest</th>{#each cols as it (itemKey(it))}<td class="tabular">{it.low == null ? '—' : `${it.low}%`}</td>{/each}<td></td><td class="tabular">{range?.low == null ? '—' : `${range.low}%`}</td></tr>
					</tfoot>
				</table>
			</div>
		{:else}
			<p class="muted">Nothing is in the report. Set it up to choose what’s in it.</p>
		{/if}
	</div>
{/if}
