<script lang="ts">
	// A module's topics, one page at a time (PL-FR-02), in the teacher's
	// order, with add, rename and delete.
	import { api, ApiError } from './api';
	import { Paged } from './paged.svelte';
	import type { Topic } from './types';
	import { confirmDialog, promptDialog } from './ui/dialog.svelte';
	import Pager from './ui/Pager.svelte';
	import { toast } from './ui/toast.svelte';

	let { moduleId }: { moduleId: string } = $props();
	const list = new Paged<Topic>(() => '/api/teacher/modules/' + moduleId + '/topics', 'topics', { sort: 'position' });
	let name = $state('');
	$effect(() => {
		if (moduleId) list.load();
	});

	async function add(e: SubmitEvent) {
		e.preventDefault();
		await api.post('/api/teacher/modules/' + moduleId + '/topics', { name });
		name = '';
		await list.load();
		list.goTo(list.pages); // the new topic is last
	}
	async function rename(t: Topic) {
		const v = await promptDialog({ title: 'Rename topic', label: 'Name', value: t.name, maxlength: 200 });
		if (v && v !== t.name) {
			await api.patch('/api/teacher/topics/' + t.id, { name: v });
			list.load();
		}
	}
	async function remove(t: Topic) {
		if (!(await confirmDialog({ title: 'Delete this topic?', body: 'Everything inside it is deleted too.', confirm: 'Delete', danger: true }))) return;
		try {
			await api.del('/api/teacher/topics/' + t.id);
			list.load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Delete failed', 'error');
		}
	}
</script>

{#each list.rows as t (t.id)}
	<div class="row">
		<a href={'/t/topics/' + t.id + '?name=' + encodeURIComponent(t.name)}>{t.name}</a>
		<span class="spacer"></span>
		<button class="btn btn-sm" onclick={() => rename(t)}>Rename</button>
		<button class="btn btn-sm btn-error btn-outline" onclick={() => remove(t)}>Delete</button>
	</div>
{/each}
<Pager {list} label="Topics" />
<form class="row" onsubmit={add}>
	<input class="input w-full" style="flex:1" placeholder="New topic" bind:value={name} required aria-label="New topic name" />
	<button class="btn btn-sm">Add topic</button>
</form>
