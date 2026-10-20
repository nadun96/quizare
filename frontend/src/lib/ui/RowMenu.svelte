<script lang="ts" module>
	import type { IconName } from './Icon.svelte';
	export type RowAction = { label: string; icon?: IconName; danger?: boolean; disabled?: boolean; onclick: () => void };
</script>

<script lang="ts">
	// A row's secondary actions (rename, duplicate, delete…) behind one "More"
	// button, so a list shows its content first rather than a column of
	// buttons, and destructive actions aren't one stray tap away. A native
	// <details>: works with the keyboard and screen readers; closes on a pick,
	// Esc, or a click elsewhere, and opens upwards near the bottom of the screen.
	import Icon from './Icon.svelte';
	import { prefs } from './prefs.svelte';

	let { actions, label = 'More actions', hint }: { actions: RowAction[]; label?: string; hint?: string } = $props();
	let el = $state<HTMLDetailsElement>();
	let up = $state(false);
	const name = $derived(hint ?? label);

	function toggled() {
		if (!el?.open) return;
		const r = el.getBoundingClientRect();
		up = window.innerHeight - r.bottom < 40 * actions.length + 24;
		el.querySelector<HTMLButtonElement>('ul button:not([disabled])')?.focus();
	}
	function pick(a: RowAction) {
		if (el) el.open = false;
		a.onclick();
	}
	function outside(e: MouseEvent) {
		if (el?.open && !el.contains(e.target as Node)) el.open = false;
	}
	function key(e: KeyboardEvent) {
		if (e.key === 'Escape' && el?.open) {
			el.open = false;
			el.querySelector('summary')?.focus();
		}
	}
</script>

<svelte:document onclick={outside} onkeydown={key} />

<details class="dropdown dropdown-end" class:dropdown-top={up} bind:this={el} ontoggle={toggled}>
	<summary class="btn btn-sm btn-ghost gap-1" class:btn-square={prefs.labels !== 'always'} aria-label={name} title={name}>
		<Icon name="more" size={18} />{#if prefs.labels === 'always'}<span>More</span>{/if}
	</summary>
	<ul class="menu dropdown-content z-30 w-48 rounded-box border border-base-300 bg-base-100 p-1 shadow-md">
		{#each actions as a (a.label)}
			<li>
				<button type="button" class:text-error={a.danger} disabled={a.disabled} onclick={() => pick(a)}>
					{#if a.icon}<Icon name={a.icon} size={15} />{/if}{a.label}
				</button>
			</li>
		{/each}
	</ul>
</details>

<style>
	summary { list-style: none; }
	summary::-webkit-details-marker { display: none; }
</style>
