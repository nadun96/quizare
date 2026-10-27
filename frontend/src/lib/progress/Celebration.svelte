<script lang="ts">
	// The one celebration (D-97): shown when a student reaches a new level or
	// earns a badge since they last looked. One short burst, then still;
	// nothing moves with reduced motion.
	import Icon from '../ui/Icon.svelte';
	import { reduced } from '../ui/motion';
	import LevelRing from './LevelRing.svelte';
	import { BADGE_ICON, levelFraction, type Badge, type Progress } from './progress.svelte';

	let { p, levelUp, badges, onclose }: { p: Progress; levelUp: boolean; badges: Badge[]; onclose: () => void } = $props();
	let dialog = $state<HTMLDialogElement>();
	let fill = $state(0);
	$effect(() => {
		dialog?.showModal();
		// The ring sweeps up to where the student is now.
		const id = requestAnimationFrame(() => (fill = levelFraction(p)));
		return () => cancelAnimationFrame(id);
	});
	const still = reduced();
	// Twelve sparks around the ring.
	const sparks = Array.from({ length: 12 }, (_, i) => i * 30);
</script>

<dialog bind:this={dialog} class="modal" onclose={onclose} aria-labelledby="celebrate-h">
	<div class="modal-box celebrate">
		<div class="stage" class:still>
			{#if !still}{#each sparks as a (a)}<span class="spark" style:--a="{a}deg"></span>{/each}{/if}
			<LevelRing fraction={still ? levelFraction(p) : fill} level={p.level} size={132} stroke={9} badge={false}>
				<span class="medal"><span class="small muted">Level</span><strong>{p.level}</strong></span>
			</LevelRing>
		</div>
		<h2 id="celebrate-h" class="m-0 mt-4 text-xl text-center">{levelUp ? `You reached level ${p.level}` : badges.length === 1 ? `New badge: ${badges[0].name}` : `${badges.length} new badges`}</h2>
		{#if badges.length}
			<ul class="earned">
				{#each badges as b (b.id)}
					<li><span class="disc"><Icon name={BADGE_ICON[b.id] ?? 'star'} size={18} /></span><span><strong>{b.name}</strong><br /><span class="small muted">{b.description}</span></span></li>
				{/each}
			</ul>
		{/if}
		<p class="m-0 mt-2 small muted text-center">{p.next_level_xp - p.xp} XP to level {p.level + 1}.</p>
		<div class="modal-action justify-center"><button class="btn btn-primary" onclick={() => dialog?.close()}>Keep going</button></div>
	</div>
</dialog>

<style>
	.celebrate { max-width: 24rem; }
	.stage { position: relative; display: grid; place-items: center; padding-top: 0.5rem; }
	.medal { display: grid; place-items: center; line-height: 1; width: 100%; height: 100%; background: color-mix(in oklab, var(--color-accent) 8%, var(--color-base-100)); }
	.medal strong { font-size: 2.75rem; color: var(--color-accent); animation: pop 600ms 250ms cubic-bezier(0.34, 1.56, 0.64, 1) both; }
	.still .medal strong { animation: none; }
	.spark { position: absolute; top: 50%; left: 50%; width: 6px; height: 6px; border-radius: 9999px; background: var(--color-accent); opacity: 0;
		animation: burst 900ms 200ms ease-out both; }
	.spark:nth-child(3n) { background: var(--color-primary); }
	.spark:nth-child(4n) { background: var(--color-success); }
	@keyframes burst {
		0% { opacity: 1; transform: rotate(var(--a)) translateY(0) scale(1); }
		100% { opacity: 0; transform: rotate(var(--a)) translateY(-96px) scale(0.4); }
	}
	@keyframes pop {
		0% { transform: scale(0.6); opacity: 0; }
		100% { transform: scale(1); opacity: 1; }
	}
	.earned { list-style: none; margin: 1rem 0 0; padding: 0; display: grid; gap: 0.6rem; }
	.earned li { display: flex; align-items: center; gap: 0.75rem; }
	.disc { display: grid; place-items: center; flex: none; width: 2.25rem; height: 2.25rem; border-radius: 9999px; background: var(--color-accent); color: var(--color-accent-content); }
</style>
