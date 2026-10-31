<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { t } from '$lib/i18n/i18n.svelte';
	let email = $state('');
	let done = $state(false);
	let error = $state('');
	let school = $state(false);
	$effect(() => {
		api.get<{ directory: boolean }>('/api/auth/options').then((o) => (school = o.directory), () => {});
	});
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		try {
			await api.post('/api/auth/password-reset/request', { email });
			done = true;
		} catch (err) {
			error = err instanceof ApiError ? err.message : t('common.somethingWrong');
		}
	}
</script>

<div class="narrow">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={submit}>
		<h1>{t('auth.resetTitle')}</h1>
		{#if school}<p class="alert alert-soft alert-info small m-0">{t('auth.resetSchool')}</p>{/if}
		{#if done}
			<p class="alert alert-soft alert-success">{t('auth.resetSent', { email })}</p>
		{:else}
			<div><label for="email">{t('common.email')}</label><input class="input w-full" id="email" type="email" bind:value={email} required /></div>
			{#if error}<p class="alert alert-soft alert-error">{error}</p>{/if}
			<button class="btn btn-primary">{t('auth.sendReset')}</button>
		{/if}
	</form>
</div>
