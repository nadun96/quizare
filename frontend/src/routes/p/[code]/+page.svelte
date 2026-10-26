<script lang="ts">
	// Taking part in a poll (D-40): the poll itself is PollTake, also shown in tutoring sessions (D-86).
	import { page } from '$app/state';
	import PollTake from '$lib/poll/PollTake.svelte';
</script>

<PollTake code={page.params.code ?? ''} />
