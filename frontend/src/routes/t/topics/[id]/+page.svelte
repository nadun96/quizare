<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import type { Overrides, Quiz, Topic } from '$lib/types';

	type Detail = Topic & { classroom_id: string; classroom_name: string; module_name: string };
	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let topic = $state<Detail | null>(null);
	let quizzes = $state<Quiz[]>([]);
	let title = $state('');
	let errors = $state<Record<string, string>>({});

	async function load() {
		topic = await api.get<Detail>('/api/teacher/topics/' + id);
		quizzes = (await api.get<{ quizzes: Quiz[] }>('/api/teacher/topics/' + id + '/quizzes')).quizzes ?? [];
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

<div class="container stack">
	{#if topic}
		<p class="small"><a href={'/t/classrooms/' + topic.classroom_id}>← {topic.classroom_name}</a> · {topic.module_name}</p>
		<h1>{topic.name}</h1>
		<div class="card table-wrap">
			<table>
				<thead><tr><th>Quiz</th><th>Questions</th><th>Status</th></tr></thead>
				<tbody>
					{#each quizzes as q (q.id)}
						<tr>
							<td><a href={'/t/quizzes/' + q.id}>{q.title}</a></td>
							<td>{q.question_count} ({q.total_marks} marks)</td>
							<td><span class="badge {q.status === 'ready' ? 'ok' : ''}">{q.status}</span></td>
						</tr>
					{:else}<tr><td colspan="3" class="muted">No quizzes yet.</td></tr>{/each}
				</tbody>
			</table>
		</div>
		<form class="card row" onsubmit={create}>
			<input style="flex:1" bind:value={title} placeholder="New quiz title" required aria-label="Quiz title" />
			<button class="primary">Create quiz</button>
		</form>
		<details class="card">
			<summary><strong>Topic settings</strong></summary>
			<div style="margin-top:1rem"><SettingsEditor level="topic" value={topic.settings} onsave={saveSettings} {errors} /></div>
		</details>
	{/if}
</div>
