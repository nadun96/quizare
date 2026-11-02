<script lang="ts">
	// A classroom over months and years (D-119): how many members take part,
	// the average and pass rate, results and tutorials, month by month; and
	// each student over the same span, with whether they're going up or down.
	import { api } from '../api';
	import StatTile from '../dashboard/StatTile.svelte';
	import TrendChart from '../dashboard/TrendChart.svelte';
	import ColumnChart from '../dashboard/ColumnChart.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import StudentTrendsDialog from './StudentTrendsDialog.svelte';
	import { direction, fromFirst, monthLabel, points, SPANS, summary, type ClassroomTrends, type TrendStudent } from './trends';

	let { classroomId }: { classroomId: string } = $props();
	let months = $state(12);
	let data = $state<ClassroomTrends | null>(null);
	let failed = $state(false);
	let find = $state('');
	let sortBy = $state<'name' | 'avg' | 'change' | 'quizzes'>('name');
	let open = $state<TrendStudent | null>(null);

	$effect(() => {
		const m = months;
		failed = false;
		api.get<ClassroomTrends>(`/api/teacher/classrooms/${classroomId}/trends?months=${m}`).then(
			(d) => (data = d),
			() => (failed = true)
		);
	});

	const shown = $derived(data ? fromFirst(data.months) : []);
	const sum = $derived(summary(shown));
	const avgPoints = $derived(points(shown, (m) => m.avg, (m) => m.pass_rate, (m) => `${m.quizzes} results`));
	const k = (v: number | null) => (v === null ? undefined : { value: v, prev: null });
	const change = (s: TrendStudent) => (s.recent_avg !== null && s.earlier_avg !== null ? s.recent_avg - s.earlier_avg : null);
	const students = $derived.by(() => {
		const q = find.trim().toLowerCase();
		const list = (data?.students ?? []).filter((s) => !q || s.name.toLowerCase().includes(q) || (s.student_number ?? '').toLowerCase().includes(q));
		const val = (s: TrendStudent) => (sortBy === 'avg' ? s.avg : sortBy === 'quizzes' ? s.quizzes : sortBy === 'change' ? change(s) : null);
		return sortBy === 'name' ? list : [...list].sort((a, b) => (val(a) ?? -1e9) - (val(b) ?? -1e9));
	});
</script>

