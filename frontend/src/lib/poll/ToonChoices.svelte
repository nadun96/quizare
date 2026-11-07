<script lang="ts">
	// Live results of a choice question, cartoon style (D-150): columns that jump
	// as answers arrive, over a tile for each option with its share. The tiles say
	// everything the columns do, so screen readers read those.
	import Icon from '../ui/Icon.svelte';
	import { markFor, tileColumns } from './toon';
	import ToonMark from './ToonMark.svelte';

	let { options, responses, big = false, correct = null }: { options: { id: string; label: string; n: number }[]; responses: number; big?: boolean; correct?: string[] | null } = $props();
	const top = $derived(Math.max(1, ...options.map((o) => o.n)));
	const pct = (n: number) => (responses ? Math.round((n / responses) * 100) : 0);
	// More options, more and smaller tiles, so they fit the presenter's screen (D-153).
	const cols = $derived(tileColumns(options.length));
	const dense = $derived(options.length > 4);
	const tiny = $derived(options.length > 9);
</script>

<div class="toon-choices" class:big class:dense class:tiny>
	<div class="chart" aria-hidden="true" style:--n={options.length}>
		{#each options as o, i (o.id)}
			<div class="col">
				<span class="count tabular">{o.n}</span>
				<!-- A few percent always shows, so an empty column still reads as a column. -->
				<span class="column" style:height="{4 + (o.n / top) * 96}%" style:background={markFor(i).color}></span>
				<span class="foot"><ToonMark index={i} size={tiny ? 15 : big ? 26 : 20} /></span>
			</div>
		{/each}
	</div>
	<ul class="tiles" class:judged={!!correct} style:--cols={cols}>
		{#each options as o, i (o.id)}
			{@const right = !!correct?.includes(o.id)}
			<li class="tile" class:right style:--tile={markFor(i).color}>
				<span class="mark"><ToonMark index={i} size={big && !dense ? 30 : 22} /></span>
				<span class="opt-label" title={o.label}>{o.label}</span>
				{#if right}<span class="tick"><Icon name="check" size={big ? 20 : 15} /><span class="sr-only">Correct</span></span>{/if}
				<span class="share tabular">{pct(o.n)}%<span class="sr-only">, {o.n} {o.n === 1 ? 'person' : 'people'}</span></span>
			</li>
		{/each}
	</ul>
</div>

<style>
	.toon-choices { display: grid; gap: 1rem; }
	.chart { display: grid; grid-template-columns: repeat(var(--n), minmax(0, 1fr)); gap: clamp(0.4rem, 2vw, 1.5rem); height: 11rem; padding: 0 0.5rem; }
	.big .chart { height: clamp(11rem, 32vh, 22rem); padding: 0 clamp(0.5rem, 4vw, 4rem); }
	.col { display: grid; grid-template-rows: auto minmax(0, 1fr) auto; min-width: 0; height: 100%; justify-items: center; }
	.count { font-weight: 650; font-size: 1rem; align-self: end; }
	.big .count { font-size: 1.5rem; }
	/* The track is the row; the column grows up from its foot. */
	.column { align-self: end; width: 100%; max-width: 8rem; border: 3px solid var(--toon-ink, #1b2148); border-bottom: 0; border-radius: 0.7rem 0.7rem 0 0; transition: height 650ms cubic-bezier(0.3, 1.6, 0.5, 1); }
	.foot { display: grid; place-items: center; width: 100%; max-width: 8rem; padding: 0.3rem 0; border: 3px solid var(--toon-ink, #1b2148); border-radius: 0 0 0.7rem 0.7rem; background: #fff; }
	/* Positioned so that hidden words stay inside when the tiles scroll. */
	.tiles { position: relative; list-style: none; margin: 0; padding: 0; display: grid; gap: 0.7rem; grid-template-columns: minmax(0, 1fr); }
	@media (min-width: 700px) { .big .tiles { grid-template-columns: repeat(var(--cols), minmax(0, 1fr)); } }
	.tile { position: relative; display: flex; align-items: center; gap: 0.6rem; min-width: 0; padding: 0.55rem 0.85rem; border: 3px solid var(--toon-ink, #1b2148); border-radius: 0.9rem; background: var(--tile); box-shadow: 3px 3px 0 var(--toon-ink, #1b2148); font-weight: 550; }
	.big .tile { padding: 0.75rem 1.1rem; font-size: 1.3rem; }
	.big.dense .tiles { gap: 0.45rem; }
	.big.dense .tile { padding: 0.25rem 0.6rem; gap: 0.4rem; font-size: clamp(0.95rem, 0.55rem + 0.7vw, 1.35rem); line-height: 1.2; box-shadow: 2px 2px 0 var(--toon-ink, #1b2148); }
	/* A long option gets two lines here; its tooltip and the table have it whole. */
	.big.dense .opt-label { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; line-clamp: 2; overflow: hidden; }
	.big.tiny .tile { font-size: clamp(0.85rem, 0.25rem + 1.4vh, 1.35rem); padding-block: 0.15rem; }
	.big.tiny .foot { padding: 0.1rem 0; }
	.big.tiny .count { font-size: 0.95rem; }
	.big.dense .tick { width: 1.4em; height: 1.4em; }
	/* With the answer shown, the others lose their colour; the figures stay. */
	.judged .tile:not(.right) { background: #fff; box-shadow: none; }
	.tile.right { outline: 3px solid var(--toon-ink, #1b2148); outline-offset: 2px; }
	.mark { display: grid; flex: none; }
	.opt-label { flex: 1; min-width: 0; overflow-wrap: anywhere; }
	.tick { display: grid; place-items: center; flex: none; width: 1.7em; height: 1.7em; border-radius: 999px; background: var(--toon-ink, #1b2148); color: #fff; }
	.share { flex: none; font-weight: 700; }
	/* A presenter's screen: everything in view. The columns give way first, then the tiles scroll. */
	@media (min-width: 1180px) and (min-height: 620px) {
		.big { flex: 1; min-height: 0; grid-template-rows: minmax(6rem, 1fr) minmax(0, auto); gap: 0.6rem; }
		.big .chart { height: auto; min-height: 0; }
		.big:not(.tiny) .count { font-size: 1.15rem; }
		.big .tiles { overflow-y: auto; padding: 0 5px 5px 0; align-content: start; }
	}
	@media (prefers-reduced-motion: reduce) { .column { transition: none; } }
</style>
