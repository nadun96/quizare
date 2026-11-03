<script lang="ts">
	// A list's filters by details (D-128): term (classrooms), department,
	// year, semester, course code and tags, from the values in use. Only
	// filters with values to choose from show.
	import Icon from '../ui/Icon.svelte';
	import { activeFilters, emptyFilter, type Facets, type PropFilter } from './props';

	let { value = $bindable(), facets, when = false, onchange }: { value: PropFilter; facets: Facets; when?: boolean; onchange: () => void } = $props();
	function set<K extends keyof PropFilter>(k: K, v: PropFilter[K]) {
		value = { ...value, [k]: v };
		onchange();
	}
	function toggleTag(t: string) {
		set('tags', value.tags.some((x) => x.toLowerCase() === t.toLowerCase()) ? value.tags.filter((x) => x.toLowerCase() !== t.toLowerCase()) : [...value.tags, t]);
	}
	const selects = $derived(
		(
			[
				['department', 'Department', facets.departments],
				['year', 'Year', facets.years],
				['semester', 'Semester', facets.semesters],
				['course_code', 'Course', facets.course_codes]
			] as const
		).filter(([k, , list]) => list.length > 0 || value[k])
	);
	const WHEN = [['', 'All'], ['current', 'Running now'], ['upcoming', 'Upcoming'], ['past', 'Ended']] as const;
	const n = $derived(activeFilters(value));
</script>

<div class="vstack gap-2">
	<div class="flex flex-wrap items-center gap-2">
		{#if when}
			<div class="join" role="group" aria-label="By term">
				{#each WHEN as [k, l] (k)}
					<button type="button" class="btn btn-sm join-item" class:btn-primary={value.when === k} aria-pressed={value.when === k} onclick={() => set('when', k)}>{l}</button>
				{/each}
			</div>
		{/if}
		{#each selects as [k, label, list] (k)}
			<select class="select select-sm w-auto" class:select-primary={value[k]} aria-label={label} value={value[k]} onchange={(e) => set(k, e.currentTarget.value)}>
				<option value="">{label}: any</option>
				{#each list as c (c.value)}<option value={c.value}>{c.value} ({c.count})</option>{/each}
				{#if value[k] && !list.some((c) => c.value === value[k])}<option value={value[k]}>{value[k]}</option>{/if}
			</select>
		{/each}
		{#if n}<button type="button" class="btn btn-sm btn-ghost" onclick={() => ((value = emptyFilter()), onchange())}><Icon name="x" size={14} />Clear {n} {n === 1 ? 'filter' : 'filters'}</button>{/if}
	</div>
	{#if facets.tags.length}
		<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label="By tag">
			<Icon name="flag" size={14} />
			{#each facets.tags.slice(0, 16) as t (t.value)}
				{@const on = value.tags.some((x) => x.toLowerCase() === t.value.toLowerCase())}
				<button type="button" class="badge badge-sm cursor-pointer" class:badge-primary={on} class:badge-ghost={!on} aria-pressed={on} onclick={() => toggleTag(t.value)}>{t.value} <span class="opacity-60">{t.count}</span></button>
			{/each}
		</div>
	{/if}
</div>
