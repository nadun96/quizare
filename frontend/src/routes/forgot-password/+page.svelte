<script lang="ts">
	import { api, ApiError } from '$lib/api';
	let email = $state('');
	let done = $state(false);
	let error = $state('');
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		try {
			await api.post('/api/auth/password-reset/request', { email });
			done = true;
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Something went wrong';
		}
	}
</script>

<div class="narrow">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={submit}>
		<h1>Reset your password</h1>
		{#if done}
			<p class="alert alert-soft alert-success">If an account exists for {email}, we have emailed a reset link. It expires in 1 hour.</p>
		{:else}
			<div><label for="email">Email</label><input class="input w-full" id="email" type="email" bind:value={email} required /></div>
			{#if error}<p class="alert alert-soft alert-error">{error}</p>{/if}
			<button class="btn btn-primary">Send reset link</button>
		{/if}
	</form>
</div>
