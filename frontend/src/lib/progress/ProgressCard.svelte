<script lang="ts">
	// The student's progress on their My page (D-97): their level ring, XP
	// to the next level, the weekly streak, badges and what earned XP lately.
	// Theirs alone: no ranking against classmates.
	import Icon from '../ui/Icon.svelte';
	import LevelRing from './LevelRing.svelte';
	import { BADGE_ICON, levelFraction, weeks, type Progress } from './progress.svelte';

	let { p }: { p: Progress } = $props();
	const toNext = $derived(p.next_level_xp - p.xp);
	const earned = $derived(p.badges.filter((b) => b.earned).length);
	const when = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
</script>

<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6" aria-labelledby="progress-h">
	<h2 id="progress-h" class="sr-only">Your progress</h2>
	<div class="top">
		<LevelRing fraction={levelFraction(p)} level={p.level} size={96} stroke={7} badge={false}>
			<span class="medal"><span class="small muted">Level</span><strong>{p.level}</strong></span>
		</LevelRing>
		<div class="vstack gap-1 min-w-0">
			<p class="m-0 text-2xl font-bold tabular-nums">{p.xp} XP</p>
			<p class="m-0 small muted">{toNext} XP to level {p.level + 1}</p>
			<div class="flex flex-wrap gap-2 pt-1">
				<span class="chip" class:on={p.streak_weeks > 0} title="Weeks in a row with a quiz or tutorial">
					<Icon name="sparkles" size={14} />{p.streak_weeks ? weeks(p.streak_weeks) + ' running' : 'No streak yet'}
				</span>
				{#if p.streak_weeks && !p.active_this_week}<span class="chip warn"><Icon name="clock" size={14} />Do one this week to keep it</span>{/if}
				<span class="chip"><Icon name="trophy" size={14} />{earned} of {p.badges.length} badges</span>
			</div>
		</div>
	</div>

	<ul class="badges" aria-label="Badges">
		{#each p.badges as b (b.id)}
			<li class="badge-item" class:earned={b.earned} title={b.description}>
				<span class="disc"><Icon name={BADGE_ICON[b.id] ?? 'star'} size={20} /></span>
				<span class="name">{b.name}</span>
				<span class="note">{b.earned && b.earned_at ? when(b.earned_at) : b.need > 1 ? `${b.have} of ${b.need}` : b.description}</span>
			</li>
		{/each}
	</ul>

	{#if p.recent.length}
		<details class="recent">
			<summary class="small">What earned XP lately</summary>
			<ul class="m-0 mt-2 p-0 list-none vstack gap-1">
				{#each p.recent as a (a.kind + a.at)}
					<li class="flex items-center gap-2 small"><Icon name={a.kind === 'quiz' ? 'check-circle' : 'book'} size={14} /><span class="truncate flex-1">{a.title}</span><span class="muted">{when(a.at)}</span><strong class="xp">+{a.xp}</strong></li>
				{/each}
			</ul>
		</details>
	{:else}
		<p class="m-0 mt-3 small muted">Finish a quiz or a tutorial to start earning XP. Results count once your teacher releases them.</p>
	{/if}
</section>

<style>
	.top { display: flex; align-items: center; gap: 1.25rem; }
	.medal { display: grid; place-items: center; line-height: 1; background: color-mix(in oklab, var(--color-accent) 8%, var(--color-base-100)); width: 100%; height: 100%; }
	.medal strong { font-size: 2rem; color: var(--color-accent); }
	.chip { display: inline-flex; align-items: center; gap: 0.3rem; padding: 0.15rem 0.6rem; border-radius: 9999px; font-size: 0.8rem; background: var(--color-base-200); }
	.chip.on { background: color-mix(in oklab, var(--color-accent) 14%, var(--color-base-100)); color: var(--color-base-content); }
	.chip.warn { background: color-mix(in oklab, var(--color-warning) 14%, var(--color-base-100)); }
	.badges { list-style: none; margin: 1.25rem 0 0; padding: 0; display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(4.75rem, 1fr)); }
	.badge-item { display: grid; justify-items: center; text-align: center; gap: 0.2rem; }
	.disc { display: grid; place-items: center; width: 2.75rem; height: 2.75rem; border-radius: 9999px; border: 2px dashed var(--color-base-300); color: color-mix(in oklab, var(--color-base-content) 45%, transparent); }
	.earned .disc { border: none; background: var(--color-accent); color: var(--color-accent-content); }
	.name { font-size: 0.8rem; font-weight: 600; }
	.note { font-size: 0.72rem; color: color-mix(in oklab, var(--color-base-content) 65%, transparent); }
	.recent { margin-top: 1rem; }
	.recent summary { cursor: pointer; }
	.xp { color: var(--color-accent); font-variant-numeric: tabular-nums; }
	@media (max-width: 420px) { .top { gap: 0.9rem; } }
</style>
