<script lang="ts">
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { t } from '$lib/i18n/i18n.svelte';
	let password = $state('');
	let done = $state(false);
	let error = $state('');
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		try {
			await api.post('/api/auth/password-reset/confirm', { token: page.url.searchParams.get('token'), password });
			done = true;
		} catch (err) {
			error = err instanceof ApiError ? (err.fields.password ?? err.message) : t('common.somethingWrong');
		}
	}
</script>

<div class="narrow">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={submit}>
		<h1>{t('auth.newPasswordTitle')}</h1>
		{#if done}
			<p class="alert alert-soft alert-success">{t('auth.changed')} <a href="/login">{t('auth.login')}</a>.</p>
		{:else}
			<div><label for="pw">{t('auth.newPassword')}</label><input class="input w-full" id="pw" type="password" autocomplete="new-password" minlength="8" bind:value={password} required /></div>
			{#if error}<p class="alert alert-soft alert-error">{error}</p>{/if}
			<button class="btn btn-primary">{t('auth.changePassword')}</button>
		{/if}
	</form>
</div>
