<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import MyFiles from '$lib/drive/MyFiles.svelte';
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
	// "16 Oct, 3:55 PM": short enough to sit on one line on a phone.
	const when = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
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
			<!-- A list, not a table: three columns don't fit a phone, where students use this page. -->
			<ul class="attempts" aria-label="My quizzes">
				{#each attempts.rows as a (a.attempt_id)}
					<li>
						<div class="min-w-0">
							<p class="m-0 truncate font-semibold">{a.session_title}</p>
							<p class="small muted m-0"><time datetime={a.created_at}>{when(a.created_at)}</time></p>
						</div>
						<div class="end">
							<span class="badge badge-soft whitespace-nowrap {STATE_BADGE[a.state] ?? ''}"><span aria-hidden="true">{STATE_ICON[a.state] ?? ''}</span> {STATE_LABEL[a.state] ?? a.state}</span>
							{#if live(a.state)}<a class="btn btn-sm btn-primary" href={'/attempt/' + a.attempt_id}>Continue</a>
							{:else if a.released}<a class="btn btn-sm" href={'/my/results/' + a.attempt_id}>Results</a>
							{:else}<span class="small muted whitespace-nowrap">Results not out yet</span>{/if}
						</div>
					</li>
				{:else}<li class="muted">No quizzes match.</li>
				{/each}
			</ul>
			<div class="mt-3"><Pager list={attempts} label="My quizzes" /></div>
		{/if}
	</div>
	<h2>My classrooms</h2>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		{#if classes.rows.length}
			<ul class="attempts" aria-label="My classrooms">
				{#each classes.rows as c (c.classroom_id)}
					<li>
						<div class="min-w-0">
							<p class="m-0 truncate font-semibold">{c.name}</p>
							{#if c.student_number}<p class="small muted m-0">Your student ID: {c.student_number}</p>{/if}
							{#if c.status === 'active'}<MyFiles classroomId={c.classroom_id} />{/if}
						</div>
						{#if c.status === 'pending'}<div class="end"><span class="badge badge-soft badge-warning whitespace-nowrap">Waiting for approval</span></div>{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="muted m-0">{classes.loaded ? 'You haven’t joined a classroom yet. Your teacher can give you a classroom code.' : 'Loading…'}</p>
		{/if}
		<Pager list={classes} label="My classrooms" />
	</div>
</div>

<style>
	.attempts { list-style: none; margin: 0; padding: 0; }
	.attempts li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 0.5rem 1rem; padding: 0.75rem 0; border-bottom: 1px solid var(--color-base-300); }
	.attempts li:last-child { border-bottom: 0; }
	.attempts li > .min-w-0 { flex: 1 1 12rem; }
	.end { display: flex; align-items: center; gap: 0.5rem; margin-left: auto; }
</style>
