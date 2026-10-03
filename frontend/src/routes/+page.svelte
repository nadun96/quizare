<script lang="ts">
	import { goto } from '$app/navigation';
	import { auth } from '$lib/session.svelte';

	$effect(() => {
		if (auth.loaded && auth.user) goto(auth.home(), { replaceState: true });
	});
</script>

<div class="narrow stack">
	<h1>Classroom Quiz</h1>
	<p>Timed, proctored quizzes that students join by scanning a QR code.</p>
	<div class="row">
		<a class="button primary" href="/login">Log in</a>
		<a class="button" href="/register">Create an account</a>
	</div>
	<p class="small muted">Have a quiz code from your teacher? <a href="/join">Join a session</a>.</p>
</div>
