<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import type { Classroom } from '$lib/types';

	const ready = requireRole('teacher');
	let list = $state<Classroom[]>([]);
	let showArchived = $state(false);
	let name = $state('');
	let error = $state('');

	async function load() {
		list = (await api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms' + (showArchived ? '?archived=1' : ''))).classrooms ?? [];
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
			name = '';
			load();
		} catch (err) {
			error = err instanceof ApiError ? (err.fields.name ?? err.message) : 'Could not create';
		}
	}
</script>

<div class="container stack">
	<h1>Classrooms</h1>
	<form class="card row" onsubmit={create}>
		<div style="flex:1;min-width:14rem"><label for="n">New classroom</label><input id="n" bind:value={name} placeholder="e.g. Grade 10 Science" required /></div>
		<button class="primary" style="align-self:flex-end">Create</button>
		{#if error}<p class="field-error" style="width:100%">{error}</p>{/if}
	</form>
	<label class="row small" style="font-weight:400"><input type="checkbox" bind:checked={showArchived} /> Show archived</label>
	<div class="grid">
		{#each list as c (c.id)}
			<a class="card" href={'/t/classrooms/' + c.id} style="text-decoration:none;color:inherit">
				<h3 style="margin-top:0">{c.name}</h3>
				<p class="small muted">Join code <strong>{c.join_code}</strong> {#if c.archived}<span class="badge">archived</span>{/if}</p>
			</a>
		{:else}
			<p class="muted">No classrooms yet. Create one to start adding modules, topics and quizzes.</p>
		{/each}
	</div>
</div>
