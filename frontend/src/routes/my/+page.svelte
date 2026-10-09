<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { STATE_BADGE, STATE_ICON, STATE_LABEL } from '$lib/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';

	type Attempt = { attempt_id: string; session_title: string; quiz_title: string; state: string; released: boolean; created_at: string };
	type Cls = { classroom_id: string; name: string; status: string; student_number: string | null };
	// Results and classrooms, one page at a time (PL-FR-02 "My results").
	const attempts = new Paged<Attempt>(() => '/api/my/attempts', 'attempts', { url: urlState, sort: 'created', desc: true });
	const classes = new Paged<Cls>(() => '/api/my/classrooms', 'classrooms', { url: urlState, prefix: 'cls', sort: 'name' });
	// Running attempts are the newest, so the first page shows them; keep them while paging back.
	let liveNow = $state<Attempt[]>([]);

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) return void goto(loginUrl('/my'));
		attempts.load();
		classes.load();
	});
	const live = (s: string) => ['waiting', 'admitted', 'in_progress', 'paused'].includes(s);
	$effect(() => {
		if (attempts.loaded && attempts.page === 1 && !attempts.q) liveNow = attempts.rows.filter((a) => live(a.state));
	});
</script>

<div class="page-container vstack">
	<div class="row"><h1 style="margin:0">My quizzes</h1><span class="spacer"></span><a class="btn btn-primary" href="/join">Join with a code</a></div>
	{#each liveNow as a (a.attempt_id)}
		<a class="card card-border interactive live-card p-4 text-inherit no-underline shadow-sm sm:p-5" href={'/attempt/' + a.attempt_id} in:flyIn>
			<div class="flex items-center gap-3">
				<span class="grid size-11 flex-none place-items-center rounded-full bg-primary text-primary-content" aria-hidden="true"><Icon name="play" size={20} /></span>
				<div class="min-w-0 flex-1">
					<p class="small m-0 font-semibold text-primary">Live now · {STATE_LABEL[a.state]}</p>
					<p class="m-0 truncate text-lg font-semibold">{a.session_title}</p>
				</div>
				<span class="btn btn-primary">Continue<Icon name="arrow-right" size={16} /></span>
			</div>
		</a>
	{/each}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		{#if !attempts.loaded}
			<Skeleton card={false} lines={3} />
		{:else if attempts.total === 0 && !attempts.q}
			<EmptyState icon="qr" title="No quizzes yet">Scan the QR code your teacher shows, or <a href="/join">enter the join code</a>.</EmptyState>
		{:else}
			<div class="mb-3"><ListSearch list={attempts} placeholder="Search quizzes" label="Search my quizzes" /></div>
			<div class="table-wrap"><table class="table">
				<thead><tr><th>Quiz</th><th>Status</th><th></th></tr></thead>
				<tbody>
					{#each attempts.rows as a (a.attempt_id)}
						<tr>
							<td><strong>{a.session_title}</strong><br /><span class="small muted">{new Date(a.created_at).toLocaleString()}</span></td>
							<td><span class="badge badge-soft {STATE_BADGE[a.state] ?? ''}"><span aria-hidden="true">{STATE_ICON[a.state] ?? ''}</span> {STATE_LABEL[a.state] ?? a.state}</span></td>
							<td>
								{#if live(a.state)}<a class="btn btn-sm btn-primary" href={'/attempt/' + a.attempt_id}>Continue</a>
								{:else if a.released}<a class="btn btn-sm" href={'/my/results/' + a.attempt_id}>Results</a>
								{:else}<span class="small muted">Results not released</span>{/if}
							</td>
						</tr>
					{:else}<tr><td colspan="3" class="muted">No quizzes match.</td></tr>
					{/each}
				</tbody>
			</table></div>
			<div class="mt-3"><Pager list={attempts} label="My quizzes" /></div>
		{/if}
	</div>
	<h2>My classrooms</h2>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		{#each classes.rows as c (c.classroom_id)}
			<p>{c.name} {#if c.student_number}<span class="muted small">· ID {c.student_number}</span>{/if} {#if c.status === 'pending'}<span class="badge badge-soft badge-warning">awaiting approval</span>{/if}</p>
		{:else}
			<p class="muted">{classes.loaded ? "You're not enrolled in any classroom yet." : 'Loading…'}</p>
		{/each}
		<Pager list={classes} label="My classrooms" />
	</div>
</div>
