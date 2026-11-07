<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// The teacher's classrooms (D-128): cards with each classroom's course
	// code, department, year, semester, term and tags, filtered by them and
	// grouped by one of them; the address keeps the view.
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import type { Classroom } from '$lib/types';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';
	import PropsFields from '$lib/props/PropsFields.svelte';
	import PropsFilters from '$lib/props/PropsFilters.svelte';
	import { activeFilters, emptyProps, filterFrom, filterParams, groupBy, hueOf, noFacets, propsLine, term, type Facets, type PropFilter } from '$lib/props/props';

	const ready = requireRole('teacher');
	let showArchived = $state(Paged.fromUrl(urlState, 'archived') === '1');
	let filter = $state<PropFilter>(filterFrom(urlState.read()));
	type Group = '' | 'department' | 'year' | 'semester';
	let group = $state<Group>((['department', 'year', 'semester'].includes(Paged.fromUrl(urlState, 'group')) ? Paged.fromUrl(urlState, 'group') : '') as Group);
	// One page of classrooms at a time, newest first (PL-FR-01).
	const list = new Paged<Classroom>(() => '/api/teacher/classrooms', 'classrooms', {
		url: urlState, sort: 'created', desc: true,
		filters: () => ({ archived: showArchived ? '1' : undefined, group: group || undefined, ...filterParams(filter) })
	});
	let facets = $state<Facets>(noFacets());
	async function loadFacets() {
		facets = await api.get<Facets>('/api/teacher/classroom-facets' + (showArchived ? '?archived=1' : '')).catch(() => noFacets());
	}

	// The form opens from the header; a teacher with no classrooms gets it open.
	let adding = $state(false);
	let name = $state('');
	let details = $state(emptyProps());
	let more = $state(false);
	let errors = $state<Record<string, string>>({});
	const filtered = $derived(activeFilters(filter) > 0 || !!list.q || showArchived);
	const formOpen = $derived(adding || (list.loaded && list.total === 0 && !filtered));
	let nameEl = $state<HTMLInputElement>();
	function openForm() {
		adding = true;
		queueMicrotask(() => nameEl?.focus());
	}

	$effect(() => {
		if (ready()) {
			list.load();
			loadFacets();
			loadInvitations();
		}
	});

	// Invitations to co-teach (D-121), answered here.
	type Invitation = { classroom_id: string; classroom: string; owner_name: string; invited_at: string };
	let invitations = $state<Invitation[]>([]);
	async function loadInvitations() {
		invitations = (await api.get<{ invitations: Invitation[] }>('/api/teacher/invitations').catch(() => ({ invitations: [] }))).invitations;
	}
	async function answer(inv: Invitation, accept: boolean) {
		try {
			await api.post(`/api/teacher/invitations/${inv.classroom_id}/${accept ? 'accept' : 'decline'}`);
			toast(accept ? tr("You now co-teach “{0}”", [inv.classroom]) : tr("Invitation declined"));
			invitations = invitations.filter((x) => x.classroom_id !== inv.classroom_id);
			if (accept) list.load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : tr("That didn’t work"), 'error');
		}
	}
	function toggleArchived() {
		showArchived = !showArchived;
		list.refilter();
		loadFacets();
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		try {
			await api.post('/api/teacher/classrooms', { name, props: details });
			toast(tr("Classroom “{0}” created", [name]));
			name = '';
			details = emptyProps();
			more = adding = false;
			list.load();
			loadFacets();
		} catch (err) {
			if (err instanceof ApiError) {
				errors = err.fields;
				if (Object.keys(err.fields).some((k) => k.startsWith('props.'))) more = true;
			} else errors = { name: 'Could not create' };
		}
	}
	const groups = $derived(groupBy(list.rows, group, { department: 'No department', year: 'No year', semester: 'No semester', '': '' }[group]));
	const TONE = { running: 'badge-success', upcoming: 'badge-info', ended: 'badge-ghost' } as const;
</script>

