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
	import { gradebookCSV, key, mergeTutoring, openPrint, printHTML, summarise, weightTotal, type Attendance, type AttendanceSummary, type Gradebook } from './grading';

	let { classroomId, classroomName = 'classroom' }: { classroomId: string; classroomName?: string } = $props();
	let g = $state<Gradebook | null>(null);
	let error = $state('');
	let editing = $state(false);
	let draft = $state<Record<string, number | null>>({});
	let saving = $state(false);
	let attendance = $state<Map<string, AttendanceSummary> | null>(null);

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
				attendance = new Map(summarise(mergeTutoring(a, rep)).map((s) => [s.user_id, s]));
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

	const weighted = $derived(!!g && g.weight_total > 0);
	const fmt = (v: number | null | undefined, unit = '') => (v == null ? '–' : `${Math.round(v * 100) / 100}${unit}`);
	const file = (what: string) => `${what}-${classroomName}-${new Date().toISOString().slice(0, 10)}`.replace(/[\\/:*?"<>|]+/g, '-');

	function download() {
		if (g) saveFile(file('results') + '.csv', gradebookCSV(g, attendance ?? undefined));
	}
	function print() {
		if (!g) return;
		const head = ['Student', 'ID', ...g.items.map((it) => it.title + (it.weight ? ` (${it.weight}%)` : '')), 'Average', ...(weighted ? ['Points', 'Final'] : []), ...(attendance ? ['Attendance'] : [])];
		const rows = g.students.map((s) => [
			s.name,
			s.student_number ?? '',
			...g!.items.map((it) => {
				const m = s.marks[key(it)];
				return m ? (it.weight ? `${m.pct}% · ${m.points}` : `${m.pct}%`) : it.weight ? '– · 0' : '–';
			}),
			s.average == null ? '–' : s.average + '%',
			...(weighted ? [`${s.total} / ${g!.weight_total}`, s.final == null ? '–' : s.final + '%'] : []),
			...(attendance ? [attendance.get(s.user_id)?.rate != null ? attendance.get(s.user_id)!.rate + '%' : '–'] : [])
		]);
		const ok = openPrint(printHTML(`Results · ${g.classroom}`, weighted ? `Weighted items count for ${g.weight_total}% of the final result; a mark shows as percent · points.` : 'Each student’s best mark on each quiz and tutorial.', head, rows));
		if (!ok) toast('Allow pop-ups for this site to print', 'error');
	}
</script>

<section class="vstack gap-3" aria-label="Results">
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>{/if}
	{#if !g}
		{#if !error}<Skeleton lines={5} />{/if}
	{:else}
		<div class="flex flex-wrap items-center gap-2">
			<p class="small muted m-0 grow">
				{#if weighted}Weighted items make up <strong>{g.weight_total}%</strong> of the final result; each mark becomes points (55% on an item worth 10% is 5.5), and a weighted item a student hasn’t done counts 0.
				{:else}Each student’s best mark on each quiz and tutorial. Set weights to work out a final result.{/if}
			</p>
			{#if !editing}<button class="btn btn-sm" onclick={startEditing} disabled={!g.items.length}><Icon name="sliders" size={15} />Weights</button>{/if}
			<button class="btn btn-sm" onclick={download} disabled={!g.students.length}><Icon name="download" size={15} />CSV</button>
			<button class="btn btn-sm" onclick={print} disabled={!g.students.length}><Icon name="table" size={15} />Print / PDF</button>
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
							<th>Student</th>
							{#each g.items as it (key(it))}
								<th class="num" title={it.topic}>{it.title}<br /><span class="small muted font-normal">{it.kind === 'tutorial' ? 'Tutorial' : 'Quiz'}{it.weight ? ` · ${it.weight}%` : weighted ? ' · not counted' : ''}</span></th>
							{/each}
							<th class="num">Average</th>
							{#if weighted}<th class="num">Points<br /><span class="small muted font-normal">of {g.weight_total}</span></th><th class="num">Final</th>{/if}
							{#if attendance}<th class="num">Attendance</th>{/if}
						</tr>
					</thead>
					<tbody>
						{#each g.students as s (s.user_id)}
							<tr>
								<td>{s.name}{#if s.student_number}<br /><span class="small muted">{s.student_number}</span>{/if}</td>
								{#each g.items as it (key(it))}
									{@const m = s.marks[key(it)]}
									<td class="num tabular">{#if m}{m.pct}%{#if m.points != null}<br /><span class="small muted">{m.points} pts</span>{/if}{:else}<span class="muted">–</span>{/if}</td>
								{/each}
								<td class="num tabular">{fmt(s.average, '%')}</td>
								{#if weighted}
									<td class="num tabular font-semibold">{fmt(s.total)}{#if s.missing}<br /><span class="small text-warning">{s.missing} missed</span>{/if}</td>
									<td class="num tabular font-semibold">{fmt(s.final, '%')}</td>
								{/if}
								{#if attendance}<td class="num tabular">{fmt(attendance.get(s.user_id)?.rate, '%')}</td>{/if}
							</tr>
						{:else}
							<tr><td colspan={g.items.length + 4} class="muted">No students yet.</td></tr>
						{/each}
					</tbody>
					{#if g.students.length && g.items.length}
						<tfoot>
							<tr>
								<th>Class average</th>
								{#each g.items as it (key(it))}<td class="num tabular">{fmt(it.avg, '%')}<br /><span class="small muted">{it.taken} took it</span></td>{/each}
								<td colspan={1 + (weighted ? 2 : 0) + (attendance ? 1 : 0)}></td>
							</tr>
						</tfoot>
					{/if}
				</table>
			</div>
		</div>
		{#if !g.items.length}<p class="muted small m-0">Results appear once quizzes are run in this classroom or tutorials with questions are published.</p>{/if}
	{/if}
</section>

<style>
	.num { text-align: right; }
	.weights { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(min(100%, 20rem), 1fr)); }
	.weight { display: flex; align-items: center; gap: 0.75rem; padding: 0.5rem; border: 1px solid var(--color-base-300); font-weight: 400; }
	.weight > span:first-child { flex: 1; }
	tfoot th, tfoot td { border-top: 2px solid var(--color-base-300); }
</style>
