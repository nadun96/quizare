<script lang="ts">
	// Live ranking (V2-02, D-42). Rows slide to their new place as scores
	// change; the viewer's own row is highlighted and, when they are outside
	// the top 10, pinned underneath.
	import { flip } from 'svelte/animate';
	import Icon from '../ui/Icon.svelte';
	import { flipMs, flyIn } from '../ui/motion';
	import { fmtPoints, ordinal } from './scoring';
	import type { Rank } from './types';

	let { ranks = [], me = null, meKey = '', teacher = false, big = false, limit = 10, onrename }: {
		ranks?: Rank[];
		me?: Rank | null;
		meKey?: string;
		teacher?: boolean;
		big?: boolean;
		limit?: number;
		onrename?: (r: Rank) => void;
	} = $props();

	const shown = $derived(ranks.slice(0, limit));
	const meOutside = $derived(!!me && !shown.some((r) => r.key === me.key));
	const top = $derived(Math.max(1, ...shown.map((r) => r.score)));
	const medal = (n: number) => (n <= 3 ? `m${n}` : '');
</script>

<div class="lb" class:big>
	{#if !shown.length}
		<p class="muted small m-0">Nobody has joined yet.</p>
	{:else}
		<ol class="rows" aria-label="Leaderboard">
			{#each shown as r (r.key)}
				<li class="row" class:me={r.key === meKey} animate:flip={{ duration: flipMs() }} in:flyIn>
					<span class="pos tabular {medal(r.rank)}" aria-label={ordinal(r.rank)}>{r.rank}</span>
					<span class="who min-w-0">
						<span class="name truncate">{r.name}{#if r.key === meKey}<span class="badge badge-primary badge-xs ml-2">you</span>{/if}</span>
						{#if teacher && r.real_name && r.real_name !== r.name}<span class="small muted truncate block">{r.real_name}</span>{/if}
						<span class="track" aria-hidden="true"><span class="fill" style:width="{(r.score / top) * 100}%"></span></span>
					</span>
					<span class="pts tabular"><strong>{fmtPoints(r.score)}</strong><span class="small muted block">{r.correct} right</span></span>
					{#if teacher && onrename}
						<button type="button" class="btn btn-ghost btn-xs btn-square" aria-label="Rename {r.name}" title="Rename" onclick={() => onrename(r)}><Icon name="pencil" size={14} /></button>
					{/if}
				</li>
			{/each}
		</ol>
		{#if meOutside && me}
			<p class="you-row small m-0"><Icon name="trophy" size={14} />You're <strong>{ordinal(me.rank)}</strong> with <strong class="tabular">{fmtPoints(me.score)}</strong> points</p>
		{/if}
		{#if ranks.length > limit}<p class="small muted m-0">and {ranks.length - limit} more</p>{/if}
	{/if}
</div>

<style>
	.lb { display: grid; gap: 0.5rem; }
	.rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
	.row { display: grid; grid-template-columns: 2rem minmax(0, 1fr) auto auto; gap: 0.75rem; align-items: center; padding: 0.5rem 0.75rem; border-radius: var(--radius-box); background: var(--color-base-100); border: 1px solid var(--color-base-300); }
	.row.me { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 8%, var(--color-base-100)); }
	.pos { width: 2rem; height: 2rem; border-radius: 999px; display: grid; place-items: center; font-weight: 750; background: var(--color-base-200); }
	/* Medals carry the rank number too, so colour is never the only cue. */
	.pos.m1 { background: #f5c542; color: #3d2f00; }
	.pos.m2 { background: #c9d1db; color: #1f2833; }
	.pos.m3 { background: #d99a6c; color: #331a07; }
	.name { display: block; font-weight: 600; }
	.track { display: block; height: 0.3rem; margin-top: 0.3rem; border-radius: 999px; background: var(--color-base-200); overflow: hidden; }
	.fill { display: block; height: 100%; border-radius: inherit; background: var(--color-primary); transition: width 600ms var(--ease-out); }
	.pts { text-align: right; line-height: 1.15; }
	.you-row { display: flex; gap: 0.4rem; align-items: center; padding: 0.5rem 0.75rem; border-radius: var(--radius-box); border: 1px dashed var(--color-primary); }
	.big .row { padding: 0.75rem 1.25rem; font-size: 1.35rem; grid-template-columns: 3rem minmax(0, 1fr) auto auto; }
	.big .pos { width: 2.75rem; height: 2.75rem; font-size: 1.25rem; }
	.big .track { height: 0.5rem; }
	@media (prefers-reduced-motion: reduce) { .fill { transition: none; } }
</style>
