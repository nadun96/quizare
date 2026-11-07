<script lang="ts" generics="T">
	// A report's column heading that sorts by that column (D-154).
	import type { Snippet } from 'svelte';
	import Icon from '../ui/Icon.svelte';
	import { nextSort, type ViewCol, type ViewState } from './reportView';

	let { view = $bindable(), cols, col, label, class: cls = '', title, children }: {
		view: ViewState;
		cols: ViewCol<T>[];
		col: string;
		label: string;
		class?: string;
		title?: string;
		children?: Snippet;
	} = $props();
	const active = $derived(view.sort === col);
</script>

<th class={cls} {title} aria-sort={active ? (view.dir === 'asc' ? 'ascending' : 'descending') : 'none'}>
	<button type="button" class="sort" class:active onclick={() => (view = nextSort(view, cols, col))}>
		{label}<span class="ind" aria-hidden="true"><Icon name={active ? (view.dir === 'asc' ? 'arrow-up' : 'arrow-down') : 'arrow-up-down'} size={12} /></span>
	</button>
	{#if children}<br />{@render children()}{/if}
</th>

<style>
	.sort { all: unset; cursor: pointer; display: inline-flex; align-items: center; gap: 0.25rem; font: inherit; color: inherit; text-transform: inherit; }
	.sort:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
	.ind { opacity: 0.45; }
	.active .ind { opacity: 1; }
</style>
