<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	let status = $state<'working' | 'ok' | 'error'>('working');
	let message = $state('');
	onMount(async () => {
		try {
			await api.post('/api/auth/verify-email', { token: page.url.searchParams.get('token') });
			status = 'ok';
		} catch (err) {
			status = 'error';
			message = err instanceof ApiError ? err.message : 'Verification failed';
		}
	});
</script>

<div class="narrow card">
	<h1>Email verification</h1>
	{#if status === 'working'}<p>Verifying…</p>
	{:else if status === 'ok'}<p class="alert ok">Your email address is verified. <a href="/">Continue</a></p>
	{:else}<p class="alert danger">{message}</p>{/if}
</div>
