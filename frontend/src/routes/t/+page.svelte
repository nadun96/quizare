<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import type { Classroom } from '$lib/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';

	const ready = requireRole('teacher');
	let list = $state<Classroom[]>([]);
	let loaded = $state(false);
	let showArchived = $state(false);
	let name = $state('');
	let error = $state('');

	async function load() {
		list = (await api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms' + (showArchived ? '?archived=1' : ''))).classrooms ?? [];
		loaded = true;
	}
	$effect(() => {
		void showArchived;
		if (ready()) load();
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			await api.post('/api/teacher/classrooms', { name });
			toast(`Classroom “${name}” created`);
			name = '';
			load();
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
	<label class="row small" style="font-weight:400"><input class="checkbox" type="checkbox" bind:checked={showArchived} /> Show archived</label>
	{#if !loaded}
		<div class="auto-grid"><Skeleton lines={1} /><Skeleton lines={1} /><Skeleton lines={1} /></div>
	{:else}
		<div class="auto-grid">
			{#each list as c, i (c.id)}
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
					<EmptyState icon="book" title="No classrooms yet">Create one above, then add modules, topics and quizzes to it.</EmptyState>
				</div>
			{/each}
		</div>
	{/if}
</div>