<div class="page-container vstack">
	<div class="row">
		<div>
			<h1 class="m-0">{tr("Classrooms")}</h1>
			{#if list.loaded && list.total}<p class="m-0 small muted">{list.total} {list.total === 1 ? 'classroom' : 'classrooms'}{filtered ? tr(" match") : ''}</p>{/if}
		</div>
		<span class="spacer"></span>
		{#if !formOpen}<button class="btn btn-primary" onclick={openForm}><Icon name="plus" size={16} />{tr("New classroom")}</button>{/if}
	</div>
	{#each invitations as inv (inv.classroom_id)}
		<div class="alert alert-info alert-soft" role="status">
			<Icon name="users" size={18} />
			<span class="grow"><strong>{inv.owner_name}</strong> {tr("invites you to co-teach")} <strong>{inv.classroom}</strong>.</span>
			<button class="btn btn-sm btn-ghost" onclick={() => answer(inv, false)}>{tr("Decline")}</button>
			<button class="btn btn-sm btn-primary" onclick={() => answer(inv, true)}>{tr("Accept")}</button>
		</div>
	{/each}
	{#if formOpen}
		<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-3" onsubmit={create} in:flyIn>
			<div class="row items-end">
				<label class="flex flex-col gap-1" style="flex:1;min-width:14rem"><span>{tr("Classroom name")}</span><input class="input w-full" class:input-error={errors.name} bind:this={nameEl} bind:value={name} placeholder={tr("e.g. MGT/IS/2025")} required /></label>
				<button type="button" class="btn btn-ghost" aria-expanded={more} onclick={() => (more = !more)}><Icon name={more ? 'chevron-down' : 'chevron-right'} size={16} />{tr("Details")}</button>
			</div>
			{#if errors.name}<p class="field-error m-0">{errors.name}</p>{/if}
			{#if more}
				<div in:flyIn>
					<p class="small muted m-0 mb-2">{tr("All optional: they help you find and filter classrooms later.")}</p>
					<PropsFields bind:value={details} {facets} dates {errors} id="new-classroom" />
				</div>
			{/if}
			<div class="flex gap-2">
				<button class="btn btn-primary">{tr("Create classroom")}</button>
				{#if list.total > 0}<button type="button" class="btn btn-ghost" onclick={() => ((adding = false), (errors = {}))}>{tr("Cancel")}</button>{/if}
			</div>
		</form>
	{/if}

	<div class="filters card card-border bg-base-100 p-3 sm:p-4 vstack gap-3">
		<div class="flex flex-wrap items-center gap-3">
			<ListSearch {list} placeholder={tr("Search name, course code, department or tag")} label={tr("Search classrooms")} />
			<label class="row small" style="font-weight:400"><input class="checkbox checkbox-sm" type="checkbox" checked={showArchived} onchange={toggleArchived} /> {tr("Show archived")}</label>
			<span class="spacer"></span>
			<label class="small muted flex items-center gap-2">{tr("Group")}
				<select class="select select-sm w-auto" bind:value={group} onchange={() => list.refilter()}>
					<option value="">{tr("None")}</option><option value="department">{tr("Department")}</option><option value="year">{tr("Year")}</option><option value="semester">{tr("Semester")}</option>
				</select></label>
			<label class="small muted flex items-center gap-2">{tr("Sort")}
				<select class="select select-sm w-auto" value={list.sort} onchange={(e) => list.sortBy(e.currentTarget.value, ['created', 'starts', 'year'].includes(e.currentTarget.value))}>
					<option value="created">{tr("Newest first")}</option><option value="name">{tr("Name A–Z")}</option><option value="starts">{tr("Start date")}</option><option value="year">{tr("Year")}</option>
				</select></label>
		</div>
		<PropsFilters bind:value={filter} {facets} when onchange={() => list.refilter()} />
	</div>

	{#if !list.loaded}
		<div class="auto-grid"><Skeleton lines={3} /><Skeleton lines={3} /><Skeleton lines={3} /></div>
	{:else}
		{#each groups as g (g.label)}
			{#if g.label}<h2 class="group-h">{tr(g.label)} <span class="badge badge-soft badge-sm">{g.rows.length}</span></h2>{/if}
			<div class="auto-grid">
				{#each g.rows as c, i (c.id)}
					{@const t = term(c.props)}
					{@const line = propsLine(c.props, false)}
					<a class="room card card-border interactive bg-base-100 text-inherit no-underline shadow-sm" style="--hue:{c.props.department ? hueOf(c.props.department) : 150}" href={'/t/classrooms/' + c.id} in:flyIn={{ delay: Math.min(i, 8) * 40 }}>
						<div class="p-4 sm:p-5 vstack gap-2">
							<div class="flex items-center gap-2">
								{#if c.props.course_code}<span class="code">{c.props.course_code}</span>{:else}<span class="icon" aria-hidden="true"><Icon name="book" size={16} /></span>{/if}
								{#if t}<span class="badge badge-soft badge-sm {TONE[t.state]}">{tr(t.label)}</span>{/if}
								{#if c.archived}<span class="badge badge-soft badge-sm">{tr("Archived")}</span>{/if}
								<span class="spacer"></span>
								<span class="muted" aria-hidden="true"><Icon name="arrow-right" size={16} /></span>
							</div>
							<h3 class="m-0 text-lg leading-tight">{c.name}</h3>
							{#if line}<p class="small m-0 muted">{line}</p>{/if}
							{#if c.description}<p class="small muted m-0 line-clamp-2">{c.description}</p>{/if}
							{#if t}
								<div class="vstack gap-1">
									<span class="small muted flex items-center gap-1"><Icon name="clock" size={13} />{t.range}</span>
									{#if t.progress !== null}<progress class="progress progress-success h-1.5" value={Math.round(t.progress * 100)} max="100" aria-label={tr("Through the term")}></progress>{/if}
								</div>
							{/if}
							{#if c.props.tags.length}<div class="flex flex-wrap gap-1">{#each c.props.tags as tag (tag)}<span class="badge badge-ghost badge-sm">{tag}</span>{/each}</div>{/if}
						</div>
						<div class="foot small">
							<span class="flex items-center gap-1"><Icon name="users" size={14} />{c.students} {c.students === 1 ? 'student' : 'students'}</span>
							{#if c.role === 'coteacher'}<span class="truncate">{tr("Co-teacher with {0}", [c.owner_name])}</span>{:else}<span class="muted">{tr("Code")} <strong class="tabular">{c.join_code}</strong></span>{/if}
						</div>
					</a>
				{/each}
			</div>
		{:else}
			<div class="card card-border bg-base-100">
				{#if filtered}<EmptyState icon="search" title={tr("No classrooms match")}>{tr("Try fewer filters or another search.")}</EmptyState>
				{:else}<EmptyState icon="book" title={tr("No classrooms yet")}>{tr("Name your first one above. You'll add modules, topics and quizzes inside it.")}</EmptyState>{/if}
			</div>
		{/each}
		<Pager {list} label={tr("Classrooms")} />
	{/if}
</div>

<style>
	.room { display: flex; flex-direction: column; overflow: hidden; border-top: 3px solid oklch(0.68 0.15 var(--hue)); }
	/* The department's hue, mixed with the theme's text so it reads on light and dark themes alike. */
	.code, .icon { color: color-mix(in oklab, oklch(0.62 0.16 var(--hue)) 65%, var(--color-base-content)); background: oklch(0.68 0.15 var(--hue) / 0.14); }
	.code { font-weight: 700; font-size: 0.8rem; letter-spacing: 0.04em; padding: 0.1rem 0.45rem; }
	.icon { display: grid; place-items: center; width: 1.75rem; height: 1.75rem; }
	.foot { display: flex; justify-content: space-between; gap: 0.75rem; margin-top: auto; padding: 0.55rem 1rem; border-top: 1px solid var(--color-base-200); background: color-mix(in oklab, var(--color-base-200) 40%, transparent); }
	.group-h { display: flex; align-items: center; gap: 0.5rem; margin: 0.5rem 0 0; font-size: 1rem; }
</style>
