<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/session.svelte';

	let { children } = $props();

	onMount(() => {
		auth.load();
	});

	// The live quiz and the projector are full-screen experiences without the nav.
	const bare = $derived(page.url.pathname.startsWith('/attempt/') || page.url.pathname.endsWith('/projector') || page.url.pathname.startsWith('/r/'));

	async function logout() {
		await auth.logout();
		goto('/login');
	}
</script>

{#if !bare}
	<nav class="top" aria-label="Main">
		<div class="container">
			<a class="brand" href={auth.user ? auth.home() : '/'}>Classroom Quiz</a>
			{#if auth.user?.role === 'teacher'}
				<a href="/t">Classrooms</a>
				<a href="/t/settings">Settings</a>
			{:else if auth.user?.role === 'student'}
				<a href="/my">My quizzes</a>
				<a href="/join">Join</a>
			{:else if auth.user?.role === 'admin'}
				<a href="/admin">Admin</a>
			{/if}
			<span class="spacer"></span>
			{#if auth.user}
				<span class="muted small">{auth.user.name}</span>
				<a href="/account" class="small">Account</a>
				<button class="small" onclick={logout}>Log out</button>
			{:else if auth.loaded}
				<a href="/login">Log in</a>
				<a href="/register">Sign up</a>
			{/if}
		</div>
	</nav>
{/if}

<main>
	{@render children()}
</main>
