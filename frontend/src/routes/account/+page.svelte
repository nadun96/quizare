<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { auth } from '$lib/session.svelte';
	let password = $state('');
	let error = $state('');
	let confirming = $state(false);
	async function del(e: SubmitEvent) {
		e.preventDefault();
		try {
			await api.del('/api/auth/me', { password });
			auth.user = null;
			goto('/');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not delete the account';
		}
	}
</script>

<div class="container stack" style="max-width:640px">
	<h1>Your account</h1>
	{#if auth.user}
		<div class="card stack">
			<p><strong>{auth.user.name}</strong><br /><span class="muted">{auth.user.email}</span></p>
			<p>Email {auth.user.email_verified ? 'verified' : 'not verified yet — check your inbox'}.</p>
			<a class="button" href="/api/my/data" download>Download my data</a>
		</div>
		{#if auth.user.role !== 'admin'}
			<div class="card stack">
				<h2 style="margin-top:0">Delete account</h2>
				<p class="small muted">Your name and email are removed. Your teachers keep anonymised quiz results.</p>
				{#if !confirming}
					<button class="danger" onclick={() => (confirming = true)}>Delete my account…</button>
				{:else}
					<form class="stack" onsubmit={del}>
						<div><label for="pw">Enter your password to confirm</label><input id="pw" type="password" bind:value={password} required /></div>
						{#if error}<p class="alert danger">{error}</p>{/if}
						<button class="danger">Delete permanently</button>
					</form>
				{/if}
			</div>
		{/if}
	{:else if auth.loaded}
		<p><a href="/login?next=/account">Log in</a> to manage your account.</p>
	{/if}
</div>
