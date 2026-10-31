<script lang="ts">
	// A person's profile (D-98): their name, their email address and whether
	// it's verified, sending the link again, and changing the address, which
	// takes over only once the new inbox confirms it.
	import { api, ApiError } from '../api';
	import { auth } from '../session.svelte';
	import { toast } from '../ui/toast.svelte';

	type EmailStatus = { email: string; verified: boolean; pending_email?: string; can_change: boolean };
	let st = $state<EmailStatus | null>(null);
	let name = $state(auth.user?.name ?? '');
	let newEmail = $state('');
	let password = $state('');
	let changing = $state(false);
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);

	async function load() {
		st = await api.get<EmailStatus>('/api/auth/me/email');
	}
	load();

	function fail(err: unknown, what: string) {
		if (err instanceof ApiError) errors = err.fields;
		toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : what, 'error');
	}

	async function saveName(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		try {
			const r = await api.put<{ name: string }>('/api/auth/me/profile', { name });
			name = r.name;
			if (auth.user) auth.user = { ...auth.user, name: r.name };
			toast('Name saved');
		} catch (err) {
			fail(err, 'Could not save the name');
		}
	}

	async function resend() {
		busy = true;
		try {
			await api.post('/api/auth/me/email/verify');
			toast(`Link sent to ${st?.email}. Check your inbox and spam.`);
		} catch (err) {
			fail(err, 'Could not send the link');
		} finally {
			busy = false;
		}
	}

	async function change(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		busy = true;
		try {
			await api.put('/api/auth/me/email', { email: newEmail, password });
			toast(`Link sent to ${newEmail.trim().toLowerCase()}. Follow it to finish the change.`);
			changing = false;
			newEmail = password = '';
			await load();
		} catch (err) {
			fail(err, 'Could not change the email address');
		} finally {
			busy = false;
		}
	}

	async function cancel() {
		await api.del('/api/auth/me/email/pending');
		toast('Change cancelled');
		await load();
	}
</script>

<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="profile-h">
	<h2 id="profile-h" class="mt-0">Profile</h2>
	{#if auth.user?.directory}
		<p class="m-0"><strong>{auth.user.name}</strong></p>
		<p class="m-0 small muted">Your name and email come from your school account.</p>
	{:else}
		<form class="flex flex-wrap items-end gap-2" onsubmit={saveName}>
			<div class="grow">
				<label for="pf-name" class="font-semibold">Name</label>
				<input id="pf-name" class="input w-full" class:input-error={errors.name} maxlength="100" bind:value={name} required autocomplete="name" />
				{#if errors.name}<p class="small text-error m-0">{errors.name}</p>{/if}
			</div>
			<button class="btn" disabled={name.trim() === auth.user?.name}>Save name</button>
		</form>
	{/if}

	{#if st}
		<div class="vstack gap-1">
			<span class="font-semibold">Email</span>
			<p class="m-0 flex flex-wrap items-center gap-2">
				<span>{st.email}</span>
				{#if st.verified}<span class="badge badge-soft badge-success">Verified</span>{:else}<span class="badge badge-soft badge-warning">Not verified</span>{/if}
			</p>
			{#if !st.verified}
				<p class="m-0 small muted">We sent a link to this address. Didn't get it? Check spam, or send it again.</p>
				<div><button class="btn btn-sm" onclick={resend} disabled={busy}>Send the link again</button></div>
			{/if}
			{#if st.pending_email}
				<div class="alert alert-soft alert-info" role="status">
					<span>Changing to <strong>{st.pending_email}</strong>: follow the link we sent there. Until then you log in with {st.email}.</span>
					<button class="btn btn-sm" onclick={cancel}>Cancel the change</button>
				</div>
			{/if}
			{#if st.can_change && !changing}
				<div><button class="btn btn-sm btn-ghost" onclick={() => (changing = true)}>Change email address</button></div>
			{/if}
		</div>
		{#if changing}
			<form class="vstack" onsubmit={change}>
				<div>
					<label for="pf-email" class="font-semibold">New email address</label>
					<input id="pf-email" class="input w-full" class:input-error={errors.email} type="email" bind:value={newEmail} required autocomplete="email" />
					{#if errors.email}<p class="small text-error m-0">{errors.email}</p>{/if}
				</div>
				<div>
					<label for="pf-pw" class="font-semibold">Your password</label>
					<input id="pf-pw" class="input w-full" class:input-error={errors.password} type="password" bind:value={password} required autocomplete="current-password" />
					{#if errors.password}<p class="small text-error m-0">{errors.password}</p>{/if}
				</div>
				<p class="m-0 small muted">We send a link to the new address. Your email changes once you follow it, and we tell your current address.</p>
				<div class="flex gap-2"><button class="btn btn-primary" disabled={busy}>Send the link</button><button type="button" class="btn btn-ghost" onclick={() => (changing = false)}>Cancel</button></div>
			</form>
		{/if}
	{/if}
	<div><a class="btn btn-sm btn-ghost" href="/api/my/data" download>Download my data</a></div>
</section>
