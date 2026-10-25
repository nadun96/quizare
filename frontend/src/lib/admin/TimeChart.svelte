<script lang="ts">
	// Figures over time with one unit, so one axis (D-82). A legend names the
	// series (each also labelled at its line's end); an optional reference
	// line marks a limit. A crosshair and tooltip follow the pointer or the
	// arrow keys, and a table gives the same figures.
	type Series = { key: string; label: string; color: string };
	type Point = { at: string; values: Record<string, number | undefined> };
	let {
		points,
		series,
		fmt,
		label,
		ref
	}: { points: Point[]; series: Series[]; fmt: (n: number) => string; label: string; ref?: { value: number; label: string } } = $props();

	let width = $state(600);
	const W = $derived(Math.max(260, width));
	const H = 200;
	const pad = { l: 72, r: 84, t: 14, b: 26 };
	const iw = $derived(W - pad.l - pad.r);
	const ih = H - pad.t - pad.b;

	const t0 = $derived(points.length ? Date.parse(points[0].at) : 0);
	const t1 = $derived(points.length ? Date.parse(points[points.length - 1].at) : 1);
	const top = $derived(Math.max(1e-9, ref?.value ?? 0, ...points.flatMap((p) => series.map((s) => p.values[s.key] ?? 0))) * 1.1);
	const x = (at: string) => pad.l + (t1 === t0 ? iw / 2 : ((Date.parse(at) - t0) / (t1 - t0)) * iw);
	const y = (v: number) => pad.t + ih - (v / top) * ih;
	const paths = $derived(
		series.map((s) => {
			let d = '';
			let pen = false;
			for (const p of points) {
				const v = p.values[s.key];
				if (v == null) {
					pen = false;
					continue;
				}
				d += `${pen ? 'L' : 'M'}${x(p.at).toFixed(1)},${y(v).toFixed(1)}`;
				pen = true;
			}
			return { ...s, d };
		})
	);
	// Labels at the lines' ends, nudged apart so they don't overlap.
	const ends = $derived.by(() => {
		const last = points[points.length - 1];
		if (!last) return [];
		const out = series.filter((s) => last.values[s.key] != null).map((s) => ({ ...s, y: y(last.values[s.key]!) })).sort((a, b) => a.y - b.y);
		for (let i = 1; i < out.length; i++) out[i].y = Math.max(out[i].y, out[i - 1].y + 13);
		return out;
	});
	const ticks = $derived([0, 0.5, 1].map((f) => (top / 1.1) * f));

	let active = $state<number | null>(null);
	let svg = $state<SVGSVGElement>();
	function onMove(e: PointerEvent) {
		if (!svg || !points.length) return;
		const r = svg.getBoundingClientRect();
		const px = ((e.clientX - r.left) / r.width) * W;
		let best = 0;
		for (let i = 1; i < points.length; i++) if (Math.abs(x(points[i].at) - px) < Math.abs(x(points[best].at) - px)) best = i;
		active = best;
	}
	function onKey(e: KeyboardEvent) {
		if (!points.length || (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft')) return;
		e.preventDefault();
		const step = e.key === 'ArrowRight' ? 1 : -1;
		active = Math.min(points.length - 1, Math.max(0, (active ?? (step > 0 ? -1 : points.length)) + step));
	}
	const span = $derived(t1 - t0);
	const when = (at: string) =>
		new Date(at).toLocaleString(undefined, span > 36 * 3600_000 ? { weekday: 'short', hour: '2-digit', minute: '2-digit' } : { hour: '2-digit', minute: '2-digit' });
</script>

<div class="vstack gap-1">
	{#if series.length > 1}
		<ul class="legend small" aria-label="Legend">
			{#each series as s (s.key)}<li><span class="swatch" style="background: {s.color}"></span>{s.label}</li>{/each}
			{#if ref}<li><span class="swatch ref"></span>{ref.label}</li>{/if}
		</ul>
	{/if}
	<div class="relative" bind:clientWidth={width}>
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<svg
			bind:this={svg}
			viewBox="0 0 {W} {H}"
			width={W}
			height={H}
			class="chart"
			role="img"
			tabindex="0"
			aria-label="{label}. Use the arrow keys to read each point."
			onpointermove={onMove}
			onpointerleave={() => (active = null)}
			onkeydown={onKey}
			onblur={() => (active = null)}
		>
			{#each ticks as t (t)}
				<line x1={pad.l} x2={pad.l + iw} y1={y(t)} y2={y(t)} class="grid" />
				<text x={pad.l - 8} y={y(t)} class="tick" text-anchor="end" dominant-baseline="middle">{fmt(t)}</text>
			{/each}
			{#if points.length}
				<text x={pad.l} y={H - 6} class="tick" text-anchor="start">{when(points[0].at)}</text>
				{#if points.length > 1}<text x={pad.l + iw} y={H - 6} class="tick" text-anchor="end">{when(points[points.length - 1].at)}</text>{/if}
			{/if}
			{#if ref}<line x1={pad.l} x2={pad.l + iw} y1={y(ref.value)} y2={y(ref.value)} class="refline" />{/if}
			{#each paths as p (p.key)}
				{#if p.d}<path d={p.d} class="line" style="stroke: {p.color}" />{/if}
				<!-- A point with no neighbour draws no line: show it as a dot. -->
				{#each points as pt, i (pt.at)}
					{@const v = pt.values[p.key]}
					{#if v != null && points[i - 1]?.values[p.key] == null && points[i + 1]?.values[p.key] == null}
						<circle cx={x(pt.at)} cy={y(v)} r="3.5" class="dot" style="fill: {p.color}" />
					{/if}
				{/each}
			{/each}
			{#each ends as e (e.key)}
				<text x={pad.l + iw + 6} y={e.y} class="tick end" dominant-baseline="middle">{e.label}</text>
			{/each}
			{#if active !== null && points[active]}
				<line x1={x(points[active].at)} x2={x(points[active].at)} y1={pad.t} y2={pad.t + ih} class="cross" />
				{#each series as s (s.key)}
					{@const v = points[active].values[s.key]}
					{#if v != null}<circle cx={x(points[active].at)} cy={y(v)} r="4.5" class="dot" style="fill: {s.color}" />{/if}
				{/each}
			{/if}
		</svg>
		{#if active !== null && points[active]}
			<div class="tip small" style="left: {(x(points[active].at) / W) * 100}%" aria-live="polite">
				<span class="muted">{when(points[active].at)}</span>
				{#each series as s (s.key)}
					{@const v = points[active].values[s.key]}
					{#if v != null}<br /><span class="swatch" style="background: {s.color}"></span>{s.label} <strong class="tabular">{fmt(v)}</strong>{/if}
				{/each}
			</div>
		{/if}
	</div>
	<details class="small">
		<summary>Show as a table</summary>
		<table class="table table-sm">
			<thead><tr><th>Time</th>{#each series as s (s.key)}<th class="text-right">{s.label}</th>{/each}</tr></thead>
			<tbody>
				{#each points as p (p.at)}
					<tr><td>{when(p.at)}</td>{#each series as s (s.key)}<td class="tabular text-right">{p.values[s.key] == null ? '–' : fmt(p.values[s.key]!)}</td>{/each}</tr>
				{/each}
			</tbody>
		</table>
	</details>
</div>

<style>
	.chart { overflow: visible; touch-action: pan-y; }
	.chart:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 4px; border-radius: 4px; }
	.grid { stroke: var(--color-base-300); stroke-width: 1; }
	.tick { fill: var(--muted, currentColor); font-size: 11px; opacity: 0.8; }
	.tick.end { fill: var(--color-base-content); opacity: 0.9; }
	.line { fill: none; stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
	.refline { stroke: var(--color-base-content); stroke-width: 1; stroke-dasharray: 4 4; opacity: 0.5; }
	.dot { stroke: var(--color-base-100); stroke-width: 2; }
	.cross { stroke: var(--color-base-content); stroke-width: 1; opacity: 0.35; }
	.legend { display: flex; flex-wrap: wrap; gap: 0.25rem 1rem; list-style: none; margin: 0; padding: 0; }
	.legend li { display: inline-flex; align-items: center; gap: 0.35rem; }
	.swatch { display: inline-block; width: 0.75rem; height: 0.2rem; border-radius: 2px; vertical-align: middle; margin-right: 0.25rem; }
	.legend .swatch { margin-right: 0; }
	.swatch.ref { background: transparent; border-top: 2px dashed var(--color-base-content); opacity: 0.6; height: 0; }
	.tip { position: absolute; top: 0; transform: translateX(-50%); pointer-events: none; background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: 8px; padding: 0.3rem 0.5rem; box-shadow: 0 2px 8px rgb(0 0 0 / 0.12); white-space: nowrap; z-index: 1; }
</style>
