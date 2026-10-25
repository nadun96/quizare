<script lang="ts">
	// Time on a presentation set beside marks (D-78, PR-FR-32, PR-FR-33): the
	// quizzes and tutorials it goes with, each student as a point (time
	// across, score up) with the straight-line trend, slide by slide who
	// spent longer and how they scored, and the records as CSV or to Drive.
	import { api, ApiError } from '../api';
	import SaveToDrive from '../drive/SaveToDrive.svelte';
	import { duration } from '../tutorial/tutorial';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { count } from '../words';
	import { niceSeconds, trendWords, type Analysis, type Linkable, type Links } from './presentation';

	let { id }: { id: string } = $props();
	let links = $state<Links | null>(null);
	let chosen = $state<string[]>([]);
	let a = $state<Analysis | null>(null);
	let editing = $state(false);

	const key = (l: Pick<Linkable, 'kind' | 'id'>) => l.kind + ':' + l.id;
	async function load() {
		links = await api.get<Links>(`/api/teacher/presentations/${id}/links`);
		chosen = links.linked.map(key);
		editing = !links.linked.length;
		a = await api.get<Analysis>(`/api/teacher/presentations/${id}/analysis`);
	}
	$effect(() => {
		load();
	});
	async function saveLinks() {
		try {
			links = await api.put<Links>(`/api/teacher/presentations/${id}/links`, {
				linked: chosen.map((k) => ({ kind: k.split(':')[0], id: k.split(':')[1] }))
			});
			editing = false;
			a = await api.get<Analysis>(`/api/teacher/presentations/${id}/analysis`);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not save', 'error');
		}
	}

	// ---- the chart: one series, so no legend; the title names it ----
	const W = 640, H = 300, L = 44, R = 28, T = 32, B = 40;
	const maxSecs = $derived(niceSeconds(Math.max(1, ...(a?.points ?? []).map((p) => p.seconds))));
	const x = (secs: number) => L + (secs / maxSecs) * (W - L - R);
	const y = (pct: number) => T + (1 - pct / 100) * (H - T - B);
	const xTicks = $derived([0, 0.25, 0.5, 0.75, 1].map((f) => f * maxSecs));
	const span = $derived.by(() => {
		const xs = (a?.points ?? []).map((p) => p.seconds);
		return xs.length ? { from: Math.min(...xs), to: Math.max(...xs) } : { from: 0, to: 0 };
	});
	let hover = $state<number | null>(null);
	// The fitted line across the students' own range of times, clipped to the plot.
	const trendLine = $derived.by(() => {
		const t = a?.trend;
		if (!t || span.to <= span.from) return null;
		const at = (secs: number) => t.intercept + (t.per_ten_minutes / 600) * secs;
		return { x1: x(span.from), y1: y(at(span.from)), x2: x(span.to), y2: y(at(span.to)) };
	});
	const pct = (v: number | undefined | null) => (v == null ? '—' : `${Math.round(v)}%`);
</script>

