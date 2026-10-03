<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { STATE_BADGE, STATE_LABEL } from '$lib/types';

	type Attempt = { attempt_id: string; session_title: string; quiz_title: string; state: string; released: boolean; created_at: string };
	type Cls = { classroom_id: string; name: string; status: string; student_number: string | null };
	let attempts = $state<Attempt[]>([]);
	let classes = $state<Cls[]>([]);

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) return void goto(loginUrl('/my'));
		api.get<{ attempts: Attempt[] }>('/api/my/attempts').then((r) => (attempts = r.attempts));
		api.get<{ classrooms: Cls[] }>('/api/my/classrooms').then((r) => (classes = r.classrooms ?? []));
	});
	const live = (s: string) => ['waiting', 'admitted', 'in_progress', 'paused'].includes(s);
</script>

<div class="container stack">
	<div class="row"><h1 style="margin:0">My quizzes</h1><span class="spacer"></span><a class="button primary" href="/join">Join with a code</a></div>
	<div class="card">
		{#if attempts.length === 0}
			<p class="muted">No quizzes yet. Scan the QR code your teacher shows to join one.</p>
		{:else}
			<div class="table-wrap"><table>
				<thead><tr><th>Quiz</th><th>Status</th><th></th></tr></thead>
				<tbody>
					{#each attempts as a (a.attempt_id)}
						<tr>
							<td><strong>{a.session_title}</strong><br /><span class="small muted">{new Date(a.created_at).toLocaleString()}</span></td>
							<td><span class="badge {STATE_BADGE[a.state] ?? ''}">{STATE_LABEL[a.state] ?? a.state}</span></td>
							<td>
								{#if live(a.state)}<a class="button small primary" href={'/attempt/' + a.attempt_id}>Continue</a>
								{:else if a.released}<a class="button small" href={'/my/results/' + a.attempt_id}>Results</a>
								{:else}<span class="small muted">Results not released</span>{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table></div>
		{/if}
	</div>
	<h2>My classrooms</h2>
	<div class="card">
		{#each classes as c (c.classroom_id)}
			<p>{c.name} {#if c.student_number}<span class="muted small">· ID {c.student_number}</span>{/if} {#if c.status === 'pending'}<span class="badge warn">awaiting approval</span>{/if}</p>
		{:else}
			<p class="muted">You're not enrolled in any classroom yet.</p>
		{/each}
	</div>
</div>
