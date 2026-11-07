<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A classroom's question pool tab (D-123): import into it, then its
	// questions by category. Quizzes in the classroom draw so many from each
	// category under their Pools.
	import { api, ApiError } from '../api';
	import Skeleton from '../ui/Skeleton.svelte';
	import PoolBrowser from './PoolBrowser.svelte';
	import PoolImporter from './PoolImporter.svelte';
	import PoolTabs from './PoolTabs.svelte';
	import type { PoolSummary } from './pools';

	let { classroomId, classroomName }: { classroomId: string; classroomName: string } = $props();
	let poolId = $state('');
	let error = $state('');
	let version = $state(0);
	$effect(() => {
		api.get<{ id: string }>(`/api/teacher/classrooms/${classroomId}/pool`).then(
			(q) => (poolId = q.id),
			(e) => (error = e instanceof ApiError ? e.message : tr("The pool didn’t load"))
		);
	});
	const pools = $derived<PoolSummary[]>([{ kind: 'pool', quiz_id: poolId, classroom_id: classroomId, classroom: classroomName, questions: 0, categories: [] }]);
</script>

<div class="vstack gap-4">
	<div>
		<h2 class="m-0 text-lg">{tr("Question pool")}</h2>
		<p class="m-0 small muted">{tr("Questions for this classroom’s quizzes, by category, shared with its co-teachers. In a quiz’s")} <strong>{tr("Pools")}</strong>{tr(", choose how many each student gets from each category, for example 10 from each of 5 categories for a 50-question quiz.")}</p>
	</div>
	{#if error}<p class="alert alert-soft alert-error m-0">{error}</p>
	{:else if !poolId}<Skeleton lines={4} />
	{:else}
		<PoolTabs quizId={poolId}>
			{#snippet questions()}
				<PoolImporter {pools} {classroomId} onimported={() => version++} />
				{#key version}<PoolBrowser quizId={poolId} />{/key}
			{/snippet}
		</PoolTabs>
	{/if}
</div>
