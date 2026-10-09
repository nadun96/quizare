<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import type { Overrides, Quiz, Topic } from '$lib/types';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import SortHeader from '$lib/ui/SortHeader.svelte';

	type Detail = Topic & { classroom_id: string; classroom_name: string; module_name: string };
	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let topic = $state<Detail | null>(null);
	let status = $state(Paged.fromUrl(urlState, 'status'));
	// A topic's quizzes, one page at a time (PL-FR-02).
	const quizzes = new Paged<Quiz>(() => '/api/teacher/topics/' + id + '/quizzes', 'quizzes', { url: urlState, sort: 'created', filters: () => ({ status }) });
	let title = $state('');
	let errors = $state<Record<string, string>>({});

	async function load() {
		topic = await api.get<Detail>('/api/teacher/topics/' + id);
		await quizzes.load();
	}
	$effect(() => {
		if (ready() && id) load();
	});
	async function create(e: SubmitEvent) {
		e.preventDefault();
		const q = await api.post<Quiz>('/api/teacher/topics/' + id + '/quizzes', { title });
		goto('/t/quizzes/' + q.id);
	}
	async function saveSettings(o: Overrides) {
		errors = {};
		try {
			await api.patch('/api/teacher/topics/' + id, { settings: o });
			await load();
		} catch (e) {
			if (e instanceof ApiError) errors = e.fields;
			throw e;
		}
	}
</script>

<div class="page-container vstack">
	{#if topic}
		<p class="small"><a href={'/t/classrooms/' + topic.classroom_id}>← {topic.classroom_name}</a> · {topic.module_name}</p>
		<h1>{topic.name}</h1>
		<div class="row">
			<ListSearch list={quizzes} placeholder="Search quizzes" label="Search quizzes" />
			<select class="select select-sm w-auto" bind:value={status} onchange={() => quizzes.refilter()} aria-label="Status"><option value="">All statuses</option><option value="draft">draft</option><option value="ready">ready</option><option value="archived">archived</option></select>
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader list={quizzes} key="title" label="Quiz" /></th><th>Questions</th><th><SortHeader list={quizzes} key="status" label="Status" /></th><th><SortHeader list={quizzes} key="created" label="Created" /></th></tr></thead>
				<tbody>
					{#each quizzes.rows as q (q.id)}
						<tr>
							<td><a href={'/t/quizzes/' + q.id}>{q.title}</a></td>
							<td>{q.question_count} ({q.total_marks} marks)</td>
							<td><span class="badge badge-soft {q.status === 'ready' ? 'ok' : ''}">{q.status}</span></td>
							<td class="small tabular">{q.created_at ? new Date(q.created_at).toLocaleDateString() : ''}</td>
						</tr>
					{:else}<tr><td colspan="4" class="muted">{quizzes.q || status ? 'No quizzes match.' : 'No quizzes yet.'}</td></tr>{/each}
				</tbody>
			</table>
		</div>
		<Pager list={quizzes} label="Quizzes" />
		<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 row" onsubmit={create}>
			<input class="input w-full" style="flex:1" bind:value={title} placeholder="New quiz title" required aria-label="Quiz title" />
			<button class="btn btn-primary">Create quiz</button>
		</form>
		<details class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
			<summary><strong>Topic settings</strong></summary>
			<div style="margin-top:1rem"><SettingsEditor level="topic" value={topic.settings} onsave={saveSettings} {errors} /></div>
		</details>
	{/if}
</div>