<section class="vstack" aria-labelledby="trends-h">
	<div class="flex flex-wrap items-end gap-3">
		<div class="grow">
			<h2 id="trends-h" class="m-0 text-lg">Trends</h2>
			<p class="m-0 small muted">Month by month{data?.updated_at ? `, brought up to date ${new Date(data.updated_at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}` : ''}. Results count in the month they were handed in.</p>
		</div>
		<div class="join" role="group" aria-label="Span">
			{#each SPANS as s (s.months)}<button class="btn btn-sm join-item" class:btn-active={months === s.months} aria-pressed={months === s.months} onclick={() => (months = s.months)}>{s.label}</button>{/each}
		</div>
	</div>

	{#if failed}
		<p class="alert alert-soft alert-warning m-0">The trends couldn’t be loaded.</p>
	{:else if !data}
		<Skeleton lines={6} />
	{:else if sum.results + sum.tutorials === 0}
		<div class="card card-border bg-base-100 p-6 small muted">Nothing yet in this span. Trends appear once students hand in quizzes or finish tutorials; they are brought up to date every 15 minutes.</div>
	{:else}
		<div class="tiles">
			<StatTile label="Members taking part" kpi={k(sum.takingPart)} unit="%" versus="" hint="Members with a result or a finished tutorial, over the months shown" />
			<StatTile label="Average score" kpi={k(sum.avg)} unit="%" versus="" />
			<StatTile label="Pass rate" kpi={k(sum.passRate)} unit="%" versus="" />
			<StatTile label="Quiz results" kpi={k(sum.results)} versus="" />
			<StatTile label="Tutorials finished" kpi={k(sum.tutorials)} versus="" />
		</div>
		<div class="charts">
			<div class="card card-border bg-base-100 p-4 vstack gap-2">
				<h3 class="m-0 text-base">Average score each month</h3>
				{#if avgPoints.length}
					<TrendChart title="Average score each month" name="Average" contextName="Pass rate" rowName="Month" noun="months" points={avgPoints} />
				{:else}
					<p class="m-0 small muted">No quiz results in this span, only tutorials.</p>
				{/if}
			</div>
			<div class="card card-border bg-base-100 p-4 vstack gap-2">
				<h3 class="m-0 text-base">Members taking part each month</h3>
				<ColumnChart title="Members taking part each month"
					cols={shown.filter((m) => m.taking_part !== null && m.taking_part !== undefined).map((m) => ({ label: monthLabel(m.month), sub: monthLabel(m.month, true), value: m.taking_part ?? 0, lines: [`${m.active} of ${m.enrolled} members`, `${m.quizzes} results, ${m.tutorials} tutorials`] }))} />
			</div>
		</div>

		<details class="card card-border bg-base-100 p-4">
			<summary class="font-semibold">Every month</summary>
			<div class="table-wrap mt-2">
				<table class="table table-sm">
					<thead><tr><th>Month</th><th>Members</th><th>Taking part</th><th>Sessions</th><th>Results</th><th>Average</th><th>Pass rate</th><th>Tutorials</th></tr></thead>
					<tbody>
						{#each [...shown].reverse() as m (m.month)}
							<tr><td>{monthLabel(m.month)}</td><td class="tabular">{m.enrolled ?? 0}</td><td class="tabular">{m.taking_part ?? '—'}{m.taking_part != null ? '%' : ''}</td><td class="tabular">{m.sessions ?? 0}</td><td class="tabular">{m.quizzes}</td><td class="tabular">{m.avg ?? '—'}{m.avg !== null ? '%' : ''}</td><td class="tabular">{m.pass_rate ?? '—'}{m.pass_rate !== null ? '%' : ''}</td><td class="tabular">{m.tutorials}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</details>
	{/if}

	{#if data && data.students.length}
		<div class="card card-border bg-base-100 p-4 vstack gap-3">
			<div class="flex flex-wrap items-center gap-2">
				<h3 class="m-0 grow text-base">Students over this span</h3>
				<label class="input input-sm w-full sm:w-60"><Icon name="search" size={14} /><span class="sr-only">Find a student</span><input type="search" placeholder="Find a student" bind:value={find} /></label>
				<label class="sr-only" for="trend-sort">Order</label>
				<select id="trend-sort" class="select select-sm w-auto" bind:value={sortBy}>
					<option value="name">By name</option>
					<option value="avg">Lowest average first</option>
					<option value="change">Falling most first</option>
					<option value="quizzes">Fewest results first</option>
				</select>
			</div>
			<div class="table-wrap">
				<table class="table table-sm">
					<thead><tr><th>Student</th><th>Results</th><th>Average</th><th>Last 3 months</th><th>Tutorials</th><th>Active months</th><th>Last active</th><th><span class="sr-only">Details</span></th></tr></thead>
					<tbody>
						{#each students as s (s.user_id)}
							{@const dir = direction(s.recent_avg, s.earlier_avg)}
							<tr>
								<td>{s.name}{#if s.student_number}<br /><span class="small muted">{s.student_number}</span>{/if}</td>
								<td class="tabular">{s.quizzes}</td>
								<td class="tabular">{s.avg ?? '—'}{s.avg !== null ? '%' : ''}</td>
								<td class="tabular">
									{s.recent_avg ?? '—'}{s.recent_avg !== null ? '%' : ''}
									{#if dir}<span class="dir {dir}" title="Against the three months before: {s.earlier_avg}%"><Icon name={dir === 'up' ? 'arrow-up' : dir === 'down' ? 'arrow-down' : 'minus'} size={13} />{dir}</span>{/if}
								</td>
								<td class="tabular">{s.tutorials}</td>
								<td class="tabular">{s.active_months}</td>
								<td class="small">{s.last_active ? monthLabel(s.last_active) : 'Not yet'}</td>
								<td><button class="btn btn-xs" onclick={() => (open = s)}>Months</button></td>
							</tr>
						{:else}
							<tr><td colspan="8" class="muted">No student matches “{find}”.</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}
</section>

{#if open}<StudentTrendsDialog {classroomId} student={open} {months} onclose={() => (open = null)} />{/if}

<style>
	.tiles { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr)); }
	.charts { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr)); }
	.dir { display: inline-flex; align-items: center; gap: 0.15rem; margin-left: 0.35rem; font-size: 0.75rem; }
	.dir.up { color: var(--color-success); }
	.dir.down { color: var(--color-error); }
	.dir.steady { color: var(--color-muted); }
</style>
