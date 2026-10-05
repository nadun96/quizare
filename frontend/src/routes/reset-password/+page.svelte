<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	let password = $state('');
	let done = $state(false);
	let error = $state('');
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		try {
			await api.post('/api/auth/password-reset/confirm', { token: page.url.searchParams.get('token'), password });
			done = true;
		} catch (err) {
			error = err instanceof ApiError ? (err.fields.password ?? err.message) : 'Something went wrong';
		}
	}
</script>

<div class="narrow">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={submit}>
		<h1>Choose a new password</h1>
		{#if done}
			<p class="alert alert-soft alert-success">Your password was changed and you were signed out everywhere. <a href="/login">Log in</a>.</p>
		{:else}
			<div><label for="pw">New password</label><input class="input w-full" id="pw" type="password" autocomplete="new-password" minlength="8" bind:value={password} required /></div>
			{#if error}<p class="alert alert-soft alert-error">{error}</p>{/if}
			<button class="btn btn-primary">Change password</button>
		{/if}
	</form>
</div>
