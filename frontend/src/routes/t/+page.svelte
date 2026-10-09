<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import type { Classroom } from '$lib/types';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';

	const ready = requireRole('teacher');
	let showArchived = $state(Paged.fromUrl(urlState, 'archived') === '1');
	// One page of classrooms at a time, newest first (PL-FR-01).
	const list = new Paged<Classroom>(() => '/api/teacher/classrooms', 'classrooms', {
		url: urlState, sort: 'created', desc: true, filters: () => ({ archived: showArchived ? '1' : undefined })
	});
	let name = $state('');
	let error = $state('');

	$effect(() => {
		if (ready()) list.load();
	});
	function toggleArchived() {
		showArchived = !showArchived;
		list.refilter();
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			await api.post('/api/teacher/classrooms', { name });
			toast(`Classroom “${name}” created`);
			name = '';
			list.load();
		} catch (err) {
			error = err instanceof ApiError ? (err.fields.name ?? err.message) : 'Could not create';
		}
	}
</script>

<div class="page-container vstack">
	<h1>Classrooms</h1>
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 row" onsubmit={create}>
		<div style="flex:1;min-width:14rem"><label for="n">New classroom</label><input class="input w-full" id="n" bind:value={name} placeholder="e.g. Grade 10 Science" required /></div>
		<button class="btn btn-primary" style="align-self:flex-end">Create</button>
		{#if error}<p class="field-error" style="width:100%">{error}</p>{/if}
	</form>
	<div class="flex flex-wrap items-center gap-3">
		<ListSearch {list} placeholder="Search classrooms" label="Search classrooms" />
		<label class="row small" style="font-weight:400"><input class="checkbox" type="checkbox" checked={showArchived} onchange={toggleArchived} /> Show archived</label>
		<span class="spacer"></span>
		<label class="small muted flex items-center gap-2">Sort
			<select class="select select-sm w-auto" value={list.sort} onchange={(e) => list.sortBy(e.currentTarget.value, e.currentTarget.value === 'created')}>
				<option value="created">Newest first</option><option value="name">Name A–Z</option>
			</select></label>
	</div>
	{#if !list.loaded}
		<div class="auto-grid"><Skeleton lines={1} /><Skeleton lines={1} /><Skeleton lines={1} /></div>
	{:else}
		<div class="auto-grid">
			{#each list.rows as c, i (c.id)}
				<a class="card card-border interactive bg-base-100 p-4 text-inherit no-underline shadow-sm sm:p-5" href={'/t/classrooms/' + c.id} in:flyIn={{ delay: Math.min(i, 8) * 40 }}>
					<div class="flex items-start gap-3">
						<span class="grid size-10 flex-none place-items-center rounded-xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="book" size={20} /></span>
						<div class="min-w-0 flex-1">
							<h3 class="m-0 truncate">{c.name}</h3>
							<p class="small muted m-0 mt-1">Join code <strong class="tabular">{c.join_code}</strong> {#if c.archived}<span class="badge badge-soft badge-sm">archived</span>{/if}</p>
						</div>
						<span class="muted" aria-hidden="true"><Icon name="arrow-right" size={18} /></span>
					</div>
				</a>
			{:else}
				<div class="card card-border col-span-full bg-base-100">
					{#if list.q}<EmptyState icon="search" title="No classrooms match “{list.q}”">Try another name.</EmptyState>
					{:else}<EmptyState icon="book" title="No classrooms yet">Create one above, then add modules, topics and quizzes to it.</EmptyState>{/if}
				</div>
			{/each}
		</div>
		<Pager {list} label="Classrooms" />
	{/if}
</div>
