<script lang="ts">
	// Placeholder while a page loads, instead of the word "Loading…".
	let { lines = 3, card = true }: { lines?: number; card?: boolean } = $props();
</script>

<div class="vstack {card ? 'card card-border bg-base-100 p-4 sm:p-6' : ''}" aria-busy="true">
	<span class="sr-only" role="status">Loading…</span>
	<div class="skeleton skeleton-line h-5 w-2/5"></div>
	{#each Array(lines) as _, i (i)}<div class="skeleton skeleton-line" style:width="{90 - i * 12}%"></div>{/each}
</div>
