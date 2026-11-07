<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import ToonPage from '$lib/poll/ToonPage.svelte';
	import { IDENTITY_LABEL } from '$lib/poll/meta';
	import PollSettingsForm from '$lib/poll/PollSettingsForm.svelte';
	import { DEFAULT_SETTINGS } from '$lib/poll/scoring';
	import type { Poll, PollSettings } from '$lib/poll/types';
	import type { Classroom } from '$lib/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';

	const ready = requireRole('teacher');
	let status = $state(Paged.fromUrl(urlState, 'status'));
	// Polls, newest first, one page at a time (PL-FR-02).
	const polls = new Paged<Poll>(() => '/api/teacher/polls', 'polls', { url: urlState, sort: 'created', desc: true, filters: () => ({ status }) });
	let classrooms = $state<Classroom[]>([]);
	let creating = $state(false);
	let title = $state('');
	let settings = $state<PollSettings>({ ...DEFAULT_SETTINGS });
	let classroomId = $state<string | null>(null);
	let topicId = $state<string | null>(null);
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (!ready()) return;
		polls.load();
		api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => (classrooms = r.classrooms ?? []));
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const p = await api.post<Poll>('/api/teacher/polls', { title, settings, classroom_id: classroomId, topic_id: classroomId ? topicId : null });
			goto('/t/polls/' + p.id);
		} catch (err) {
			error = err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("Could not create the poll");
		} finally {
			busy = false;
		}
	}
	const STATUS = { draft: ['Draft', ''], open: ['Open', 'ok'], closed: ['Closed', 'warn'] } as const;
</script>

<ToonPage>
<div class="page-container vstack">
	<div class="row">
		<h1 class="m-0">{tr("Polls")}</h1>
		<span class="spacer"></span>
		{#if !creating}<button class="btn btn-primary" onclick={() => (creating = true)}><Icon name="plus" size={16} />{tr("New poll")}</button>{/if}
	</div>
	<p class="muted m-0">{tr("Quick questions for any audience, anonymous or identified, with results that update live.")}</p>

	{#if creating}
		<form class="card card-border vstack bg-base-100 p-4 shadow-sm sm:p-6" onsubmit={create} in:flyIn>
			<div><label for="np-title">{tr("Title")}</label><input id="np-title" class="input w-full" bind:value={title} required maxlength="200" placeholder={tr("e.g. Lesson 4 check-in")} /></div>
			<PollSettingsForm bind:settings bind:classroomId bind:topicId {classrooms} />
			{#if error}<p class="field-error" role="alert">{error}</p>{/if}
			<div class="flex gap-2">
				<button class="btn btn-primary" disabled={busy}>{tr("Create and add questions")}</button>
				<button type="button" class="btn" onclick={() => (creating = false)}>{tr("Cancel")}</button>
			</div>
		</form>
	{/if}

	<div class="flex flex-wrap items-center gap-3">
		<ListSearch list={polls} placeholder={tr("Search title or join code")} label={tr("Search polls")} />
		<select class="select select-sm w-auto" bind:value={status} onchange={() => polls.refilter()} aria-label={tr("Status")}><option value="">{tr("All polls")}</option><option value="draft">{tr("Drafts")}</option><option value="open">{tr("Open")}</option><option value="closed">{tr("Closed")}</option></select>
		<span class="spacer"></span>
		<label class="small muted flex items-center gap-2">{tr("Sort")}
			<select class="select select-sm w-auto" value={polls.sort} onchange={(e) => polls.sortBy(e.currentTarget.value, e.currentTarget.value === 'created')}>
				<option value="created">{tr("Newest first")}</option><option value="title">{tr("Title A–Z")}</option>
			</select></label>
	</div>
	{#if !polls.loaded}
		<div class="auto-grid"><Skeleton lines={2} /><Skeleton lines={2} /></div>
	{:else if polls.total === 0 && !creating && !polls.q && !status}
		<div class="card card-border bg-base-100"><EmptyState icon="chart" title={tr("No polls yet")}>{tr("Create one to ask a quick question with a word cloud, rating, scale or any other input.")}</EmptyState></div>
	{:else}
		<div class="auto-grid">
			{#each polls.rows as p, i (p.id)}
				<a class="card card-border interactive bg-base-100 p-4 text-inherit no-underline shadow-sm sm:p-5" href={'/t/polls/' + p.id} in:flyIn={{ delay: Math.min(i, 8) * 40 }}>
					<div class="flex items-start gap-3">
						<span class="grid size-10 flex-none place-items-center rounded-xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="chart" size={20} /></span>
						<div class="min-w-0 flex-1">
							<h3 class="m-0 truncate">{p.title}</h3>
							<p class="small muted m-0 mt-1"><span class="badge badge-soft badge-sm {STATUS[p.status][1]}">{STATUS[p.status][0]}</span> · {IDENTITY_LABEL[p.identity]} · <span class="tabular">{p.participants}</span> {tr("joined")}</p>
							<p class="small muted m-0 mt-1">{tr("Code")} <strong class="tabular">{p.join_code}</strong></p>
						</div>
					</div>
				</a>
			{:else}<p class="muted">{tr("No polls match.")}</p>
			{/each}
		</div>
		<Pager list={polls} label={tr("Polls")} />
	{/if}
</div>
</ToonPage>

