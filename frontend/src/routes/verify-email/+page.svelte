<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth } from '$lib/session.svelte';
	import { t } from '$lib/i18n/i18n.svelte';
	let status = $state<'working' | 'ok' | 'error'>('working');
	let message = $state('');
	onMount(async () => {
		try {
			await api.post('/api/auth/verify-email', { token: page.url.searchParams.get('token') });
			status = 'ok';
			auth.load(); // the address, or a new one, is now verified
		} catch (err) {
			status = 'error';
			message = err instanceof ApiError ? err.message : t('auth.verifyFailed');
		}
	});
</script>

<div class="narrow card card-border bg-base-100 shadow-sm p-4 sm:p-6">
	<h1>{t('auth.verifyTitle')}</h1>
	{#if status === 'working'}<p>{t('auth.verifying')}</p>
	{:else if status === 'ok'}<p class="alert alert-soft alert-success">{t('auth.verified')} <a href="/">{t('auth.continue')}</a></p>
	{:else}<p class="alert alert-soft alert-error">{message}</p>{/if}
</div>
