<script lang="ts">
	// A percentage over a run of quizzes (D-105), 0–100 on one axis: the
	// series that is the point in the accent hue with its dots, the context
	// series (the class average) in gray: the emphasis form. A crosshair
	// snaps to the nearest quiz on hover or with the arrow keys, and the
	// tooltip lists both; a table gives every value without hovering.
	import { draw } from '../ui/motion';
	import { t } from '../i18n/i18n.svelte';
	type Point = { label: string; sub: string; value: number; context: number | null; note?: string };
	let { points, name, contextName, title }: { points: Point[]; name: string; contextName: string; title: string } = $props();

	let width = $state(560);
	const W = $derived(Math.max(260, width));
	const H = 220;
	const pad = { l: 40, r: 16, t: 14, b: 26 };
	const iw = $derived(W - pad.l - pad.r);
	const ih = H - pad.t - pad.b;
	const x = (i: number) => pad.l + (points.length < 2 ? iw / 2 : (i / (points.length - 1)) * iw);
	const y = (v: number) => pad.t + ih - (v / 100) * ih;
	const path = (vals: (number | null)[]) => {
		let d = '';
		let pen = false;
		vals.forEach((v, i) => {
			if (v === null) return void (pen = false);
			d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`;
			pen = true;
		});
		return d;
	};
	const main = $derived(path(points.map((p) => p.value)));
	const ctx = $derived(path(points.map((p) => p.context)));
	const area = $derived(points.length > 1 ? `${main}L${x(points.length - 1)},${y(0)}L${x(0)},${y(0)}Z` : '');
	const hasContext = $derived(points.some((p) => p.context !== null));
	const last = $derived(points.length ? points[points.length - 1] : null);

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
		if (!points.length || (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft')) return;
		e.preventDefault();
		const s = e.key === 'ArrowRight' ? 1 : -1;
		active = Math.min(points.length - 1, Math.max(0, (active ?? (s > 0 ? -1 : points.length)) + s));
	}
	const tipLeft = $derived(active === null ? 0 : Math.min(Math.max((x(active) / W) * 100, 12), 88));
</script>

<div class="vstack gap-2">
	<div class="legend small" aria-hidden="true">
		<span><i class="key accent"></i>{name}</span>
		{#if hasContext}<span><i class="key context"></i>{contextName}</span>{/if}
	</div>
	<div class="relative" bind:clientWidth={width}>
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<svg bind:this={svg} viewBox="0 0 {W} {H}" width={W} height={H} class="chart" role="img" tabindex="0"
			aria-label="{title}: {points.length} quizzes{last ? `, latest ${last.value}%` : ''}. Use the arrow keys to read each one."
			use:draw={points}
			onpointermove={onMove} onpointerleave={() => (active = null)} onkeydown={onKey} onblur={() => (active = null)}>
			{#each [0, 50, 100] as t (t)}
				<line x1={pad.l} x2={W - pad.r} y1={y(t)} y2={y(t)} class={t === 0 ? 'axis' : 'grid'} />
				<text x={pad.l - 8} y={y(t)} class="tick" text-anchor="end" dominant-baseline="middle">{t}%</text>
			{/each}
			{#if points.length}
				<text x={x(0)} y={H - 6} class="tick" text-anchor={points.length > 1 ? 'start' : 'middle'}>{points[0].sub}</text>
				{#if points.length > 1}<text x={x(points.length - 1)} y={H - 6} class="tick" text-anchor="end">{points[points.length - 1].sub}</text>{/if}
			{/if}
			{#if area}<path d={area} class="wash" data-fade />{/if}
			{#if ctx}<path d={ctx} class="line context" data-draw />{/if}
			{#if main}<path d={main} class="line accent" data-draw />{/if}
			{#each points as p, i (i)}
				<circle cx={x(i)} cy={y(p.value)} r={active === i ? 5.5 : 4} class="dot" data-pop />
			{/each}
			{#if active !== null && points[active]}
				<line x1={x(active)} x2={x(active)} y1={pad.t} y2={pad.t + ih} class="cross" />
			{/if}
		</svg>
		{#if active !== null && points[active]}
			{@const p = points[active]}
			<div class="tip small" style="left: {tipLeft}%" aria-live="polite">
				<p class="m-0 font-semibold truncate">{p.label}</p>
				<p class="m-0 muted">{p.sub}{p.note ? ' · ' + p.note : ''}</p>
				<p class="m-0 row-k"><i class="key accent"></i><strong>{p.value}%</strong><span class="muted">{name}</span></p>
				{#if p.context !== null}<p class="m-0 row-k"><i class="key context"></i><strong>{p.context}%</strong><span class="muted">{contextName}</span></p>{/if}
			</div>
		{/if}
	</div>
	<details class="small">
		<summary>{t('common.showTable')}</summary>
		<div class="table-wrap">
			<table class="table table-sm">
				<thead><tr><th>Quiz</th><th>When</th><th>{name}</th>{#if hasContext}<th>{contextName}</th>{/if}</tr></thead>
				<tbody>{#each points as p, i (i)}<tr><td>{p.label}</td><td>{p.sub}</td><td class="tabular">{p.value}%{p.note ? ' · ' + p.note : ''}</td>{#if hasContext}<td class="tabular">{p.context ?? '—'}{p.context !== null ? '%' : ''}</td>{/if}</tr>{/each}</tbody>
			</table>
		</div>
	</details>
</div>

<style>
	.chart { overflow: visible; touch-action: pan-y; display: block; }
	.chart:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 4px; border-radius: 4px; }
	.grid { stroke: var(--viz-grid); stroke-width: 1; }
	.axis { stroke: var(--viz-axis); stroke-width: 1; }
	.tick { fill: currentColor; opacity: 0.6; font-size: 11px; font-variant-numeric: tabular-nums; }
	.line { fill: none; stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
	.line.accent { stroke: var(--viz-accent); }
	.line.context { stroke: var(--viz-context); }
	.wash { fill: var(--viz-accent-wash); }
	.dot { fill: var(--viz-accent); stroke: var(--color-base-100); stroke-width: 2; transition: r 120ms; }
	.cross { stroke: var(--color-base-content); stroke-width: 1; opacity: 0.3; }
	.legend { display: flex; gap: 1rem; flex-wrap: wrap; color: color-mix(in oklab, var(--color-base-content) 75%, transparent); }
	.legend span, .row-k { display: inline-flex; align-items: center; gap: 0.4rem; }
	.key { display: inline-block; width: 14px; height: 2px; border-radius: 1px; }
	.key.accent { background: var(--viz-accent); height: 3px; }
	.key.context { background: var(--viz-context); }
	.tip { position: absolute; top: 0; transform: translateX(-50%); pointer-events: none; background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: 10px; padding: 0.45rem 0.6rem; box-shadow: 0 6px 18px rgb(0 0 0 / 0.14); min-width: 10rem; max-width: 15rem; display: grid; gap: 0.1rem; z-index: 2; }
</style>
