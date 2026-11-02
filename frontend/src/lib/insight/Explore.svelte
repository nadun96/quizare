<script lang="ts">
	// Explore (D-120): pick what to measure, what to break it down by, and
	// which classrooms and dates; the answer comes back as a chart and a
	// table (or a grid for two breakdowns), with CSV. The query lives in the
	// address, so a view can be bookmarked or sent to a colleague.
	import { replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '../api';
	import TrendChart from '../dashboard/TrendChart.svelte';
	import RankedBars from '../dashboard/RankedBars.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { cellText, dateLabel, fmt, fromParams, pivot, range, RANGES, toCSV, toParams, type Catalog, type Fact, type Query, type Result } from './insight';

	let catalog = $state<Catalog | null>(null);
	let q = $state<Query>(fromParams(page.url.searchParams));
	let result = $state<Result | null>(null);
	let error = $state('');
	let busy = $state(false);
	let view = $state<'table' | 'grid'>('table');
	let gridMeasure = $state('');
	let rangeKey = $state('');

	api.get<Catalog>('/api/insight/catalog').then((c) => {
		catalog = c;
		// A shared link from a teacher may name what an admin can't break down by.
		q.dimensions = q.dimensions.filter((d) => c.dimensions.some((x) => x.key === d));
	});

	const fact = $derived<Fact>((catalog?.measures.find((m) => q.measures.includes(m.key))?.fact ?? 'activity') as Fact);
	const dims = $derived((catalog?.dimensions ?? []).filter((d) => d.facts.includes(fact)));
	const measureList = $derived((catalog?.measures ?? []).filter((m) => m.fact === fact));
	const unitOf = (k: string) => catalog?.measures.find((m) => m.key === k)?.unit;

	function toggleMeasure(key: string, f: Fact) {
		if (f !== fact) {
			// The other table: start over with this measure, and keep only the breakdowns it allows.
			q.measures = [key];
			q.dimensions = q.dimensions.filter((d) => catalog?.dimensions.find((x) => x.key === d)?.facts.includes(f));
			for (const k of Object.keys(q.filters)) if (!catalog?.dimensions.find((x) => x.key === k)?.facts.includes(f)) delete q.filters[k];
			return;
		}
		q.measures = q.measures.includes(key) ? q.measures.filter((m) => m !== key) : [...q.measures, key];
	}
	function setDim(i: number, key: string) {
		const d = [...q.dimensions];
		if (key) d[i] = key;
		else d.splice(i, 1);
		q.dimensions = d.filter((x, j) => x && d.indexOf(x) === j).slice(0, 3);
	}
	function setRange(key: string) {
		rangeKey = key;
		if (key) Object.assign(q, range(key));
	}
	function toggleFilter(dim: string, id: string) {
		const cur = q.filters[dim] ?? [];
		q.filters = { ...q.filters, [dim]: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id] };
	}

	// Run whenever the query changes, a moment after the last change.
	let timer: ReturnType<typeof setTimeout> | undefined;
	let seq = 0;
	$effect(() => {
		const body = $state.snapshot(q);
		if (!catalog) return;
		clearTimeout(timer);
		timer = setTimeout(async () => {
			// Into the address, keeping what else is there (the admin console's tool).
			const p = new URLSearchParams(location.search);
			for (const k of [...p.keys()]) if (['m', 'd', 'g', 'from', 'to', 'totals'].includes(k) || k.startsWith('f.')) p.delete(k);
			for (const [k, v] of toParams(body)) p.set(k, v);
			try {
				replaceState('?' + p.toString(), page.state);
			} catch {
				/* before the router starts */
			}
			if (!body.measures.length) {
				result = null;
				error = 'Choose at least one measure.';
				return;
			}
			const mine = ++seq;
			busy = true;
			try {
				const r = await api.post<Result>('/api/insight/query', body);
				if (mine === seq) {
					result = r;
					error = '';
				}
			} catch (e) {
				if (mine === seq) error = e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : 'The query failed';
			} finally {
				if (mine === seq) busy = false;
			}
		}, 250);
		return () => clearTimeout(timer);
	});

	const dimCols = $derived(result?.columns.filter((c) => c.kind === 'dimension') ?? []);
	const measCols = $derived(result?.columns.filter((c) => c.kind === 'measure') ?? []);
	const rows0 = $derived(result?.rows.filter((r) => r.level === 0) ?? []);
	const hiddenCount = $derived(result?.rows.filter((r) => r.hidden).length ?? 0);
	// The chart: one breakdown, the first measure.
	const chart = $derived.by(() => {
		if (!result || dimCols.length !== 1 || !measCols.length) return null;
		const d = dimCols[0].key;
		const m = measCols[0];
		const pts = rows0.filter((r) => !r.hidden && r.values[m.key] != null);
		if (!pts.length) return null;
		if (d === 'date' && m.unit === '%') {
			return { kind: 'line' as const, m, points: pts.map((r) => ({ label: dateLabel(r.dims![d].label, result!.grain), sub: dateLabel(r.dims![d].label, result!.grain), value: r.values[m.key]!, context: null })) };
		}
		const seen = new Map<string, number>();
		const bars = pts.slice(0, 40).map((r) => {
			let label = cellText(d, r.dims![d], result!.grain);
			const n = (seen.get(label) ?? 0) + 1;
			seen.set(label, n);
			if (n > 1) label += ` (${n})`;
			return { label, value: r.values[m.key]!, note: '' };
		});
		const max = m.unit === '%' ? 100 : Math.max(...bars.map((b) => b.value), 1);
		return { kind: 'bars' as const, m, bars, max, more: pts.length - bars.length };
	});
	$effect(() => {
		if (measCols.length && !measCols.some((c) => c.key === gridMeasure)) gridMeasure = measCols[0].key;
	});
	const grid = $derived(result && dimCols.length === 2 ? pivot(result, gridMeasure && measCols.some((c) => c.key === gridMeasure) ? gridMeasure : (measCols[0]?.key ?? '')) : null);

	function download() {
		if (!result) return;
		const a = document.createElement('a');
		a.href = URL.createObjectURL(new Blob([toCSV(result)], { type: 'text/csv;charset=utf-8' }));
		a.download = `explore-${result.from}-to-${result.to}.csv`;
		a.click();
		setTimeout(() => URL.revokeObjectURL(a.href), 1000);
	}
	async function copyLink() {
		try {
			await navigator.clipboard.writeText(location.href);
		} catch {
			/* the address bar has it too */
		}
	}
