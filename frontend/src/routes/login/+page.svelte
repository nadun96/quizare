<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, safeNext, withBusyRetry } from '$lib/api';
	import { auth, type User } from '$lib/session.svelte';

	let email = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	const next = $derived(page.url.searchParams.get('next'));
	// With school sign-in (D-64) the first field also takes a Windows/school username.
	let school = $state(false);
	$effect(() => {
		api.get<{ directory: boolean }>('/api/auth/options').then((o) => (school = o.directory), () => {});
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
			error = err instanceof ApiError ? err.message : 'Login failed';
		} finally {
			busy = false;
		}
	}
</script>

<div class="narrow">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={submit}>
		<h1>Log in</h1>
		{#if school}
			<div>
				<label for="email">Email or school username</label>
				<input class="input w-full" id="email" type="text" autocomplete="username" autocapitalize="none" spellcheck="false" bind:value={email} required aria-describedby="school-hint" />
				<p class="small muted m-0 mt-1" id="school-hint">Students and teachers: use your school account, the one you sign in to school computers with.</p>
			</div>
		{:else}
			<div><label for="email">Email</label><input class="input w-full" id="email" type="email" autocomplete="email" bind:value={email} required /></div>
		{/if}
		<div><label for="pw">Password</label><input class="input w-full" id="pw" type="password" autocomplete="current-password" bind:value={password} required /></div>
		{#if error}<p class="alert alert-soft alert-error" role="alert">{error}</p>{/if}
		<button class="btn btn-primary" disabled={busy}>{busy ? 'Logging in…' : 'Log in'}</button>
		<p class="small"><a href="/forgot-password">Forgot your password?</a>{#if school}{' '}<span class="muted">School accounts reset it the way your school does.</span>{/if}</p>
		<p class="small">New here? <a href={'/register' + (next ? '?next=' + encodeURIComponent(next) : '')}>Create an account</a></p>
	</form>
</div>
