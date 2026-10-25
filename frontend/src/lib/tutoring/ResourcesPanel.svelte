<script lang="ts">
	// What the session uses, for its teachers (D-81, TS-FR-33): the estimate
	// the service pushes every 2 seconds, against this session's share and the
	// whole server's budget; what the service switched off to stay within it,
	// to turn back on when there's room (TS-FR-39); and the session's plan.
	import { ApiError } from '../api';
	import { toast } from '../ui/toast.svelte';
	import { sentence, withBudget } from './budget';
	import type { TutorClient } from './client';
	import PlanEditor from './PlanEditor.svelte';
	import { mbps, meter, STEP_LABELS, type AutoOffStep, type Resources, type TutoringView } from './types';

	let { client, view, resources }: { client: TutorClient; view: TutoringView; resources: Resources | null } = $props();
	const path = $derived('/api/sessions/' + view.session.id);
	const mine = $derived(resources ? meter(resources.load.mbps, resources.limit_mbps) : null);
	const all = $derived(resources ? meter(resources.total_mbps, resources.budget_mbps) : null);
	const live = $derived(view.session.status === 'live');

	async function restore(step: AutoOffStep) {
		try {
			if (await withBudget((force) => client.post(path + '/budget/restore', { step, force }), 'Turn it back on')) toast(`${STEP_LABELS[step]}: back on`);
		} catch (e) {
			toast(e instanceof ApiError ? sentence(e.message) : 'That didn’t work', 'error');
		}
	}
</script>

<section class="vstack gap-3" aria-label="Resources">
	{#if !live}
		<p class="small muted m-0">Once the session is live, what it sends shows here, every 2 seconds.</p>
	{:else if !resources || !mine || !all}
		<p class="small muted m-0">Measuring…</p>
	{:else}
		<div class="vstack gap-1">
			<p class="m-0"><strong class="tabular">{mbps(resources.load.mbps)}</strong> <span class="small muted">this session, estimated · {resources.load.viewers} watching</span></p>
			<progress class="progress w-full" class:progress-warning={mine.level === 'warn'} class:progress-error={mine.level === 'over'} value={mine.pct} max="100" aria-label="This session against its share"></progress>
			<p class="small muted m-0">Its share now: {mbps(resources.limit_mbps)}. The plan expects {mbps(resources.plan.mbps)}.</p>
		</div>
		<div class="vstack gap-1">
			<p class="m-0 small">All tutoring: <strong class="tabular">{mbps(resources.total_mbps)}</strong> of {mbps(resources.budget_mbps)}{#if all.level === 'over'}{' '}<span class="badge badge-error badge-sm">over budget</span>{:else if all.level === 'warn'}{' '}<span class="badge badge-warning badge-sm">nearly full</span>{/if}</p>
			<progress class="progress w-full" class:progress-warning={all.level === 'warn'} class:progress-error={all.level === 'over'} value={all.pct} max="100" aria-label="All tutoring against the budget"></progress>
		</div>
		{#if resources.load.items.length}
			<table class="table table-xs">
				<tbody>{#each resources.load.items as it (it.name)}<tr><td>{it.name}</td><td class="tabular text-right">{mbps(it.mbps)}</td></tr>{/each}</tbody>
			</table>
		{/if}
	{/if}

	{#if view.session.auto_off.length}
		<div class="alert alert-soft alert-warning py-2" role="status">
			<div class="vstack gap-2 w-full">
				<span class="small">Switched off to stay within the server’s bandwidth:</span>
				{#each view.session.auto_off as step (step)}
					<div class="row gap-2 w-full"><span class="small flex-1">{STEP_LABELS[step]}</span><button class="btn btn-xs" onclick={() => restore(step)}>Turn back on</button></div>
				{/each}
			</div>
		</div>
	{/if}

	<details open={view.session.status === 'open'}>
		<summary class="font-semibold">Plan</summary>
		<div class="mt-2">
			<PlanEditor {client} sessionId={view.session.id} plan={view.session.plan} readonly={view.me.role !== 'teacher'} />
		</div>
	</details>
	<p class="small muted m-0">Figures are estimates from what is sent and how many receive it.</p>
</section>
