<script lang="ts">
	// Star rating as a radio group: arrow keys move, Enter/Space chooses, and
	// the count is spoken ("4 of 5 stars"), so it isn't colour or shape alone.
	let { points = 5, value, disabled = false, label, onchange }: { points?: number; value?: number; disabled?: boolean; label: string; onchange: (v: number | undefined) => void } = $props();
	let hover = $state(0);
	const shown = $derived(hover || value || 0);
	const stars = $derived(Array.from({ length: points }, (_, i) => i + 1));
	let group = $state<HTMLElement>();

	function key(e: KeyboardEvent, n: number) {
		let next = n;
		if (e.key === 'ArrowRight' || e.key === 'ArrowUp') next = Math.min(points, n + 1);
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') next = Math.max(1, n - 1);
		else if (e.key === 'Home') next = 1;
		else if (e.key === 'End') next = points;
		else return;
		e.preventDefault();
		onchange(next);
		group?.querySelectorAll<HTMLElement>('[role=radio]')[next - 1]?.focus();
	}
</script>

<div class="flex flex-wrap items-center gap-3">
	<div class="stars" role="radiogroup" tabindex="-1" aria-label={label} bind:this={group} onmouseleave={() => (hover = 0)}>
		{#each stars as n (n)}
			<button
				type="button"
				role="radio"
				class="star"
				class:on={n <= shown}
				aria-checked={value === n}
				aria-label="{n} of {points} star{n === 1 ? '' : 's'}"
				tabindex={(value ?? 1) === n ? 0 : -1}
				{disabled}
				onmouseenter={() => (hover = n)}
				onclick={() => onchange(n)}
				onkeydown={(e) => key(e, n)}
			>
				<svg viewBox="0 0 24 24" width="36" height="36" aria-hidden="true"><path d="M12 2.5l2.9 6.1 6.6.8-4.9 4.6 1.3 6.6L12 17.3l-5.9 3.3 1.3-6.6-4.9-4.6 6.6-.8z" /></svg>
			</button>
		{/each}
	</div>
	<span class="small muted tabular" aria-hidden="true">{value ? `${value} / ${points}` : 'Not rated'}</span>
	{#if value && !disabled}<button type="button" class="btn btn-ghost btn-sm" onclick={() => onchange(undefined)}>Clear</button>{/if}
</div>

<style>
	.stars { display: inline-flex; gap: 0.15rem; }
	.star { padding: 0.2rem; border-radius: 0.5rem; background: none; border: 0; cursor: pointer; color: var(--color-field); transition: transform var(--motion-fast) var(--ease-out), color var(--motion-fast); }
	.star:hover:not(:disabled) { transform: scale(1.12); }
	.star path { fill: transparent; stroke: currentColor; stroke-width: 1.6; stroke-linejoin: round; }
	.star.on { color: #d18700; }
	.star.on path { fill: #f5b301; stroke: #b97700; }
	:global([data-theme='quiz-dark']) .star.on path { fill: #fbbf24; stroke: #fbbf24; }
	@media (prefers-color-scheme: dark) { :global(:root:not([data-theme='quiz'])) .star.on path { fill: #fbbf24; stroke: #fbbf24; } }
	.star:disabled { cursor: default; }
</style>
