<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Explore for teachers (D-120): their classrooms, every measure and breakdown.
	import Explore from '$lib/insight/Explore.svelte';
</script>

<svelte:head><title>{tr("Explore · Classroom Quiz")}</title></svelte:head>

<div class="page-container vstack">
	<div>
		<h1 class="m-0">{tr("Explore")}</h1>
		<p class="m-0 muted">{tr("Any measure, broken down any way: by date, classroom, module, topic, quiz, question type or student.")}</p>
	</div>
	<Explore />
</div>
