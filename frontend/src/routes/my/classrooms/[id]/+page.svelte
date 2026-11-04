<script lang="ts">
	// A student's one classroom (D-138): what's on now, what's due, what's
	// coming and everything they've completed, with its files.
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import MyFiles from '$lib/drive/MyFiles.svelte';
	import { requireLogin } from '$lib/guard.svelte';
	import { action, dateLine, kindOf, markLine, statusOf, withTutoring, type ClassroomWork, type WorkItem } from '$lib/my/classroomWork';
	import { TutorClient, tutoringConfig } from '$lib/tutoring/client';
	import type { CalendarEntry, MyClassroomAttendance } from '$lib/tutoring/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';

	const ready = requireLogin();
	const id = $derived(page.params.id ?? '');
	let work = $state<ClassroomWork | null>(null);
	let error = $state('');
	let view = $state<'all' | 'todo' | 'done'>('all');

	async function load() {
		error = '';
		try {
			const w = await api.get<ClassroomWork>(`/api/my/classrooms/${id}/work`);
			work = w;
			if (w.status === 'active') addTutoring(w);
		} catch (e) {
			error = e instanceof ApiError && e.status === 404 ? 'You aren’t enrolled in this classroom.' : 'This classroom didn’t load. Try again.';
		}
	}
	// Tutoring is a separate service: the page shows without it.
	async function addTutoring(w: ClassroomWork) {
		try {
			if (!(await tutoringConfig()).enabled) return;
			const client = new TutorClient();
			const now = Date.now();
			const from = new Date(now - 864e5).toISOString();
			const to = new Date(now + 60 * 864e5).toISOString();
			const [cal, att] = await Promise.all([
				client.get<{ sessions: CalendarEntry[] }>(`/api/calendar?from=${from}&to=${to}`),
				client.get<{ classrooms: MyClassroomAttendance[] }>('/api/my/attendance')
			]);
			work = withTutoring(w, cal.sessions ?? [], att.classrooms ?? [], now);
		} catch {
			/* without tutoring sessions */
		}
	}
	$effect(() => {
		if (ready() && id) load();
	});
	// A quiz that ended without them can't be done any more, so it isn't to do.
	const missed = $derived((work?.due ?? []).filter((x) => x.status === 'missed'));
	const todo = $derived((work?.due ?? []).filter((x) => x.status !== 'missed'));
	const todoCount = $derived((work?.live.length ?? 0) + todo.length);
	const showTodo = $derived(view !== 'done');
	const showDone = $derived(view !== 'todo');
</script>

