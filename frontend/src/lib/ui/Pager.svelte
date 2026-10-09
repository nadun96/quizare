<script lang="ts" generics="T">
	// Page controls for a server-paginated list (PL-FR-01, TS-FR-92): "26–50 of
	// 340", first, previous, next and last, and the rows per page. Works with
	// the keyboard, and the range is announced to screen readers (TS-NFR-30).
	import { PAGE_SIZES, type Paged } from '../paged.svelte';
	import IconBtn from './IconBtn.svelte';

	let { list, label = 'List' }: { list: Paged<T>; label?: string } = $props();
	const id = $props.id();
	const first = $derived(list.page <= 1);
	const last = $derived(list.page >= list.pages);
</script>

{#if list.loaded && list.total > PAGE_SIZES[0]}
	<nav class="pager" aria-label="{label} pages">
		<span class="small muted tabular" aria-live="polite">{list.range}</span>
		<span class="spacer"></span>
		<label class="small muted flex items-center gap-2" for="{id}-size">Rows per page
			<select class="select select-sm w-auto" id="{id}-size" value={list.size} onchange={(e) => list.setSize(Number(e.currentTarget.value))}>
				{#each PAGE_SIZES as n (n)}<option value={n}>{n}</option>{/each}
			</select>
		</label>
		<span class="flex items-center gap-1">
			<IconBtn icon="chevrons-left" label="First page" class="btn-sm btn-ghost" disabled={first || list.loading} onclick={() => list.goTo(1)} />
			<IconBtn icon="chevron-left" label="Previous page" class="btn-sm btn-ghost" disabled={first || list.loading} onclick={() => list.goTo(list.page - 1)} />
			<span class="small tabular px-1">Page {list.page} of {list.pages}</span>
			<IconBtn icon="chevron-right" label="Next page" class="btn-sm btn-ghost" disabled={last || list.loading} onclick={() => list.goTo(list.page + 1)} />
			<IconBtn icon="chevrons-right" label="Last page" class="btn-sm btn-ghost" disabled={last || list.loading} onclick={() => list.goTo(list.pages)} />
		</span>
	</nav>
{:else if list.loaded && list.total > 0}
	<p class="small muted tabular m-0" aria-live="polite">{list.range}</p>
{/if}

<style>
	.pager { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 1rem; }
</style>
