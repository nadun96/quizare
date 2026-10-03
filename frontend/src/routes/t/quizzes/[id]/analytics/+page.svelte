<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import AnalyticsView from '$lib/AnalyticsView.svelte';
	import { requireRole } from '$lib/guard.svelte';

	const ready = requireRole('teacher');
	let stats = $state<any>(null);
	let linkMsg = $state('');
	$effect(() => {
		if (ready()) api.get('/api/teacher/quizzes/' + page.params.id + '/analytics').then((s) => (stats = s));
	});
	async function shareLink() {
		const l = await api.post<{ token: string }>('/api/teacher/share-links', { scope: 'quiz', target_id: page.params.id, views: ['question_pct', 'pass_rate'] });
		linkMsg = location.origin + '/r/' + l.token;
	}
</script>

<div class="container stack" style="max-width:1200px">
	<p class="small"><a href={'/t/quizzes/' + page.params.id}>← Quiz</a></p>
	{#if stats}
		<div class="row"><h1 style="margin:0">{stats.quiz_title}: all sessions</h1><span class="spacer"></span><button onclick={shareLink}>Share summary</button></div>
		{#if linkMsg}<p class="alert ok small">Public link (shown once): <a href={linkMsg} target="_blank" style="word-break:break-all">{linkMsg}</a></p>{/if}
		<h2>Session comparison</h2>
		<div class="card table-wrap">
			<table><thead><tr><th>Session</th><th>Date</th><th>Finished</th><th>Mean</th><th>Median</th><th>Pass rate</th></tr></thead><tbody>
				{#each stats.comparison as s (s.session_id)}
					<tr><td><a href={'/t/sessions/' + s.session_id + '/results'}>{s.title}</a></td><td class="small">{new Date(s.created_at).toLocaleDateString()}</td>
						<td>{s.class.finished}</td><td>{s.class.mean_pct}%</td><td>{s.class.median_pct}%</td><td>{s.class.pass_rate}%</td></tr>
				{/each}
			</tbody></table>
		</div>
		<AnalyticsView cls={stats.class} questions={stats.questions} students={stats.students} />
	{:else}<p>Loading…</p>{/if}
</div>
