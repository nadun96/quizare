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
	<form class="card stack" onsubmit={submit}>
		<h1>Reset your password</h1>
		{#if done}
			<p class="alert ok">If an account exists for {email}, we have emailed a reset link. It expires in 1 hour.</p>
		{:else}
			<div><label for="email">Email</label><input id="email" type="email" bind:value={email} required /></div>
			{#if error}<p class="alert danger">{error}</p>{/if}
			<button class="primary">Send reset link</button>
		{/if}
	</form>
</div>
