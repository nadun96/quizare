<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A pool's three views (D-132): its questions, reviewing them one by one,
	// and spreading where the right answers are.
	import type { Snippet } from 'svelte';
	import Icon from '../ui/Icon.svelte';
	import PoolBalance from './PoolBalance.svelte';
	import PoolReview from './PoolReview.svelte';

	let { quizId, questions, onchange }: { quizId: string; questions: Snippet; onchange?: () => void } = $props();
	let tab = $state<'questions' | 'review' | 'randomize'>('questions');
	// Each view loads afresh when opened, so a change in one shows in the others.
	let version = $state(0);
	function go(t: typeof tab) {
		tab = t;
		version++;
	}
</script>

<div class="vstack gap-3">
	<div class="tabs tabs-box w-fit" role="tablist" aria-label={tr("Pool")}>
		<button class="tab gap-1.5" role="tab" aria-selected={tab === 'questions'} class:tab-active={tab === 'questions'} onclick={() => go('questions')}><Icon name="list" size={15} />{tr("Questions")}</button>
		<button class="tab gap-1.5" role="tab" aria-selected={tab === 'review'} class:tab-active={tab === 'review'} onclick={() => go('review')}><Icon name="check-circle" size={15} />{tr("Review")}</button>
		<button class="tab gap-1.5" role="tab" aria-selected={tab === 'randomize'} class:tab-active={tab === 'randomize'} onclick={() => go('randomize')}><Icon name="shuffle" size={15} />{tr("Randomize")}</button>
	</div>
	{#key version}
		{#if tab === 'questions'}{@render questions()}
		{:else if tab === 'review'}<PoolReview {quizId} {onchange} />
		{:else}<PoolBalance {quizId} {onchange} />{/if}
	{/key}
</div>
