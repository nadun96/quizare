<script lang="ts">
	import { goto } from '$app/navigation';
	import { auth } from '$lib/session.svelte';
	import Icon, { type IconName } from '$lib/ui/Icon.svelte';
	import { flyIn } from '$lib/ui/motion';

	$effect(() => {
		if (auth.loaded && auth.user) goto(auth.home(), { replaceState: true });
	});

	const points: [IconName, string, string][] = [
		['qr', 'Join in seconds', 'Students scan the QR code on the board. No app to install.'],
		['clock', 'Fair timing', 'Timers run on the server, and teachers can pause or extend.'],
		['sparkles', 'Faster marking', 'Answer keys mark instantly; essays can be marked by your own LLM key.']
	];
</script>

<section class="page-container landing">
	<div class="readable mx-auto text-center" in:flyIn>
		<span class="badge badge-soft badge-primary mb-4">For classrooms</span>
		<h1 class="hero-title">Timed, proctored quizzes students join with a QR code</h1>
		<p class="lead">Run a live quiz on every phone in the room, watch progress as it happens, and release results when you're ready.</p>
		<div class="mt-6 flex flex-wrap justify-center gap-3">
			<a class="btn btn-primary btn-lg" href="/login">Log in</a>
			<a class="btn btn-lg" href="/register">Create an account</a>
		</div>
		<p class="small muted mt-4">Have a code from your teacher? <a href="/join">Join a quiz</a>.</p>
	</div>
	<div class="points mt-12">
		{#each points as [icon, title, body], i (title)}
			<div class="card card-border bg-base-100 p-5 shadow-sm" in:flyIn={{ delay: 80 + i * 60 }}>
				<span class="mb-3 grid size-10 place-items-center rounded-xl bg-primary/10 text-primary" aria-hidden="true"><Icon name={icon} size={20} /></span>
				<h2 class="m-0 text-base">{title}</h2>
				<p class="small muted mb-0 mt-1">{body}</p>
			</div>
		{/each}
	</div>
</section>

<style>
	.landing { padding-top: 3rem; padding-bottom: 2rem; }
	.hero-title { font-size: clamp(1.75rem, 1.2rem + 2.4vw, 2.75rem); line-height: 1.15; }
	.lead { font-size: 1.125rem; color: var(--color-muted); }
	.points { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(min(16rem, 100%), 1fr)); }
	.points .card + .card { margin-top: 0; }
</style>
