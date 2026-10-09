<script lang="ts">
	// The database's size over the last 30 days (PL-FR-04): one series, so no
	// legend; the heading names it. A crosshair and tooltip follow the
	// pointer or the arrow keys, and a table gives the same figures.
	import { fmtBytes } from '../staff';

	type Point = { day: string; db_bytes: number };
	let { points }: { points: Point[] } = $props();

	// Drawn at its real width in pixels, so text stays at its size.
	let width = $state(600);
	const W = $derived(Math.max(240, width));
	const H = 180;
	const pad = { l: 64, r: 12, t: 12, b: 26 };
	const iw = $derived(W - pad.l - pad.r);
	const ih = H - pad.t - pad.b;

	const max = $derived(Math.max(1, ...points.map((p) => p.db_bytes)) * 1.1);
	const x = (i: number) => pad.l + (points.length < 2 ? iw / 2 : (i / (points.length - 1)) * iw);
	const y = (v: number) => pad.t + ih - (v / max) * ih;
	const line = $derived(points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.db_bytes).toFixed(1)}`).join(''));
	const area = $derived(points.length > 1 ? `${line}L${x(points.length - 1)},${pad.t + ih}L${x(0)},${pad.t + ih}Z` : '');
	const ticks = $derived([0, 0.5, 1].map((f) => (max / 1.1) * f));

	let active = $state<number | null>(null);
	let svg = $state<SVGSVGElement>();
	function onMove(e: PointerEvent) {
		if (!svg || !points.length) return;
		const r = svg.getBoundingClientRect();
		const px = ((e.clientX - r.left) / r.width) * W;
		let best = 0;
		for (let i = 1; i < points.length; i++) if (Math.abs(x(i) - px) < Math.abs(x(best) - px)) best = i;
		active = best;
	}
	function onKey(e: KeyboardEvent) {
		if (!points.length) return;
		if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
			e.preventDefault();
			const step = e.key === 'ArrowRight' ? 1 : -1;
			active = Math.min(points.length - 1, Math.max(0, (active ?? (step > 0 ? -1 : points.length)) + step));
		}
	}
	const day = (d: string) => new Date(d + 'T00:00:00').toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
</script>

<!-- A div, not <figure>: daisyUI lays out figures inside cards as centred rows. -->
<div class="vstack">
	<div class="relative" bind:clientWidth={width}>
		<!-- The chart is focusable so keyboard users can read each day with the arrow keys. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<svg
			bind:this={svg}
			viewBox="0 0 {W} {H}"
			width={W}
			height={H}
			class="chart"
			role="img"
			tabindex="0"
			aria-label="Database size over the last {points.length} days{points.length ? `, now ${fmtBytes(points[points.length - 1].db_bytes)}` : ''}. Use the arrow keys to read each day."
			onpointermove={onMove}
			onpointerleave={() => (active = null)}
			onkeydown={onKey}
			onblur={() => (active = null)}
		>
			{#each ticks as t (t)}
				<line x1={pad.l} x2={W - pad.r} y1={y(t)} y2={y(t)} class="grid" />
				<text x={pad.l - 8} y={y(t)} class="tick" text-anchor="end" dominant-baseline="middle">{fmtBytes(t)}</text>
			{/each}
			{#if points.length}
				<text x={x(0)} y={H - 6} class="tick" text-anchor={points.length > 1 ? 'start' : 'middle'}>{day(points[0].day)}</text>
				{#if points.length > 1}<text x={x(points.length - 1)} y={H - 6} class="tick" text-anchor="end">{day(points[points.length - 1].day)}</text>{/if}
			{/if}
			{#if area}<path d={area} class="area" />{/if}
			{#if points.length > 1}<path d={line} class="line" />{/if}
			{#if points.length === 1}<circle cx={x(0)} cy={y(points[0].db_bytes)} r="4" class="dot" />{/if}
			{#if active !== null && points[active]}
				<line x1={x(active)} x2={x(active)} y1={pad.t} y2={pad.t + ih} class="cross" />
				<circle cx={x(active)} cy={y(points[active].db_bytes)} r="4.5" class="dot" />
			{/if}
		</svg>
		{#if active !== null && points[active]}
			<div class="tip small" style="left: {(x(active) / W) * 100}%" aria-live="polite">
				<strong>{fmtBytes(points[active].db_bytes)}</strong><br /><span class="muted">{day(points[active].day)}</span>
			</div>
		{/if}
	</div>
	<details class="small">
		<summary>Show as a table</summary>
		<table class="table table-sm">
			<thead><tr><th>Day</th><th>Database</th></tr></thead>
			<tbody>{#each points as p (p.day)}<tr><td>{day(p.day)}</td><td class="tabular">{fmtBytes(p.db_bytes)}</td></tr>{/each}</tbody>
		</table>
	</details>
</div>

<style>
	.chart { overflow: visible; touch-action: pan-y; }
	.chart:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 4px; border-radius: 4px; }
	.grid { stroke: var(--color-base-300); stroke-width: 1; }
	.tick { fill: var(--muted, currentColor); font-size: 11px; opacity: 0.8; }
	.line { fill: none; stroke: var(--cat-1); stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
	.area { fill: var(--cat-1); opacity: 0.12; }
	.dot { fill: var(--cat-1); stroke: var(--color-base-100); stroke-width: 2; }
	.cross { stroke: var(--color-base-content); stroke-width: 1; opacity: 0.35; }
	.tip { position: absolute; top: 0; transform: translateX(-50%); pointer-events: none; background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: 8px; padding: 0.3rem 0.5rem; box-shadow: 0 2px 8px rgb(0 0 0 / 0.12); white-space: nowrap; }
</style>
