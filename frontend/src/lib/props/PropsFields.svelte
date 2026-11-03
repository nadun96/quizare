<script lang="ts">
	// The details of a classroom, meeting or tutoring session (D-128), all
	// optional. Values already in use are offered as suggestions; tags are
	// typed and added with Enter or a comma.
	import Icon from '../ui/Icon.svelte';
	import { noFacets, parseTags, type Facets, type Props } from './props';

	let { value = $bindable(), facets = noFacets(), dates = false, errors = {}, id = 'props' }: { value: Props; facets?: Facets; dates?: boolean; errors?: Record<string, string>; id?: string } = $props();
	let tagText = $state('');
	const err = (k: string) => errors['props.' + k];

	function addTags() {
		value.tags = parseTags(tagText, value.tags);
		tagText = '';
	}
	function onTagKey(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			e.preventDefault();
			addTags();
		} else if (e.key === 'Backspace' && !tagText && value.tags.length) value.tags = value.tags.slice(0, -1);
	}
	const suggestedTags = $derived(facets.tags.map((t) => t.value).filter((t) => !value.tags.some((x) => x.toLowerCase() === t.toLowerCase())).slice(0, 8));
</script>

<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
	<label class="flex flex-col gap-1">
		<span class="small">Course code</span>
		<input class="input w-full uppercase placeholder:normal-case" class:input-error={err('course_code')} list="{id}-codes" maxlength="40" placeholder="e.g. IS2201" bind:value={value.course_code} />
		<datalist id="{id}-codes">{#each facets.course_codes as c (c.value)}<option value={c.value}></option>{/each}</datalist>
	</label>
	<label class="flex flex-col gap-1">
		<span class="small">Department</span>
		<input class="input w-full" list="{id}-depts" maxlength="100" placeholder="e.g. Information Systems" bind:value={value.department} />
		<datalist id="{id}-depts">{#each facets.departments as c (c.value)}<option value={c.value}></option>{/each}</datalist>
	</label>
	<label class="flex flex-col gap-1">
		<span class="small">Year</span>
		<input class="input w-full" class:input-error={err('year')} type="number" min="1990" max="2100" placeholder="e.g. 2025" value={value.year ?? ''} oninput={(e) => (value.year = e.currentTarget.value ? Number(e.currentTarget.value) : null)} />
		{#if err('year')}<span class="field-error">{err('year')}</span>{/if}
	</label>
	<label class="flex flex-col gap-1">
		<span class="small">Semester</span>
		<input class="input w-full" list="{id}-sems" maxlength="40" placeholder="e.g. Semester 1" bind:value={value.semester} />
		<datalist id="{id}-sems">{#each facets.semesters.length ? facets.semesters : ['Semester 1', 'Semester 2'].map((value) => ({ value, count: 0 })) as c (c.value)}<option value={c.value}></option>{/each}</datalist>
	</label>
	{#if dates}
		<label class="flex flex-col gap-1">
			<span class="small">Starts</span>
			<input class="input w-full" class:input-error={err('starts_on')} type="date" value={value.starts_on ?? ''} oninput={(e) => (value.starts_on = e.currentTarget.value || null)} />
			{#if err('starts_on')}<span class="field-error">{err('starts_on')}</span>{/if}
		</label>
		<label class="flex flex-col gap-1">
			<span class="small">Ends</span>
			<input class="input w-full" class:input-error={err('ends_on')} type="date" min={value.starts_on ?? undefined} value={value.ends_on ?? ''} oninput={(e) => (value.ends_on = e.currentTarget.value || null)} />
			{#if err('ends_on')}<span class="field-error">{err('ends_on')}</span>{/if}
		</label>
	{/if}
	<div class="flex flex-col gap-1" class:sm:col-span-2={dates} class:lg:col-span-4={!dates} class:lg:col-span-2={dates}>
		<label class="small" for="{id}-tags">Tags</label>
		<div class="tags input w-full" class:input-error={err('tags')}>
			{#each value.tags as t (t)}
				<span class="badge badge-soft badge-primary gap-1">{t}<button type="button" class="opacity-70 hover:opacity-100" aria-label="Remove the tag {t}" onclick={() => (value.tags = value.tags.filter((x) => x !== t))}><Icon name="x" size={12} /></button></span>
			{/each}
			<input id="{id}-tags" class="grow min-w-24" placeholder={value.tags.length ? '' : 'Type a tag, then Enter'} bind:value={tagText} onkeydown={onTagKey} onblur={addTags} />
		</div>
		{#if err('tags')}<span class="field-error">{err('tags')}</span>{/if}
		{#if suggestedTags.length}
			<div class="flex flex-wrap items-center gap-1">
				<span class="small muted">Used before:</span>
				{#each suggestedTags as t (t)}<button type="button" class="badge badge-ghost badge-sm" onclick={() => (value.tags = parseTags(t, value.tags))}>+ {t}</button>{/each}
			</div>
		{/if}
	</div>
</div>

<style>
	.tags { display: flex; flex-wrap: wrap; align-items: center; gap: 0.3rem; height: auto; min-height: 2.5rem; padding-block: 0.3rem; }
	.tags input { outline: none; background: transparent; border: 0; }
</style>
