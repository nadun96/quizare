<script lang="ts">
	// The dashboard's charts (D-105): one range control above everything it
	// scopes, a row of stat tiles against the period before, then the charts.
	// A student: their scores against the class average, their weeks, their
	// classrooms. A teacher: each quiz's average, their students' weeks, each
	// classroom's participation. While a new range loads, the old charts
	// stay, dimmed, so nothing jumps.
	import { api } from '../api';
	import Skeleton from '../ui/Skeleton.svelte';
	import StatTile from './StatTile.svelte';
	import TrendChart from './TrendChart.svelte';
	import ColumnChart from './ColumnChart.svelte';
	import WeekStrip from './WeekStrip.svelte';
	import RankedBars from './RankedBars.svelte';
	import { RANGES, dayLabel, type Insights } from './insights';

	let { teacher }: { teacher: boolean } = $props();

	const KEY = 'qp:dash-range';
	function stored() {
		try {
			const n = Number(localStorage.getItem(KEY));
			return RANGES.some((r) => r.days === n) ? n : 90;
		} catch {
			return 90;
		}
	}
	let days = $state(stored());
	let data = $state<Insights | null>(null);
	let loading = $state(false);
	let failed = $state(false);

	$effect(() => {
		const d = days;
		loading = true;
		failed = false;
		api.get<Insights>('/api/my/dashboard/insights?days=' + d).then(
			(r) => { if (d === days) data = r; },
			() => (failed = true)
		).finally(() => (loading = false));
		try { localStorage.setItem(KEY, String(d)); } catch { /* fine */ }
	});
	const range = $derived(RANGES.find((r) => r.days === days) ?? RANGES[1]);

	const scorePoints = $derived((data?.scores ?? []).map((s) => ({ label: s.title, sub: dayLabel(s.at), value: s.pct, context: s.class_avg, note: `${s.classroom}, ${s.passed ? 'passed' : 'not passed'}` })));
	const sessionCols = $derived((data?.sessions ?? []).map((s) => ({ label: s.title, sub: dayLabel(s.at), value: s.avg, lines: [s.classroom, `${s.pass_rate}% passed`, `${s.results} ${s.results === 1 ? 'result' : 'results'}`] })));
	const roomRows = $derived((data?.classrooms ?? []).map((c) =>
		teacher
			? { label: c.name, value: c.participation ?? null, note: `${c.students ?? 0} ${c.students === 1 ? 'student' : 'students'}${c.avg !== null ? ` · avg ${c.avg}%` : ''}` }
			: { label: c.name, value: c.avg, note: `${c.quizzes} ${c.quizzes === 1 ? 'quiz' : 'quizzes'}` }));
	const k = $derived(data?.kpis ?? {});
	// The first eight, largest first; the rest on request.
	let allRooms = $state(false);
	const shownRooms = $derived(allRooms ? roomRows : roomRows.slice(0, 8));
</script>

