<script lang="ts">
	// Columns on a 0–100 % axis (D-105), one hue, ≤ 24 px wide with a 4 px
	// rounded top and a square foot. Only the highest and the latest carry
	// their value; every column has its own hover and focus tooltip, and the
	// table gives them all.
	type Col = { label: string; sub: string; value: number; lines: string[] };
	let { cols, title, unit = '%' }: { cols: Col[]; title: string; unit?: string } = $props();

	let width = $state(560);
	const W = $derived(Math.max(260, width));
	const H = 220;
	const pad = { l: 40, r: 8, t: 22, b: 26 };
	const iw = $derived(W - pad.l - pad.r);
	const ih = H - pad.t - pad.b;
	const band = $derived(cols.length ? iw / cols.length : iw);
	const bw = $derived(Math.max(6, Math.min(24, band * 0.6)));
	const cx = (i: number) => pad.l + band * i + band / 2;
	const y = (v: number) => pad.t + ih - (Math.max(0, Math.min(100, v)) / 100) * ih;
	const top = $derived(cols.reduce((b, c, i) => (c.value > (cols[b]?.value ?? -1) ? i : b), 0));
	// The latest always; the highest too when it isn't the latest and its label has room.
	const labelled = $derived(new Set([cols.length - 1, ...(top !== cols.length - 1 && (cols.length - 1 - top) * band >= 40 ? [top] : [])]));

	// A rounded top, square at the baseline.
	function bar(i: number, v: number) {
		const x0 = cx(i) - bw / 2, y0 = y(v), base = y(0), r = Math.min(4, bw / 2, Math.max(0, base - y0));
		return `M${x0},${base}V${y0 + r}Q${x0},${y0} ${x0 + r},${y0}H${x0 + bw - r}Q${x0 + bw},${y0} ${x0 + bw},${y0 + r}V${base}Z`;
	}
	let active = $state<number | null>(null);
	const tipLeft = $derived(active === null ? 0 : Math.min(Math.max((cx(active) / W) * 100, 14), 86));
</script>

<div class="vstack gap-2">
	<div class="relative" bind:clientWidth={width}>
		<svg viewBox="0 0 {W} {H}" width={W} height={H} class="chart" role="group" aria-label={title}>
			{#each [0, 50, 100] as t (t)}
				<line x1={pad.l} x2={W - pad.r} y1={y(t)} y2={y(t)} class={t === 0 ? 'axis' : 'grid'} />
				<text x={pad.l - 8} y={y(t)} class="tick" text-anchor="end" dominant-baseline="middle">{t}{unit}</text>
			{/each}
			{#each cols as c, i (i)}
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<g class="col" class:on={active === i} tabindex="0" role="img" aria-label="{c.label}, {c.sub}: {c.value}{unit}. {c.lines.join('. ')}"
					onpointerenter={() => (active = i)} onpointerleave={() => (active = null)} onfocus={() => (active = i)} onblur={() => (active = null)}>
					<rect x={cx(i) - band / 2} y={pad.t} width={band} height={ih} class="hit" />
					{#if c.value > 0}<path d={bar(i, c.value)} class="bar" />{:else}<line x1={cx(i) - bw / 2} x2={cx(i) + bw / 2} y1={y(0)} y2={y(0)} class="zero" />{/if}
					{#if labelled.has(i)}<text x={cx(i)} y={y(c.value) - 6} class="val" text-anchor="middle">{c.value}{unit}</text>{/if}
				</g>
			{/each}
			{#if cols.length}
				<text x={cx(0)} y={H - 6} class="tick" text-anchor={cols.length > 1 ? 'start' : 'middle'} dx={cols.length > 1 ? -bw / 2 : 0}>{cols[0].sub}</text>
				{#if cols.length > 1}<text x={cx(cols.length - 1)} y={H - 6} class="tick" text-anchor="end" dx={bw / 2}>{cols[cols.length - 1].sub}</text>{/if}
			{/if}
		</svg>
		{#if active !== null && cols[active]}
			{@const c = cols[active]}
			<div class="tip small" style="left: {tipLeft}%" aria-hidden="true">
				<p class="m-0"><strong class="text-base">{c.value}{unit}</strong></p>
				<p class="m-0 font-semibold truncate">{c.label}</p>
				<p class="m-0 muted">{c.sub}</p>
				{#each c.lines as l (l)}<p class="m-0 muted">{l}</p>{/each}
			</div>
		{/if}
	</div>
	<details class="small">
		<summary>Show as a table</summary>
		<div class="table-wrap">
			<table class="table table-sm">
				<thead><tr><th>Quiz</th><th>When</th><th>Average</th><th>Details</th></tr></thead>
				<tbody>{#each cols as c, i (i)}<tr><td>{c.label}</td><td>{c.sub}</td><td class="tabular">{c.value}{unit}</td><td>{c.lines.join(' · ')}</td></tr>{/each}</tbody>
			</table>
		</div>
	</details>
</div>

<style>
	.chart { overflow: visible; display: block; }
	.grid { stroke: var(--viz-grid); stroke-width: 1; }
	.axis, .zero { stroke: var(--viz-axis); stroke-width: 1; }
	.zero { stroke-width: 2; }
	.tick { fill: currentColor; opacity: 0.6; font-size: 11px; font-variant-numeric: tabular-nums; }
	.val { fill: currentColor; font-size: 11px; font-weight: 600; }
	.hit { fill: transparent; }
	.bar { fill: var(--viz-accent); transition: opacity 120ms; }
	.col { outline: none; cursor: default; }
	.col:hover .bar, .col.on .bar { opacity: 0.8; }
	.col:focus-visible .hit { stroke: var(--color-primary); stroke-width: 2; rx: 6; }
	.tip { position: absolute; top: 0; transform: translateX(-50%); pointer-events: none; background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: 10px; padding: 0.45rem 0.6rem; box-shadow: 0 6px 18px rgb(0 0 0 / 0.14); min-width: 9rem; max-width: 15rem; display: grid; gap: 0.1rem; z-index: 2; }
</style>
