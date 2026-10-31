<script lang="ts">
	// The one celebration (D-97): shown when a student reaches a new level or
	// earns a badge since they last looked. One short burst, then still;
	// nothing moves with reduced motion.
	import Icon from '../ui/Icon.svelte';
	import { gsap } from 'gsap';
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
	// One GSAP timeline (D-108): the sparks burst out, the level number pops,
	// then each new badge slides in.
	let stage = $state<HTMLElement>();
	let box = $state<HTMLElement>();
	$effect(() => {
		if (still || !stage || !box) return;
		const tl = gsap.timeline({ delay: 0.2 });
		tl.fromTo(stage.querySelectorAll('.spark'), { y: 0, scale: 1, autoAlpha: 1 }, { y: -96, scale: 0.4, autoAlpha: 0, duration: 0.9, ease: 'power2.out', stagger: 0.015 }, 0)
			.from(stage.querySelector('.medal strong'), { scale: 0.6, autoAlpha: 0, duration: 0.6, ease: 'back.out(2.2)' }, 0.05)
			.from(box.querySelectorAll('.earned li'), { x: -16, autoAlpha: 0, duration: 0.4, ease: 'power3.out', stagger: 0.08 }, 0.35);
		return () => tl.kill();
	});
	// Twelve sparks around the ring.
	const sparks = Array.from({ length: 12 }, (_, i) => i * 30);
</script>

<dialog bind:this={dialog} class="modal" onclose={onclose} aria-labelledby="celebrate-h">
	<div class="modal-box celebrate" bind:this={box}>
		<div class="stage" class:still bind:this={stage}>
			{#if !still}{#each sparks as a (a)}<span class="spark-arm" style:transform="rotate({a}deg)"><span class="spark"></span></span>{/each}{/if}
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
	.medal strong { font-size: 2.75rem; color: var(--color-accent); }
	.spark-arm { position: absolute; top: 50%; left: 50%; width: 0; height: 0; }
	.spark { position: absolute; left: -3px; top: -3px; width: 6px; height: 6px; background: var(--color-accent); opacity: 0; }
	.spark-arm:nth-child(3n) .spark { background: var(--color-primary); }
	.spark-arm:nth-child(4n) .spark { background: var(--color-success); }
	.earned { list-style: none; margin: 1rem 0 0; padding: 0; display: grid; gap: 0.6rem; }
	.earned li { display: flex; align-items: center; gap: 0.75rem; }
	.disc { display: grid; place-items: center; flex: none; width: 2.25rem; height: 2.25rem; border-radius: 9999px; background: var(--color-accent); color: var(--color-accent-content); }
</style>
