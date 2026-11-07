<script lang="ts" generics="T">
	import { tr } from '$lib/i18n/i18n.svelte';
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

<div class="flex flex-wrap items-end gap-2" role="search" aria-label={tr("Filter and sort this report")}>
	<label class="vstack gap-1 small">{tr("Search")}<input class="input input-sm w-44" type="search" bind:value={view.q} {placeholder} /></label>
	{#if show.length}
		<label class="vstack gap-1 small">{tr("Show")}
			<select class="select select-sm w-auto max-w-64" bind:value={view.show}>
				<option value="">{tr("Everyone")}</option>
				{#each show as o (o.v)}<option value={o.v}>{tr(o.label)}</option>{/each}
			</select>
		</label>
	{/if}
	{#if groups.length}
		<label class="vstack gap-1 small">{tr("Group")}
			<select class="select select-sm w-auto max-w-48" bind:value={view.group}>
				<option value="">{tr("All groups")}</option>
				{#each groups as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
			</select>
		</label>
	{/if}
	<label class="vstack gap-1 small">{tr("Sort by")}
		<select class="select select-sm w-auto max-w-56" bind:value={view.sort}>
			{#each cols as c (c.key)}<option value={c.key}>{tr(c.label)}</option>{/each}
		</select>
	</label>
	<button type="button" class="btn btn-sm" onclick={() => (view.dir = view.dir === 'asc' ? 'desc' : 'asc')} aria-label={view.dir === 'asc' ? tr("Ascending; sort descending instead") : tr("Descending; sort ascending instead")}>
		<Icon name={view.dir === 'asc' ? 'arrow-up' : 'arrow-down'} size={14} />{view.dir === 'asc' ? tr("Ascending") : tr("Descending")}
	</button>
	{#if !pristine}<button type="button" class="btn btn-ghost btn-sm" onclick={() => (view = newView(first))}>{tr("Clear")}</button>{/if}
	<span class="small muted pb-1 ms-auto" role="status">{shown === total ? `${total} ${noun}` : tr("Showing {0} of {1} {2}", [shown, total, noun])}</span>
</div>