</script>

<div class="vstack">
	{#if !catalog}
		<Skeleton lines={6} />
	{:else}
		<div class="card card-border bg-base-100 p-4 vstack gap-4">
			<fieldset class="vstack gap-2">
				<legend class="small font-semibold">Measure</legend>
				<div class="flex flex-wrap gap-2" role="group" aria-label="Activity measures">
					<span class="small muted self-center">Activity</span>
					{#each catalog.measures.filter((m) => m.fact === 'activity') as m (m.key)}
						<button type="button" class="chip" class:on={q.measures.includes(m.key)} aria-pressed={q.measures.includes(m.key)} title={m.help ?? ''} onclick={() => toggleMeasure(m.key, m.fact)}>{m.label}</button>
					{/each}
				</div>
				<div class="flex flex-wrap gap-2" role="group" aria-label="Question measures">
					<span class="small muted self-center">Questions</span>
					{#each catalog.measures.filter((m) => m.fact === 'answers') as m (m.key)}
						<button type="button" class="chip" class:on={q.measures.includes(m.key)} aria-pressed={q.measures.includes(m.key)} title={m.help ?? ''} onclick={() => toggleMeasure(m.key, m.fact)}>{m.label}</button>
					{/each}
				</div>
			</fieldset>

			<div class="controls">
				{#each [0, 1, 2] as i (i)}
					{#if i <= q.dimensions.length}
						<label class="vstack gap-1">
							<span class="small font-semibold">{i === 0 ? 'Break down by' : i === 1 ? 'then by' : 'and by'}</span>
							<select class="select select-sm w-full" value={q.dimensions[i] ?? ''} onchange={(e) => setDim(i, e.currentTarget.value)}>
								<option value="">{i === 0 ? 'Nothing: one total' : '—'}</option>
								{#each dims as d (d.key)}<option value={d.key} disabled={q.dimensions.includes(d.key) && q.dimensions[i] !== d.key}>{d.label}</option>{/each}
							</select>
						</label>
					{/if}
				{/each}
				{#if q.dimensions.includes('date')}
					<label class="vstack gap-1">
						<span class="small font-semibold">Dates by</span>
						<select class="select select-sm w-full" bind:value={q.grain}>
							{#each catalog.grains as g (g)}<option value={g}>{g[0].toUpperCase() + g.slice(1)}</option>{/each}
						</select>
					</label>
				{/if}
			</div>

			<div class="controls">
				<label class="vstack gap-1">
					<span class="small font-semibold">Dates</span>
					<select class="select select-sm w-full" value={rangeKey} onchange={(e) => setRange(e.currentTarget.value)}>
						<option value="">From – to</option>
						{#each RANGES as r (r.key)}<option value={r.key}>{r.label}</option>{/each}
					</select>
				</label>
				<label class="vstack gap-1"><span class="small font-semibold">From</span><input type="date" class="input input-sm w-full" bind:value={q.from} oninput={() => (rangeKey = '')} /></label>
				<label class="vstack gap-1"><span class="small font-semibold">To</span><input type="date" class="input input-sm w-full" bind:value={q.to} oninput={() => (rangeKey = '')} /></label>
				<div class="vstack gap-1">
					<span class="small font-semibold">Classrooms</span>
					<details class="dropdown w-full">
						<summary class="select select-sm w-full">{(q.filters.classroom ?? []).length ? `${q.filters.classroom.length} chosen` : 'All'}</summary>
						<div class="dropdown-content z-30 mt-1 max-h-72 w-72 overflow-auto border border-base-300 bg-base-100 p-2 shadow">
							{#each catalog.classrooms as c (c.id)}
								<label class="flex items-center gap-2 py-1 font-normal"><input type="checkbox" class="checkbox checkbox-sm" checked={(q.filters.classroom ?? []).includes(c.id)} onchange={() => toggleFilter('classroom', c.id)} />{c.label}</label>
							{:else}
								<p class="m-0 small muted">No classrooms yet.</p>
							{/each}
						</div>
					</details>
				</div>
				{#if fact === 'activity'}
					<label class="vstack gap-1">
						<span class="small font-semibold">Activity</span>
						<select class="select select-sm w-full" value={(q.filters.kind ?? [])[0] ?? ''} onchange={(e) => (q.filters = { ...q.filters, kind: e.currentTarget.value ? [e.currentTarget.value] : [] })}>
							<option value="">Quizzes and tutorials</option><option value="quiz">Quizzes</option><option value="tutorial">Tutorials</option>
						</select>
					</label>
				{/if}
				<label class="flex items-center gap-2 self-end pb-1 font-normal"><input type="checkbox" class="toggle toggle-sm" bind:checked={q.totals} />Totals</label>
			</div>
			{#if catalog.staff}<p class="m-0 small muted"><Icon name="shield" size={13} /> As an admin you see groups, not single students; a group of fewer than {catalog.min_group} students is shown without its figures.</p>{/if}
		</div>

		{#if error}
			<p class="alert alert-soft alert-warning m-0">{error}</p>
		{:else if !result}
			<Skeleton lines={4} />
		{:else}
			<div class="flex flex-wrap items-center gap-2" class:opacity-60={busy}>
				<p class="m-0 grow small muted">
					{dateLabel(result.from, 'day')} – {dateLabel(result.to, 'day')}{result.loaded_at ? ` · data as of ${new Date(result.loaded_at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })}` : ' · the first load is still running'}
					{#if result.truncated} · only the first 5,000 rows{/if}
					{#if hiddenCount} · {hiddenCount} small {hiddenCount === 1 ? 'group' : 'groups'} hidden{/if}
				</p>
				{#if dimCols.length === 2}
					<div class="join" role="group" aria-label="Show as">
						<button class="btn btn-sm join-item" class:btn-active={view === 'table'} aria-pressed={view === 'table'} onclick={() => (view = 'table')}>Table</button>
						<button class="btn btn-sm join-item" class:btn-active={view === 'grid'} aria-pressed={view === 'grid'} onclick={() => (view = 'grid')}>Grid</button>
					</div>
				{/if}
				<button class="btn btn-sm" onclick={copyLink}><Icon name="link" size={15} />Copy link</button>
				<button class="btn btn-sm" onclick={download}><Icon name="download" size={15} />CSV</button>
			</div>

			{#if chart}
				<div class="card card-border bg-base-100 p-4 vstack gap-2">
					<h3 class="m-0 text-base">{chart.m.label} by {dimCols[0].label.toLowerCase()}</h3>
					{#if chart.kind === 'line'}
						<TrendChart title="{chart.m.label} by date" name={chart.m.label} contextName="" rowName="Date" noun="dates" points={chart.points} />
					{:else}
						<RankedBars title="{chart.m.label} by {dimCols[0].label}" rows={chart.bars} unit={chart.m.unit === '%' ? '%' : chart.m.unit ? ' ' + chart.m.unit : ''} max={chart.max} />
						{#if chart.more > 0}<p class="m-0 small muted">And {chart.more} more in the table.</p>{/if}
					{/if}
				</div>
			{/if}

			{#if grid && view === 'grid'}
				<div class="card card-border bg-base-100 p-4 vstack gap-2">
					{#if measCols.length > 1}
						<label class="flex items-center gap-2 small">Showing
							<select class="select select-sm w-auto" bind:value={gridMeasure}>{#each measCols as m (m.key)}<option value={m.key}>{m.label}</option>{/each}</select>
						</label>
					{/if}
					<div class="table-wrap">
						<table class="table table-sm">
							<thead><tr><th>{dimCols[0].label} \ {dimCols[1].label}</th>{#each grid.cols as c (c.id)}<th class="tabular">{cellText(grid.colDim, c, result.grain)}</th>{/each}</tr></thead>
							<tbody>
								{#each grid.rows as r (r.id)}
									<tr><th class="font-normal">{cellText(grid.rowDim, r, result.grain)}</th>{#each grid.cols as c (c.id)}{@const v = grid.at(r.id, c.id)}<td class="tabular">{v === undefined ? '' : fmt(v, unitOf(gridMeasure || measCols[0].key))}</td>{/each}</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			{:else}
				<div class="card card-border bg-base-100 p-4">
					<div class="table-wrap">
						<table class="table table-sm">
							<thead><tr>{#each result.columns as c (c.key)}<th class:num={c.kind === 'measure'}>{c.label}</th>{/each}</tr></thead>
							<tbody>
								{#each result.rows as r, i (i)}
									<tr class:sub={r.level === 1} class:total={r.level === 2}>
										{#each dimCols as c, j (c.key)}
											<td>{r.level === 2 ? (j === 0 ? 'Total' : '') : r.dims?.[c.key] ? cellText(c.key, r.dims[c.key], result.grain) : r.level === 1 ? 'Subtotal' : ''}</td>
										{/each}
										{#each measCols as c, j (c.key)}
											<td class="tabular num">{#if r.hidden}{#if j === 0}<span class="small muted" title="Groups of fewer than {result.min_group} students are not shown to staff">under {result.min_group} students</span>{/if}{:else}{fmt(r.values[c.key], c.unit)}{/if}</td>
										{/each}
									</tr>
								{:else}
									<tr><td colspan={result.columns.length} class="muted">Nothing in these dates.</td></tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			{/if}
		{/if}
	{/if}
</div>

<style>
	.chip { padding: 0.3rem 0.65rem; font-size: 0.85rem; border: 1px solid var(--color-base-300); background: var(--color-base-100); }
	.chip:hover { border-color: var(--color-primary); }
	.chip.on { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 14%, transparent); color: var(--color-primary); font-weight: 600; }
	.chip:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
	.controls { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr)); align-items: end; }
	.num { text-align: right; }
	tr.sub td { font-weight: 600; background: var(--color-base-200); }
	tr.total td { font-weight: 700; border-top: 2px solid var(--color-base-300); }
</style>