<section class="insights vstack" aria-labelledby="ins-h">
	<div class="flex flex-wrap items-end gap-2">
		<div class="grow">
			<h2 id="ins-h" class="m-0 text-lg">{teacher ? 'How your classes are doing' : 'How you are doing'}</h2>
			<p class="m-0 small muted">The last {range.label}, against {range.prev}.</p>
		</div>
		<div class="join" role="group" aria-label="Period">
			{#each RANGES as r (r.days)}
				<button class="btn btn-sm join-item" class:btn-active={days === r.days} aria-pressed={days === r.days} onclick={() => (days = r.days)}>{r.label}</button>
			{/each}
		</div>
	</div>

	{#if !data}
		{#if failed}<p class="small muted">The charts couldn't load. Reload the page to try again.</p>{:else}<Skeleton card={false} lines={4} />{/if}
	{:else}
		<div class="body vstack" class:dim={loading} aria-busy={loading}>
			<div class="tiles">
				{#if teacher}
					<StatTile label="Active students" kpi={k.active_students} versus={range.prev} hint="of {k.students?.value ?? 0} in your classrooms" />
					<StatTile label="Average score" kpi={k.avg_score} unit="%" deltaUnit=" pts" versus={range.prev} hint={k.avg_score?.value ? '' : 'No results yet'} />
					<StatTile label="Pass rate" kpi={k.pass_rate} unit="%" versus={range.prev} hint="Of results in the period" />
					<StatTile label="Quizzes run" kpi={k.sessions} versus={range.prev} />
				{:else}
					<StatTile label="Average score" kpi={k.avg_score} unit="%" deltaUnit=" pts" versus={range.prev} hint={k.quizzes?.value ? '' : 'No results yet'} />
					<StatTile label="Quizzes done" kpi={k.quizzes} versus={range.prev} />
					<StatTile label="Pass rate" kpi={k.pass_rate} unit="%" versus={range.prev} hint="Of your quizzes in the period" />
					<StatTile label="Tutorials finished" kpi={k.tutorials} versus={range.prev} />
				{/if}
			</div>

			<div class="charts">
				<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2 wide">
					{#if teacher}
						<h3 class="m-0 text-base">Average score by quiz</h3>
						<p class="m-0 small muted">Your latest {sessionCols.length || ''} quizzes with results, oldest first.</p>
						{#if sessionCols.length}<ColumnChart cols={sessionCols} title="Average score by quiz" />{:else}<p class="empty small muted">No quiz results in this period yet.</p>{/if}
					{:else}
						<h3 class="m-0 text-base">Your scores</h3>
						<p class="m-0 small muted">Each quiz, with the class average for comparison.</p>
						{#if scorePoints.length}<TrendChart points={scorePoints} name="You" contextName="Class average" title="Your scores" />{:else}<p class="empty small muted">No released results in this period yet.</p>{/if}
					{/if}
				</article>

				<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2">
					<h3 class="m-0 text-base">{teacher ? 'Students active each week' : 'Your weeks'}</h3>
					<p class="m-0 small muted">{teacher ? 'Students who finished a quiz or tutorial.' : 'Quizzes and tutorials you finished.'}</p>
					{#if teacher}
						<WeekStrip weeks={data.weeks} measure={(w) => w.students ?? 0} title="Students active each week" detail={(w) => `${w.students ?? 0} students · ${w.quizzes} results · ${w.tutorials} tutorials`} />
					{:else}
						<WeekStrip weeks={data.weeks} measure={(w) => w.quizzes + w.tutorials} title="Your weeks" />
					{/if}
				</article>

				<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2">
					<h3 class="m-0 text-base">{teacher ? 'Participation by classroom' : 'Average by class'}</h3>
					<p class="m-0 small muted">{teacher ? 'Students with at least one result in the period.' : 'Your average in each class this period.'}</p>
					{#if roomRows.length}
						<RankedBars rows={shownRooms} title={teacher ? 'Participation by classroom' : 'Average by class'} />
						{#if roomRows.length > 8}<button class="btn btn-ghost btn-xs self-start" onclick={() => (allRooms = !allRooms)}>{allRooms ? 'Show the first 8' : `Show all ${roomRows.length}`}</button>{/if}
					{:else}<p class="empty small muted">{teacher ? 'No classrooms yet.' : 'No results in this period yet.'}</p>{/if}
				</article>
			</div>
		</div>
	{/if}
</section>

<style>
	.body { transition: opacity 150ms; }
	.body.dim { opacity: 0.55; }
	.tiles { display: grid; gap: 0.5rem; grid-template-columns: repeat(4, minmax(0, 1fr)); }
	.charts { display: grid; gap: 1rem; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: start; }
	.charts > :global(.wide) { grid-column: 1 / -1; }
	.empty { padding: 2rem 0; text-align: center; }
	@media (max-width: 860px) { .charts { grid-template-columns: minmax(0, 1fr); } }
	@media (max-width: 640px) { .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
