<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { IDENTITY_LABEL } from '$lib/poll/meta';
	import PollSettingsForm from '$lib/poll/PollSettingsForm.svelte';
	import type { Poll, PollSettings } from '$lib/poll/types';
	import type { Classroom } from '$lib/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';

	const ready = requireRole('teacher');
	let polls = $state<Poll[] | null>(null);
	let classrooms = $state<Classroom[]>([]);
	let creating = $state(false);
	let title = $state('');
	let settings = $state<PollSettings>({ identity: 'anonymous', audience: 'anyone', pacing: 'self', show_results: 'after_answer', allow_edit: true });
	let classroomId = $state<string | null>(null);
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (!ready()) return;
		api.get<{ polls: Poll[] }>('/api/teacher/polls').then((r) => (polls = r.polls));
		api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms').then((r) => (classrooms = r.classrooms ?? []));
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const p = await api.post<Poll>('/api/teacher/polls', { title, settings, classroom_id: classroomId });
			goto('/t/polls/' + p.id);
		} catch (err) {
			error = err instanceof ApiError ? Object.values(err.fields)[0] ?? err.message : 'Could not create the poll';
		} finally {
			busy = false;
		}
	}
	const STATUS = { draft: ['Draft', ''], open: ['Open', 'ok'], closed: ['Closed', 'warn'] } as const;
</script>

<div class="page-container vstack">
	<div class="row">
		<h1 class="m-0">Polls</h1>
		<span class="spacer"></span>
		{#if !creating}<button class="btn btn-primary" onclick={() => (creating = true)}><Icon name="plus" size={16} />New poll</button>{/if}
	</div>
	<p class="muted m-0">Quick questions for any audience, anonymous or identified, with results that update live.</p>

	{#if creating}
		<form class="card card-border vstack bg-base-100 p-4 shadow-sm sm:p-6" onsubmit={create} in:flyIn>
			<div><label for="np-title">Title</label><input id="np-title" class="input w-full" bind:value={title} required maxlength="200" placeholder="e.g. Lesson 4 check-in" /></div>
			<PollSettingsForm bind:settings bind:classroomId {classrooms} />
			{#if error}<p class="field-error" role="alert">{error}</p>{/if}
			<div class="flex gap-2">
				<button class="btn btn-primary" disabled={busy}>Create and add questions</button>
				<button type="button" class="btn" onclick={() => (creating = false)}>Cancel</button>
			</div>
		</form>
	{/if}

	{#if polls === null}
		<div class="auto-grid"><Skeleton lines={2} /><Skeleton lines={2} /></div>
	{:else if polls.length === 0 && !creating}
		<div class="card card-border bg-base-100"><EmptyState icon="chart" title="No polls yet">Create one to ask a quick question with a word cloud, rating, scale or any other input.</EmptyState></div>
	{:else}
		<div class="auto-grid">
			{#each polls as p, i (p.id)}
				<a class="card card-border interactive bg-base-100 p-4 text-inherit no-underline shadow-sm sm:p-5" href={'/t/polls/' + p.id} in:flyIn={{ delay: Math.min(i, 8) * 40 }}>
					<div class="flex items-start gap-3">
						<span class="grid size-10 flex-none place-items-center rounded-xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="chart" size={20} /></span>
						<div class="min-w-0 flex-1">
							<h3 class="m-0 truncate">{p.title}</h3>
							<p class="small muted m-0 mt-1"><span class="badge badge-soft badge-sm {STATUS[p.status][1]}">{STATUS[p.status][0]}</span> · {IDENTITY_LABEL[p.identity]} · <span class="tabular">{p.participants}</span> joined</p>
							<p class="small muted m-0 mt-1">Code <strong class="tabular">{p.join_code}</strong></p>
						</div>
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>
