<script lang="ts">
	// The teacher's question bank (D-66): one per teacher, created on first visit.
	import { api } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import QuizWorkspace from '$lib/quiz/QuizWorkspace.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import type { Quiz } from '$lib/types';

	const ready = requireRole('teacher');
	let bankId = $state('');
	$effect(() => {
		if (ready() && !bankId) api.get<Quiz>('/api/teacher/bank').then((b) => (bankId = b.id));
	});
</script>

{#if bankId}<QuizWorkspace quizId={bankId} />{:else}<div class="page-container"><Skeleton lines={4} /></div>{/if}
