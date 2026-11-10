<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A group presentation on the classroom's screen (D-166), for the teacher
	// and for students: the slide, who's presenting, the controls for whoever
	// turns the slides, and stars for classmates. It can fill the screen.
	import type { Snippet } from 'svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import { presenterText, step, type ClassStage } from './classpres';

	let {
		stage,
		src,
		canTurn = false,
		busy = false,
		onturn,
		onrate,
		actions
	}: {
		stage: ClassStage;
		src: string;
		canTurn?: boolean;
		busy?: boolean;
		onturn?: (n: number) => void;
		onrate?: (stars: number) => void;
		actions?: Snippet;
	} = $props();
	let box = $state<HTMLElement | null>(null);
	let full = $state(false);

	function go(by: number) {
		const n = step(stage, by);
		if (n != null && !busy) onturn?.(n);
	}
	function key(e: KeyboardEvent) {
		if (!canTurn || (e.target as HTMLElement)?.closest('input, textarea, select, button')) return;
		if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') {
			e.preventDefault();
			go(1);
		}
		if (e.key === 'ArrowLeft' || e.key === 'PageUp') {
			e.preventDefault();
			go(-1);
		}
	}
	async function toggleFull() {
		try {
			if (document.fullscreenElement) await document.exitFullscreen();
			else await box?.requestFullscreen();
		} catch {
			/* not allowed here */
		}
	}
</script>

<svelte:window onkeydown={key} />
<svelte:document onfullscreenchange={() => (full = !!document.fullscreenElement)} />

<section bind:this={box} class="stage card card-border border-primary bg-base-100 p-3 sm:p-4 vstack gap-2" class:full aria-label={tr("On the classroom’s screen")}>
	<div class="flex flex-wrap items-center gap-2">
		<span class="badge badge-error badge-soft"><span class="dot" aria-hidden="true"></span>{tr("Live")}</span>
		<strong class="grow min-w-0 truncate">{stage.title} · {stage.group}</strong>
		<span class="small muted truncate">{stage.task}</span>
		<span class="badge badge-soft tabular">{stage.slide} / {stage.slides}</span>
		<button class="btn btn-ghost btn-xs" onclick={toggleFull} aria-label={full ? tr("Leave full screen") : tr("Full screen")} title={full ? tr("Leave full screen") : tr("Full screen")}><Icon name={full ? 'minimize' : 'maximize'} size={15} /></button>
	</div>
	<img class="slide" {src} alt={tr("Slide {0} of {1}", [stage.slide, stage.slides])} />
	<p class="m-0 small" class:text-primary={stage.presenting} class:font-semibold={stage.presenting} class:muted={!stage.presenting}>{tr(presenterText(stage))}</p>
	{#if canTurn}
		<div class="flex items-center gap-2">
			<button class="btn grow" disabled={busy || step(stage, -1) == null} onclick={() => go(-1)}><Icon name="chevron-left" size={18} />{tr("Back")}</button>
			<button class="btn btn-primary grow" disabled={busy || step(stage, 1) == null} onclick={() => go(1)}>{tr("Next")}<Icon name="chevron-right" size={18} /></button>
		</div>
	{/if}
	{#if onrate && stage.rating && !stage.mine}
		<div class="flex flex-wrap items-center gap-1 small" role="group" aria-label={tr("Rate this presentation")}>
			<span class="muted mr-1">{tr("Your rating")}</span>
			{#each [1, 2, 3, 4, 5] as n (n)}
				<button class="star" class:on={n <= stage.my_stars} aria-label={tr("{0} of 5", [n])} aria-pressed={n <= stage.my_stars} onclick={() => onrate?.(n)}><Icon name="star" size={22} /></button>
			{/each}
		</div>
	{/if}
	{#if actions}<div class="flex flex-wrap items-center gap-2">{@render actions()}</div>{/if}
</section>

<style>
	.slide { width: 100%; max-height: 65vh; object-fit: contain; background: #fff; border-radius: 0.5rem; border: 1px solid var(--color-base-300); }
	.stage.full { justify-content: center; border-radius: 0; }
	.stage.full .slide { max-height: calc(100vh - 9rem); flex: 1 1 auto; }
	.star { color: var(--color-base-300); background: none; border: 0; padding: 0.15rem; cursor: pointer; }
	.star.on { color: #f59e0b; }
	.dot { display: inline-block; width: 0.45rem; height: 0.45rem; border-radius: 50%; background: currentColor; margin-right: 0.3rem; animation: pulse 1.6s ease-in-out infinite; }
	@keyframes pulse { 50% { opacity: 0.3; } }
	@media (prefers-reduced-motion: reduce) { .dot { animation: none; } }
</style>
