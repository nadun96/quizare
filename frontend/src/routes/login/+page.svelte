<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, safeNext, withBusyRetry } from '$lib/api';
	import { auth, type User } from '$lib/session.svelte';
	import { ssoError, startUrl, type SSOOption } from '$lib/sso';
	import Icon from '$lib/ui/Icon.svelte';
	import { t } from '$lib/i18n/i18n.svelte';

	let email = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	const next = $derived(page.url.searchParams.get('next'));
	// With school sign-in (D-64) the first field also takes a Windows/school username.
	let school = $state(false);
	// Sign-in with other accounts (D-70), and why one just came back.
	let sso = $state<SSOOption[]>([]);
	const ssoProblem = $derived(ssoError(page.url.searchParams.get('sso_error')));
	$effect(() => {
		api.get<{ directory: boolean; sso: SSOOption[] }>('/api/auth/options').then((o) => {
			school = o.directory;
			sso = o.sso ?? [];
		}, () => {});
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			// Many students log in at once when the QR goes up; the server queues hashing (R1).
			auth.user = await withBusyRetry(() => api.post<User>('/api/auth/login', { email, password }));
			goto(safeNext(next, auth.home()));
		} catch (err) {
			error = err instanceof ApiError ? err.message : t('auth.loginFailed');
		} finally {
			busy = false;
		}
	}
</script>

<div class="narrow">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={submit}>
		<h1>{t('auth.login')}</h1>
		{#if ssoProblem}<p class="alert alert-soft alert-error" role="alert">{ssoProblem}</p>{/if}
		{#if sso.length}
			<div class="vstack gap-2">
				{#each sso as o (o.id)}
					<a class="btn w-full" href={startUrl(o.id, next)} data-sveltekit-reload><Icon name="arrow-right" size={16} />{t('auth.signInWith', { name: o.name })}</a>
				{/each}
			</div>
			<div class="divider my-0 small muted">{t('auth.orPassword')}</div>
		{/if}
		{#if school}
			<div>
				<label for="email">{t('auth.emailOrSchool')}</label>
				<input class="input w-full" id="email" type="text" autocomplete="username" autocapitalize="none" spellcheck="false" bind:value={email} required aria-describedby="school-hint" />
				<p class="small muted m-0 mt-1" id="school-hint">{t('auth.schoolHint')}</p>
			</div>
		{:else}
			<div><label for="email">{t('common.email')}</label><input class="input w-full" id="email" type="email" autocomplete="email" bind:value={email} required /></div>
		{/if}
		<div><label for="pw">{t('common.password')}</label><input class="input w-full" id="pw" type="password" autocomplete="current-password" bind:value={password} required /></div>
		{#if error}<p class="alert alert-soft alert-error" role="alert">{error}</p>{/if}
		<button class="btn btn-primary" disabled={busy}>{busy ? t('auth.loggingIn') : t('auth.login')}</button>
		<p class="small"><a href="/forgot-password">{t('auth.forgot')}</a>{#if school}{' '}<span class="muted">{t('auth.schoolReset')}</span>{/if}</p>
		<p class="small">{t('auth.newHere')} <a href={'/register' + (next ? '?next=' + encodeURIComponent(next) : '')}>{t('auth.createAccount')}</a></p>
	</form>
</div>
