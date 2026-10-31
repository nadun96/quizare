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
	import { cascade } from '../ui/motion';
	import { t, tn } from '../i18n/i18n.svelte';

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

	const scorePoints = $derived((data?.scores ?? []).map((s) => ({ label: s.title, sub: dayLabel(s.at), value: s.pct, context: s.class_avg, note: s.classroom })));
	const sessionCols = $derived((data?.sessions ?? []).map((s) => ({ label: s.title, sub: dayLabel(s.at), value: s.avg, lines: [s.classroom, t('ins.passed', { n: s.pass_rate }), tn('ins.results', s.results)] })));
	const roomRows = $derived((data?.classrooms ?? []).map((c) =>
		teacher
			? { label: c.name, value: c.participation ?? null, note: tn('ins.students', c.students ?? 0) + (c.avg !== null ? ` · ${c.avg}%` : '') }
			: { label: c.name, value: c.avg, note: tn('ins.quizzes', c.quizzes) }));
	const k = $derived(data?.kpis ?? {});
	// The first eight, largest first; the rest on request.
	let allRooms = $state(false);
	const shownRooms = $derived(allRooms ? roomRows : roomRows.slice(0, 8));
</script>

<section class="insights vstack" aria-labelledby="ins-h">
	<div class="flex flex-wrap items-end gap-2">
		<div class="grow">
			<h2 id="ins-h" class="m-0 text-lg">{teacher ? t('ins.classTitle') : t('ins.youTitle')}</h2>
			<p class="m-0 small muted">{t('ins.period', { range: t(range.label), prev: t(range.prev) })}</p>
		</div>
		<div class="join" role="group" aria-label={t('ins.periodLabel')}>
			{#each RANGES as r (r.days)}
				<button class="btn btn-sm join-item" class:btn-active={days === r.days} aria-pressed={days === r.days} onclick={() => (days = r.days)}>{t(r.label)}</button>
			{/each}
		</div>
	</div>

	{#if !data}
		{#if failed}<p class="small muted">{t('ins.failed')}</p>{:else}<Skeleton card={false} lines={4} />{/if}
	{:else}
		<div class="body vstack" class:dim={loading} aria-busy={loading}>
			<div class="tiles" use:cascade>
				{#if teacher}
					<StatTile label={t('ins.activeStudents')} kpi={k.active_students} versus={t(range.prev)} hint={t('ins.ofEnrolled', { n: k.students?.value ?? 0 })} />
					<StatTile label={t('ins.avgScore')} kpi={k.avg_score} unit="%" deltaUnit=" pts" versus={t(range.prev)} hint={k.avg_score?.value ? '' : t('ins.noResults')} />
					<StatTile label={t('ins.passRate')} kpi={k.pass_rate} unit="%" versus={t(range.prev)} hint={t('ins.ofResults')} />
					<StatTile label={t('ins.quizzesRun')} kpi={k.sessions} versus={t(range.prev)} />
				{:else}
					<StatTile label={t('ins.avgScore')} kpi={k.avg_score} unit="%" deltaUnit=" pts" versus={t(range.prev)} hint={k.quizzes?.value ? '' : t('ins.noResults')} />
					<StatTile label={t('ins.quizzesDone')} kpi={k.quizzes} versus={t(range.prev)} />
					<StatTile label={t('ins.passRate')} kpi={k.pass_rate} unit="%" versus={t(range.prev)} hint={t('ins.ofYours')} />
					<StatTile label={t('ins.tutorialsDone')} kpi={k.tutorials} versus={t(range.prev)} />
				{/if}
			</div>

			<div class="charts" use:cascade={{ y: 18, stagger: 0.08, delay: 0.1 }}>
				<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2 wide">
					{#if teacher}
						<h3 class="m-0 text-base">{t('ins.byQuiz')}</h3>
						<p class="m-0 small muted">{t('ins.byQuizSub')}</p>
						{#if sessionCols.length}<ColumnChart cols={sessionCols} title={t('ins.byQuiz')} />{:else}<p class="empty small muted">{t('ins.noQuizResults')}</p>{/if}
					{:else}
						<h3 class="m-0 text-base">{t('ins.yourScores')}</h3>
						<p class="m-0 small muted">{t('ins.yourScoresSub')}</p>
						{#if scorePoints.length}<TrendChart points={scorePoints} name={t('ins.you')} contextName={t('ins.classAvg')} title={t('ins.yourScores')} />{:else}<p class="empty small muted">{t('ins.noReleased')}</p>{/if}
					{/if}
				</article>

				<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2">
					<h3 class="m-0 text-base">{teacher ? t('ins.weeklyT') : t('ins.weeks')}</h3>
					<p class="m-0 small muted">{teacher ? t('ins.weeklyTSub') : t('ins.weeksSub')}</p>
					{#if teacher}
						<WeekStrip weeks={data.weeks} measure={(w) => w.students ?? 0} title={t('ins.weeklyT')} detail={(w) => `${tn('ins.students', w.students ?? 0)} · ${tn('ins.results', w.quizzes)} · ${w.tutorials} ${t('kind.tutorial')}`} />
					{:else}
						<WeekStrip weeks={data.weeks} measure={(w) => w.quizzes + w.tutorials} title={t('ins.weeks')} detail={(w) => `${tn('ins.quizzes', w.quizzes)} · ${w.tutorials} ${t('kind.tutorial')}`} />
					{/if}
				</article>

				<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2">
					<h3 class="m-0 text-base">{teacher ? t('ins.participation') : t('ins.byClass')}</h3>
					<p class="m-0 small muted">{teacher ? t('ins.participationSub') : t('ins.byClassSub')}</p>
					{#if roomRows.length}
						<RankedBars rows={shownRooms} title={teacher ? t('ins.participation') : t('ins.byClass')} />
						{#if roomRows.length > 8}<button class="btn btn-ghost btn-xs self-start" onclick={() => (allRooms = !allRooms)}>{allRooms ? t('ins.showFirst') : t('ins.showAll', { n: roomRows.length })}</button>{/if}
					{:else}<p class="empty small muted">{teacher ? t('ins.noClassrooms') : t('ins.noReleased')}</p>{/if}
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
	.empty { padding: 0.5rem 0; margin: 0; }
	@media (max-width: 860px) { .charts { grid-template-columns: minmax(0, 1fr); } }
	@media (max-width: 640px) { .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