{#snippet row(it: WorkItem)}
	{@const k = kindOf(it.kind)}
	{@const s = statusOf(it.status)}
	{@const act = it.href ? action(it) : ''}
	<li class="item">
		<span class="kind" title={k.label}><Icon name={k.icon} size={17} /></span>
		<div class="grow min-w-0">
			<p class="m-0 font-semibold truncate">{it.title}</p>
			<p class="m-0 small muted">{k.label}{#if dateLine(it)}{' · '}{dateLine(it)}{/if}{#if it.detail && it.status !== 'live' && it.status !== 'missed'}{' · '}{it.detail}{/if}</p>
		</div>
		<span class="badge badge-soft badge-sm whitespace-nowrap {s.tone}">{s.label}</span>
		{#if act}<a class="btn btn-sm" class:btn-primary={it.status === 'live' || it.status === 'overdue' || it.status === 'due_soon'} href={it.href}>{act}</a>{/if}
	</li>
{/snippet}

<svelte:head><title>{work?.name ?? 'Classroom'} · Classroom Quiz</title></svelte:head>

<div class="page-container vstack">
	<p class="small m-0"><a href="/my">← My quizzes</a></p>
	{#if error}
		<div class="card card-border bg-base-100"><EmptyState icon="book" title={error}><a href="/my">Back to your classrooms</a></EmptyState></div>
	{:else if !work}
		<Skeleton lines={6} />
	{:else}
		<div>
			<h1 class="m-0">{work.name}</h1>
			<p class="m-0 small muted">{#if work.teacher}Teacher: {work.teacher}{/if}{#if work.student_number}{' · '}Your student ID: <strong class="tabular">{work.student_number}</strong>{/if}</p>
			{#if work.description}<p class="m-0 mt-1">{work.description}</p>{/if}
		</div>

		{#if work.status !== 'active'}
			<div class="alert alert-soft alert-warning" role="status"><Icon name="clock" size={18} />Your teacher hasn’t approved your enrolment yet. Your work appears here once they do.</div>
		{:else}
			<div class="join self-start" role="group" aria-label="Show">
				<button class="btn btn-sm join-item" class:btn-primary={view === 'all'} aria-pressed={view === 'all'} onclick={() => (view = 'all')}>All</button>
				<button class="btn btn-sm join-item" class:btn-primary={view === 'todo'} aria-pressed={view === 'todo'} onclick={() => (view = 'todo')}>To do ({todoCount})</button>
				<button class="btn btn-sm join-item" class:btn-primary={view === 'done'} aria-pressed={view === 'done'} onclick={() => (view = 'done')}>Completed ({work.completed.length})</button>
			</div>

			{#if showTodo}
				{#if work.live.length}
					<section aria-labelledby="live-h" class="vstack gap-2">
						<h2 id="live-h" class="m-0 text-base">On now</h2>
						<ul class="list card card-border bg-base-100">{#each work.live as it (it.kind + it.id)}{@render row(it)}{/each}</ul>
					</section>
				{/if}
				<section aria-labelledby="due-h" class="vstack gap-2">
					<h2 id="due-h" class="m-0 text-base">To do</h2>
					{#if todo.length}
						<ul class="list card card-border bg-base-100">{#each todo as it (it.kind + it.id)}{@render row(it)}{/each}</ul>
					{:else}
						<p class="m-0 small muted">Nothing due. You’re up to date.</p>
					{/if}
				</section>
				{#if work.upcoming.length}
					<section aria-labelledby="up-h" class="vstack gap-2">
						<h2 id="up-h" class="m-0 text-base">Coming up</h2>
						<ul class="list card card-border bg-base-100">{#each work.upcoming as it (it.kind + it.id)}{@render row(it)}{/each}</ul>
					</section>
				{/if}
			{/if}

			{#if view !== 'todo' && missed.length}
				<section aria-labelledby="missed-h" class="vstack gap-2">
					<h2 id="missed-h" class="m-0 text-base">Missed</h2>
					<p class="m-0 small muted">Quizzes that ended without you, in the last 60 days. Ask your teacher if you were away.</p>
					<ul class="list card card-border bg-base-100">{#each missed as it (it.kind + it.id)}{@render row(it)}{/each}</ul>
				</section>
			{/if}

			{#if showDone}
				<section aria-labelledby="done-h" class="vstack gap-2">
					<h2 id="done-h" class="m-0 text-base">Completed</h2>
					{#if work.completed.length}
						<ul class="list card card-border bg-base-100">
							{#each work.completed as d (d.kind + d.id)}
								{@const k = kindOf(d.kind)}
								{@const s = statusOf(d.status)}
								<li class="item">
									<span class="kind" title={k.label}><Icon name={k.icon} size={17} /></span>
									<div class="grow min-w-0">
										<p class="m-0 font-semibold truncate">{d.title}</p>
										<p class="m-0 small muted">{k.label}{#if d.at}{' · '}{dateLine(d)}{/if}{#if (d.tries ?? 0) > 1}{' · '}best of {d.tries} tries{/if}</p>
									</div>
									{#if markLine(d)}<span class="small tabular whitespace-nowrap" class:font-semibold={d.score != null}>{markLine(d)}</span>{/if}
									{#if d.passed != null}<span class="badge badge-soft badge-sm {d.passed ? 'badge-success' : 'badge-warning'}">{d.passed ? 'Passed' : 'Not passed'}</span>{:else}<span class="badge badge-soft badge-sm whitespace-nowrap {s.tone}">{s.label}</span>{/if}
									{#if d.href}<a class="btn btn-sm btn-ghost" href={d.href}>{d.kind === 'quiz' ? 'Results' : 'View'}</a>{/if}
								</li>
							{/each}
						</ul>
					{:else}
						<p class="m-0 small muted">Nothing completed yet.</p>
					{/if}
				</section>
			{/if}

			{#if view === 'all'}<MyFiles classroomId={work.classroom_id} />{/if}
		{/if}
	{/if}
</div>

<style>
	.list { list-style: none; margin: 0; padding: 0; overflow: hidden; }
	.item { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 0.75rem; padding: 0.7rem 1rem; }
	.item + .item { border-top: 1px solid var(--color-base-200); }
	.item > .grow { flex: 1 1 12rem; }
	.kind { display: grid; place-items: center; flex: none; width: 2rem; height: 2rem; color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 10%, transparent); }
</style>
