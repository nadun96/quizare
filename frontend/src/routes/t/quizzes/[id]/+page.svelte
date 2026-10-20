<script lang="ts">
	import { page } from '$app/state';
	import QuizWorkspace from '$lib/quiz/QuizWorkspace.svelte';
</script>

{#key page.params.id}<QuizWorkspace quizId={page.params.id ?? ''} />{/key}
