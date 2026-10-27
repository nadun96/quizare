<script lang="ts">
	// The teacher's grid of what students share backstage (TS-FR-35): pages
	// of 25, any tile enlarged. Only the visible page and the enlarged tile
	// are received, small or large as their tiles are.
	import type { SessionMedia, Tile } from './media.svelte';
	import TrackView from './TrackView.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { ordered, pageOf, pages, wanted, PAGE_SIZE } from './stage';

	let { media }: { media: SessionMedia } = $props();
	let page = $state(0);
	let enlarged = $state<string | null>(null);

	const list = $derived(ordered(media.stage));
	const count = $derived(pages(list.length));
	const shown = $derived(pageOf(list, page));
	const big = $derived(list.find((e) => e.key === enlarged) ?? null);

	$effect(() => {
		if (page > count - 1) page = count - 1;
	});
	$effect(() => {
		media.showStage(wanted(list, page, enlarged));
	});

	const tile = (e: (typeof list)[number]): Tile | null =>
		e.track ? { key: e.key, participant: e.participant, name: e.name, label: e.label, source: e.source, track: e.track, local: false, backstage: true, group: false } : null;
</script>

{#if list.length}
	<section class="vstack" aria-label="Shared with the teachers">
		<div class="flex flex-wrap items-center gap-2">
			<h2 class="m-0 text-sm grow">Shared with the teachers only · {list.length}</h2>
			{#if count > 1}
				<IconBtn icon="chevron-left" label="Previous page" class="btn-xs" disabled={page === 0} onclick={() => page--} />
				<span class="small" aria-live="polite">Page {page + 1} of {count}</span>
				<IconBtn icon="chevron-right" label="Next page" class="btn-xs" disabled={page >= count - 1} onclick={() => page++} />
			{/if}
		</div>
		{#if big}
			{@const t = tile(big)}
			<div class="vstack">
				{#if t}<TrackView tile={t} big />{:else}<div class="placeholder big">Loading {big.name}’s {big.label.toLowerCase()}…</div>{/if}
				<div><button class="btn btn-xs" onclick={() => (enlarged = null)}>Back to the grid</button></div>
			</div>
		{/if}
		<div class="stage-grid" style:--per-page={PAGE_SIZE}>
			{#each shown as e (e.key)}
				{@const t = tile(e)}
				<div class="cell" class:active={e.key === enlarged}>
					{#if t}<TrackView tile={t} />{:else}<div class="placeholder">{e.name}</div>{/if}
					<button class="btn btn-xs enlarge" onclick={() => (enlarged = enlarged === e.key ? null : e.key)} aria-pressed={e.key === enlarged} aria-label="Enlarge {e.name}’s {e.label.toLowerCase()}">{e.key === enlarged ? 'Shown above' : 'Enlarge'}</button>
				</div>
			{/each}
		</div>
	</section>
{/if}

<style>
	.stage-grid { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(10rem, 1fr)); }
	.cell { position: relative; }
	.cell.active { outline: 2px solid var(--color-primary); border-radius: 0.8rem; }
	.enlarge { position: absolute; top: 0.4rem; right: 0.4rem; }
	.placeholder { display: grid; place-items: center; aspect-ratio: 16 / 9; border-radius: 0.75rem; background: #111; color: #ccc; font-size: 0.85rem; padding: 0.5rem; text-align: center; }
	.placeholder.big { aspect-ratio: 16 / 9; }
</style>