{#if !links || !a}
	<Skeleton lines={5} />
{:else}
	<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3" aria-labelledby="links-h">
		<div class="flex flex-wrap items-center gap-2">
			<h3 class="m-0 text-base flex-1" id="links-h">Set beside marks from</h3>
			{#if !editing && links.options.length}<button class="btn btn-sm" onclick={() => (editing = true)}><Icon name="pencil" size={14} />Change</button>{/if}
		</div>
		{#if !links.options.length}
			<p class="small muted m-0">This topic has no quizzes or tutorials yet. Add the lesson’s quiz or tutorial to the topic, then link it here.</p>
		{:else if editing}
			<ul class="opts" aria-label="Quizzes and tutorials of this topic">
				{#each links.options as o (key(o))}
					<li><label class="flex items-center gap-2"><input type="checkbox" class="checkbox checkbox-sm" checked={chosen.includes(key(o))}
						onchange={(e) => (chosen = e.currentTarget.checked ? [...chosen, key(o)] : chosen.filter((k) => k !== key(o)))} />
						<span class="badge badge-soft badge-sm">{o.kind === 'quiz' ? 'Quiz' : 'Tutorial'}</span>{o.title}</label></li>
				{/each}
			</ul>
			<div class="flex gap-2"><button class="btn btn-sm btn-primary" onclick={saveLinks}>Save</button>{#if links.linked.length}<button class="btn btn-sm btn-ghost" onclick={() => { editing = false; chosen = links!.linked.map(key); }}>Cancel</button>{/if}</div>
			<p class="small muted m-0">Students’ best attempt at each quiz and their marked tutorial answers are added up into one score.</p>
		{:else}
			<p class="m-0">{links.linked.map((l) => `${l.title} (${l.kind})`).join(', ')}</p>
		{/if}
	</section>

	{#if links.linked.length}
		<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3" aria-labelledby="scatter-h">
			<h3 class="m-0 text-base" id="scatter-h">Time on the slides against score</h3>
			{#if !a.points.length}
				<p class="small muted m-0">Nothing to set side by side yet: no student has both viewed the slides and been marked on what’s linked.</p>
			{:else}
				{#if a.trend}<p class="m-0">{trendWords(a.trend, span.from, span.to, duration)}</p>{/if}
				<div class="chart">
					<svg viewBox="0 0 {W} {H}" role="img" aria-labelledby="scatter-h scatter-d">
						<desc id="scatter-d">{count(a.points.length, 'student')}: time on the slides across, score up. The table below has the same numbers.</desc>
						{#each [0, 25, 50, 75, 100] as g (g)}
							<line class="grid" x1={L} x2={W - R} y1={y(g)} y2={y(g)} />
							<text class="tick" x={L - 8} y={y(g) + 4} text-anchor="end">{g}%</text>
						{/each}
						{#each xTicks as t (t)}
							<text class="tick" x={x(t)} y={H - B + 18} text-anchor="middle">{t ? duration(t) : '0'}</text>
						{/each}
						<text class="tick" x={(L + W - R) / 2} y={H - 4} text-anchor="middle">time on the slides</text>
						<text class="tick" x={L - 8} y={T - 18} text-anchor="end">score</text>
						<clipPath id="plot-{id}"><rect x={L} y={T} width={W - L - R} height={H - T - B} /></clipPath>
						{#if trendLine}
							<line class="trend" {...trendLine} clip-path="url(#plot-{id})" />
						{/if}
						{#each a.points as p, i (p.user_id)}
							<!-- Hover detail; the table below gives the same to keyboards and screen readers. -->
							<g class="pt" class:on={hover === i} aria-hidden="true" onpointerenter={() => (hover = i)} onpointerleave={() => (hover = null)}>
								<circle class="hit" cx={x(p.seconds)} cy={y(p.score_pct)} r="12" />
								<circle class="dot" cx={x(p.seconds)} cy={y(p.score_pct)} r="5" />
							</g>
						{/each}
					</svg>
					{#if hover !== null && a.points[hover]}
						{@const p = a.points[hover]}
						<div class="tip" role="status" style="left:{(x(p.seconds) / W) * 100}%; top:{(y(p.score_pct) / H) * 100}%">
							<strong>{p.name}</strong><br />{duration(p.seconds)} · {count(p.seen, 'slide')} · <strong>{pct(p.score_pct)}</strong>
						</div>
					{/if}
				</div>
				<p class="small muted m-0">
					{#if a.not_viewed}{count(a.not_viewed, 'student')} with marks never opened the slides. {/if}
					{#if a.no_score}{count(a.no_score, 'student')} viewed the slides but have no marks yet. {/if}
					{#if trendLine}The grey line is the trend.{/if} Spending longer and scoring higher can go together without one causing the other.
				</p>
				<details>
					<summary class="small">As a table</summary>
					<table class="table table-sm">
						<thead><tr><th>Student</th><th class="text-right">Time</th><th class="text-right">Slides</th><th class="text-right">Score</th></tr></thead>
						<tbody>{#each a.points as p (p.user_id)}<tr><td>{p.name}</td><td class="text-right tabular">{duration(p.seconds)}</td><td class="text-right tabular">{p.seen}</td><td class="text-right tabular">{pct(p.score_pct)}</td></tr>{/each}</tbody>
					</table>
				</details>
			{/if}
		</section>

		{#if a.points.length}
			<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap" aria-labelledby="split-h">
				<h3 class="m-0 text-base" id="split-h">Slide by slide</h3>
				<p class="small muted mt-1 mb-2">For each slide: the score of students who spent at least the median time on it, and of those who spent less or skipped it.</p>
				<table class="table table-sm">
					<thead><tr><th>Slide</th><th class="text-right">Median time</th><th class="text-right">Longer: score</th><th class="text-right">Less: score</th><th class="text-right">Skipped</th></tr></thead>
					<tbody>
						{#each a.slides as s (s.n)}
							<tr>
								<td>{s.n}{#if s.text}<span class="small muted">{' · '}{s.text.slice(0, 50)}</span>{/if}</td>
								<td class="text-right tabular">{s.median_seconds ? duration(s.median_seconds) : '—'}</td>
								<td class="text-right tabular">{pct(s.longer_pct)} <span class="small muted">({s.longer_n})</span></td>
								<td class="text-right tabular">{pct(s.shorter_pct)} <span class="small muted">({s.shorter_n})</span></td>
								<td class="text-right tabular">{s.skipped || '—'}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</section>
		{/if}
	{/if}

	<section class="card card-border bg-base-100 shadow-sm p-4 flex flex-wrap items-center gap-2">
		<span class="flex-1">The viewing records, one row per student, with each slide’s time{links.linked.length ? ' and the linked score' : ''}.</span>
		<a class="btn btn-sm" href={`/api/teacher/presentations/${id}/report.csv`} download><Icon name="download" size={14} />CSV</a>
		<SaveToDrive kind="presentation" {id} size="btn-sm" />
	</section>
{/if}

<style>
	.opts { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
	.chart { position: relative; }
	.chart svg { width: 100%; height: auto; display: block; overflow: visible; }
	.grid { stroke: var(--color-base-300); stroke-width: 1; }
	.tick { fill: var(--color-muted, currentColor); font-size: 11px; }
	.trend { stroke: color-mix(in oklab, var(--color-base-content) 55%, transparent); stroke-width: 2; }
	.dot { fill: var(--cat-1); stroke: var(--color-base-100); stroke-width: 2; }
	.hit { fill: transparent; }
	.pt { cursor: default; outline: none; }
	.pt.on .dot { r: 7; }
	.tip { position: absolute; transform: translate(-50%, calc(-100% - 12px)); background: var(--color-base-100); border: 1px solid var(--color-base-300);
		border-radius: 0.5rem; padding: 0.35rem 0.55rem; font-size: 0.8rem; white-space: nowrap; box-shadow: 0 4px 12px rgb(0 0 0 / 0.12); pointer-events: none; }
</style>
