<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, safeNext, withBusyRetry } from '$lib/api';
	import { auth, type User } from '$lib/session.svelte';

	let name = $state('');
	let email = $state('');
	let password = $state('');
	let role = $state<'student' | 'teacher'>('student');
	let errors = $state<Record<string, string>>({});
	let message = $state('');
	let busy = $state(false);
	const next = $derived(page.url.searchParams.get('next'));

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		errors = {};
		message = '';
		try {
			const u = await withBusyRetry(() => api.post<User>('/api/auth/register', { name, email, password, role }));
			if (u.status === 'pending_approval') {
				message = 'Your teacher account is waiting for an administrator to approve it. We sent you a verification email.';
				return;
			}
			auth.user = u;
			goto(safeNext(next, auth.home()));
		} catch (err) {
			if (err instanceof ApiError) errors = Object.keys(err.fields).length ? err.fields : { _: err.message };
		} finally {
			busy = false;
		}
	}
</script>

<div class="narrow">
	<form class="card stack" onsubmit={submit}>
		<h1>Create an account</h1>
		{#if message}<p class="alert ok">{message}</p>{/if}
		<fieldset class="row" style="border:none;padding:0">
			<legend class="sr-only">I am a</legend>
			<label class="row" style="font-weight:400"><input type="radio" bind:group={role} value="student" /> Student</label>
			<label class="row" style="font-weight:400"><input type="radio" bind:group={role} value="teacher" /> Teacher</label>
		</fieldset>
		<div>
			<label for="name">Full name</label><input id="name" autocomplete="name" bind:value={name} required />
			{#if errors.name}<p class="field-error">{errors.name}</p>{/if}
		</div>
		<div>
			<label for="email">Email</label><input id="email" type="email" autocomplete="email" bind:value={email} required />
			{#if errors.email}<p class="field-error">{errors.email}</p>{/if}
		</div>
		<div>
			<label for="pw">Password (at least 8 characters)</label>
			<input id="pw" type="password" autocomplete="new-password" minlength="8" bind:value={password} required />
			{#if errors.password}<p class="field-error">{errors.password}</p>{/if}
		</div>
		{#if errors._}<p class="alert danger">{errors._}</p>{/if}
		<button class="primary" disabled={busy}>{busy ? 'Creating…' : 'Create account'}</button>
		<p class="small">Already registered? <a href={'/login' + (next ? '?next=' + encodeURIComponent(next) : '')}>Log in</a></p>
	</form>
</div>
