<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A presentation on the poll's screen (D-165): everyone follows its
	// slides; its presenter turns them; the audience may rate it.
	import { ApiError } from '$lib/api';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { presClient, presenterText, step, type Stage } from './present';

	let { code, stage, meKey = '', own = false }: { code: string; stage: Stage; meKey?: string; own?: boolean } = $props();
	const presenting = $derived(!!stage.presenter_key && stage.presenter_key === meKey);
	let stars = $state(0);
	let ratedFor = $state('');
	let busy = $state(false);
	$effect(() => {
		if (ratedFor !== stage.submission_id) stars = 0;
	});

	async function go(by: number) {
		const n = step(stage, by);
		if (n == null || busy) return;
		busy = true;
		try {
			await presClient.turn(code, n);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work. Try again."), 'error');
		} finally {
			busy = false;
		}
	}
	async function rate(n: number) {
		try {
			await presClient.rate(code, stage.submission_id, n);
			stars = n;
			ratedFor = stage.submission_id;
			toast(tr("Thanks for rating it"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work. Try again."), 'error');
		}
	}
	function key(e: KeyboardEvent) {
		if (!presenting || (e.target as HTMLElement)?.closest('input, textarea, select')) return;
		if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') go(1);
		if (e.key === 'ArrowLeft' || e.key === 'PageUp') go(-1);
	}
</script>

<svelte:window onkeydown={key} />

<section class="card card-border bg-base-100 p-3 shadow-sm sm:p-4 vstack gap-2" aria-label={tr("Presentation")}>
	<div class="flex flex-wrap items-center gap-2">
		<Icon name="monitor" size={16} />
		<strong class="grow min-w-0 truncate">{stage.title}</strong>
		<span class="small muted">{stage.owner}</span>
		<span class="badge badge-soft tabular">{stage.slide} / {stage.slides}</span>
	</div>
	<img class="slide" src={presClient.slideUrl(code, stage.submission_id, stage.slide)} alt={tr("Slide {0} of {1}", [stage.slide, stage.slides])} />
	<p class="m-0 small muted" class:text-primary={presenting} class:font-semibold={presenting}>{tr(presenterText(stage, meKey))}</p>
	{#if presenting}
		<div class="flex items-center gap-2">
			<button class="btn grow" disabled={busy || step(stage, -1) == null} onclick={() => go(-1)}><Icon name="chevron-left" size={18} />{tr("Back")}</button>
			<button class="btn btn-primary grow" disabled={busy || step(stage, 1) == null} onclick={() => go(1)}>{tr("Next")}<Icon name="chevron-right" size={18} /></button>
		</div>
	{:else if stage.rating && !own}
		<div class="flex flex-wrap items-center gap-1 small" role="group" aria-label={tr("Rate this presentation")}>
			<span class="muted mr-1">{tr("Your rating")}</span>
			{#each [1, 2, 3, 4, 5] as n (n)}
				<button class="star" class:on={n <= stars} aria-label={tr("{0} of 5", [n])} aria-pressed={n <= stars} onclick={() => rate(n)}><Icon name="star" size={22} /></button>
			{/each}
		</div>
	{/if}
</section>

<style>
	.slide { width: 100%; max-height: 70vh; object-fit: contain; background: #fff; border-radius: 0.5rem; border: 1px solid var(--color-base-300); }
	.star { color: var(--color-base-300); background: none; border: 0; padding: 0.15rem; cursor: pointer; }
	.star.on { color: #f59e0b; }
</style>
