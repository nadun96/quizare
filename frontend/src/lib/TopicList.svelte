<script lang="ts">
	import RowMenu from '$lib/ui/RowMenu.svelte';
	import Icon from '$lib/ui/Icon.svelte';
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

{#if list.rows.length}
	<ul class="topics">
		{#each list.rows as t (t.id)}
			<li>
				<a class="topic" href={'/t/topics/' + t.id + '?name=' + encodeURIComponent(t.name)}><Icon name="book" size={16} /><span class="truncate">{t.name}</span></a>
				<RowMenu hint="More actions for {t.name}" actions={[
					{ label: 'Rename', icon: 'pencil', onclick: () => rename(t) },
					{ label: 'Delete topic', icon: 'trash', danger: true, onclick: () => remove(t) }
				]} />
			</li>
		{/each}
	</ul>
{:else if list.loaded}
	<p class="small muted m-0">No topics yet. Topics hold the quizzes, for example “Plants” or “Week 3”.</p>
{/if}
<Pager {list} label="Topics" />
<form class="row" onsubmit={add}>
	<input class="input input-sm w-full" style="flex:1" placeholder="Add a topic" bind:value={name} required aria-label="New topic name" />
	<button class="btn btn-sm"><Icon name="plus" size={14} />Add topic</button>
</form>

<style>
	.topics { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
	.topics li { display: flex; align-items: center; gap: 0.5rem; border-radius: var(--radius-field); }
	.topics li:hover { background: var(--color-base-200); }
	.topic { flex: 1; min-width: 0; display: flex; align-items: center; gap: 0.5rem; padding: 0.55rem 0.5rem; color: var(--color-base-content); text-decoration: none; font-weight: 600; }
	.topic :global(svg) { color: var(--color-primary); flex: none; }
	.topic:hover span { text-decoration: underline; }
</style>
