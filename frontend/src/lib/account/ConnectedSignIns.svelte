<script lang="ts">
	// The account's sign-ins with other providers (D-70): connect one, so it
	// opens this account whatever its email, or disconnect it.
	import { page } from '$app/state';
	import { api, ApiError } from '../api';
	import { ssoError, type Identity, type SSOOption } from '../sso';
	import Icon from '../ui/Icon.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';

	let identities = $state<Identity[] | null>(null);
	let available = $state<SSOOption[]>([]);

	async function load() {
		const r = await api.get<{ identities: Identity[]; available: SSOOption[] }>('/api/auth/oidc/identities');
		identities = r.identities;
		available = r.available;
	}
	$effect(() => {
		load();
	});

	// Coming back from the provider.
	$effect(() => {
		const q = page.url.searchParams;
		if (q.get('sso') === 'linked') toast('Sign-in connected');
		else if (q.get('sso_error')) toast(ssoError(q.get('sso_error')), 'error');
		if (q.has('sso') || q.has('sso_error')) history.replaceState(history.state, '', page.url.pathname);
	});

	const connected = (id: string) => identities?.some((i) => i.provider_id === id);

	async function disconnect(i: Identity) {
		if (!(await confirmDialog({ title: `Disconnect ${i.name || 'this sign-in'}?`, body: 'You won’t be able to sign in with it until you connect it again.', confirm: 'Disconnect' }))) return;
		try {
			await api.del('/api/auth/oidc/identities/' + i.provider_id);
			toast('Disconnected');
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not disconnect', 'error');
		}
	}
</script>

{#if identities && (identities.length || available.length)}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
		<h2 class="mt-0">Sign-in with other accounts</h2>
		<p class="small muted m-0">Sign in here with an account you already have elsewhere.</p>
		<ul class="ids">
			{#each identities as i (i.provider_id)}
				<li>
					<Icon name="check-circle" size={18} />
					<span class="min-w-0 flex-1"><strong>{i.name || 'A removed provider'}</strong><br /><span class="small muted truncate">{i.email || 'Connected'}</span></span>
					<button class="btn btn-sm" onclick={() => disconnect(i)}>Disconnect</button>
				</li>
			{/each}
			{#each available.filter((o) => !connected(o.id)) as o (o.id)}
				<li class="off">
					<Icon name="link" size={18} />
					<span class="min-w-0 flex-1"><strong>{o.name}</strong><br /><span class="small muted">Not connected</span></span>
					<a class="btn btn-sm" href={'/api/auth/oidc/' + encodeURIComponent(o.id) + '/link'} data-sveltekit-reload>Connect</a>
				</li>
			{/each}
		</ul>
	</div>
{/if}

<style>
	.ids { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
	.ids li { display: flex; align-items: center; gap: 0.75rem; padding: 0.6rem 0; border-top: 1px solid var(--color-base-300); }
	.ids li :global(svg) { color: var(--color-success); flex: none; }
	.ids li.off :global(svg) { color: var(--color-muted); }
</style>
