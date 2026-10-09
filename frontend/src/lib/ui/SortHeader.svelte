<script lang="ts" generics="T">
	// A sortable column heading for a server-paginated list (TS-FR-93): sorts
	// every page on the server; pressing it again flips the direction.
	import type { Paged } from '../paged.svelte';
	import Icon from './Icon.svelte';

	let { list, key, label, desc = false }: { list: Paged<T>; key: string; label: string; desc?: boolean } = $props();
	const active = $derived(list.sort === key);
</script>

<button type="button" class="sort" class:active onclick={() => list.sortBy(key, desc)} aria-label="Sort by {label}{active ? (list.desc ? ', descending' : ', ascending') : ''}">
	{label}
	<span aria-hidden="true" class="ind">{#if active}<Icon name={list.desc ? 'arrow-down' : 'arrow-up'} size={12} />{:else}<Icon name="arrow-up-down" size={12} />{/if}</span>
</button>

<style>
	.sort { all: unset; cursor: pointer; display: inline-flex; align-items: center; gap: 0.25rem; font: inherit; color: inherit; }
	.sort:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; border-radius: 4px; }
	.ind { opacity: 0.45; }
	.active .ind { opacity: 1; }
</style>
