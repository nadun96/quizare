<script lang="ts">
	// Explore for teachers (D-120): their classrooms, every measure and breakdown.
	import Explore from '$lib/insight/Explore.svelte';
</script>

<svelte:head><title>Explore · Classroom Quiz</title></svelte:head>

<div class="page-container vstack">
	<div>
		<h1 class="m-0">Explore</h1>
		<p class="m-0 muted">Any measure, broken down any way: by date, classroom, module, topic, quiz, question type or student.</p>
	</div>
	<Explore />
</div>
