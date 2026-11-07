<script lang="ts" generics="T">
	// Search, filter and sort for a classroom report (D-154).
	import Icon from '../ui/Icon.svelte';
	import { isFiltered, newView, type ShowOption, type ViewCol, type ViewState } from './reportView';

	let { view = $bindable(), cols, show = [], groups = [], shown, total, noun, placeholder = 'Name or ID' }: {
		view: ViewState;
		cols: ViewCol<T>[];
		show?: ShowOption[];
		groups?: { id: string; name: string }[];
		shown: number;
		total: number;
		noun: string;
		placeholder?: string;
	} = $props();
	const first = $derived(cols[0]?.key ?? 'student');
	const pristine = $derived(!isFiltered(view) && view.sort === first && view.dir === 'asc');
</script>

<div class="flex flex-wrap items-end gap-2" role="search" aria-label="Filter and sort this report">
	<label class="vstack gap-1 small">Search<input class="input input-sm w-44" type="search" bind:value={view.q} {placeholder} /></label>
	{#if show.length}
		<label class="vstack gap-1 small">Show
			<select class="select select-sm w-auto max-w-64" bind:value={view.show}>
				<option value="">Everyone</option>
				{#each show as o (o.v)}<option value={o.v}>{o.label}</option>{/each}
			</select>
		</label>
	{/if}
	{#if groups.length}
		<label class="vstack gap-1 small">Group
			<select class="select select-sm w-auto max-w-48" bind:value={view.group}>
				<option value="">All groups</option>
				{#each groups as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
			</select>
		</label>
	{/if}
	<label class="vstack gap-1 small">Sort by
		<select class="select select-sm w-auto max-w-56" bind:value={view.sort}>
			{#each cols as c (c.key)}<option value={c.key}>{c.label}</option>{/each}
		</select>
	</label>
	<button type="button" class="btn btn-sm" onclick={() => (view.dir = view.dir === 'asc' ? 'desc' : 'asc')} aria-label={view.dir === 'asc' ? 'Ascending; sort descending instead' : 'Descending; sort ascending instead'}>
		<Icon name={view.dir === 'asc' ? 'arrow-up' : 'arrow-down'} size={14} />{view.dir === 'asc' ? 'Ascending' : 'Descending'}
	</button>
	{#if !pristine}<button type="button" class="btn btn-ghost btn-sm" onclick={() => (view = newView(first))}>Clear</button>{/if}
	<span class="small muted pb-1 ms-auto" role="status">{shown === total ? `${total} ${noun}` : `Showing ${shown} of ${total} ${noun}`}</span>
</div>
