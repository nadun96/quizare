<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { auth } from '$lib/session.svelte';
	import DisplayMenu from '$lib/ui/DisplayMenu.svelte';
	import AccountSecurity from '$lib/account/AccountSecurity.svelte';
	import ConnectedSignIns from '$lib/account/ConnectedSignIns.svelte';
	import ProfileCard from '$lib/account/ProfileCard.svelte';
	let password = $state('');
	let error = $state('');
	let confirming = $state(false);
	async function del(e: SubmitEvent) {
		e.preventDefault();
		try {
			await api.del('/api/auth/me', { password });
			auth.user = null;
			goto('/');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not delete the account';
		}
	}
</script>

<div class="page-container vstack">
	<h1 class="m-0">Your account</h1>
	{#if auth.user}
	<!-- Two columns on wide screens (D-110): who you are on the left, how the app behaves on the right. -->
	<div class="acct">
		<div class="vstack">
		<ProfileCard />
		<AccountSecurity />
		</div>
		<div class="vstack">
		<ConnectedSignIns />
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
			<h2 class="mt-0">Display</h2>
			<p class="small muted">Saved on this device. Larger text and reduced motion also apply during quizzes.</p>
			<DisplayMenu idPrefix="acct" />
		</div>
		{#if auth.user.role !== 'admin' && auth.user.role !== 'manager'}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
				<h2 style="margin-top:0">Delete account</h2>
				<p class="small muted">Your name and email are removed. Your teachers keep anonymised quiz results.</p>
				{#if !confirming}
					<button class="btn btn-error btn-outline" onclick={() => (confirming = true)}>Delete my account…</button>
				{:else}
					<form class="vstack" onsubmit={del}>
						<div><label for="pw">{auth.user?.directory ? 'Enter your school password to confirm' : 'Enter your password to confirm'}</label><input class="input w-full" id="pw" type="password" bind:value={password} required /></div>
						{#if error}<p class="alert alert-soft alert-error">{error}</p>{/if}
						<button class="btn btn-error btn-outline">Delete permanently</button>
					</form>
				{/if}
			</div>
		{/if}
		</div>
	</div>
	{:else if auth.loaded}
		<p><a href="/login?next=/account">Log in</a> to manage your account.</p>
	{/if}
</div>

<style>
	.acct { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(min(100%, 28rem), 1fr)); align-items: start; max-width: 76rem; }
	.acct > :global(*) { margin: 0; }
</style>
