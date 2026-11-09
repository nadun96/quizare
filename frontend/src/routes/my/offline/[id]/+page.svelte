<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// An offline assignment as a student sees it (D-159): what to do and by
	// when, and their mark once the teacher shows it.
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireLogin } from '$lib/guard.svelte';
	import type { MyOffline } from '$lib/offline/offline';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';

	const ready = requireLogin();
	const id = $derived(page.params.id ?? '');
	let v = $state<MyOffline | null>(null);
	let error = $state('');
	$effect(() => {
		if (!ready() || !id) return;
		api.get<MyOffline>(`/api/my/offline-assignments/${id}`).then(
			(x) => (v = x),
			(e) => (error = e instanceof ApiError && e.status === 404 ? tr("This assignment isn’t in any of your classrooms.") : tr("That didn’t work. Try again."))
		);
	});
	const when = (s: string) => new Date(s).toLocaleString(localeTag(), { dateStyle: 'medium', timeStyle: 'short' });
</script>

<svelte:head><title>{v?.title ?? tr("Assignment")}</title></svelte:head>

<div class="page-container vstack max-w-2xl">
	{#if error}
		<p class="alert alert-soft alert-warning">{error}</p>
	{:else if !v}
		<Skeleton lines={4} />
	{:else}
		<p class="small m-0"><a href={'/my/classrooms/' + v.classroom_id}>← {v.classroom}</a> · {v.topic}</p>
		<h1 class="m-0 flex items-center gap-2"><Icon name="pencil" size={22} />{v.title}</h1>
		<p class="m-0 small muted">{v.mode === 'group' ? tr("Group work") : tr("Your own work")} · {tr("out of {0}", [v.max_marks])}{#if v.due_at} · {tr("due {0}", [when(v.due_at)])}{/if}</p>
		{#if v.instructions}<div class="card card-border bg-base-100 p-4 whitespace-pre-line">{v.instructions}</div>{/if}
		<p class="m-0 small muted">{tr("You hand this in to your teacher, not here. Your mark appears on this page once your teacher shows it.")}</p>
		<section class="card card-border bg-base-100 p-4 vstack gap-1" aria-label={tr("Your mark")}>
			<h2 class="m-0 text-base">{tr("Your mark")}</h2>
			{#if !v.released}
				<p class="m-0 muted">{tr("Marks not released yet")}</p>
			{:else if v.score == null}
				<p class="m-0 muted">{tr("You have no mark for this. Ask your teacher if you think you should.")}</p>
			{:else}
				<p class="m-0 text-2xl font-semibold tabular">{v.score} / {v.max_marks} <span class="text-base muted">({v.pct}%)</span></p>
				{#if v.group}<p class="m-0 small muted">{tr("Your group’s mark: {0}", [v.group])}</p>{/if}
				{#if v.comment}<p class="m-0 whitespace-pre-line">{v.comment}</p>{/if}
			{/if}
		</section>
	{/if}
</div>
