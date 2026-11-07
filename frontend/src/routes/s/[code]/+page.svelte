<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Joining a live presentation with its code or QR code (D-77).
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';

	const code = $derived(appPage.params.code ?? '');
	let problem = $state('');
	$effect(() => {
		if (!auth.loaded || problem) return;
		if (!auth.user) {
			goto(loginUrl('/s/' + code), { replaceState: true });
			return;
		}
		api.get<{ id: string }>('/api/presentations/code/' + encodeURIComponent(code)).then(
			(r) => goto(`/slides/${r.id}/live`, { replaceState: true }),
			(e) => (problem = e instanceof ApiError && e.status === 404 ? tr("No presentation is live with this code for your classes. Check the code with your teacher.") : tr("Could not join; try again."))
		);
	});
</script>

<svelte:head><title>{tr("Join a presentation")}</title></svelte:head>

<div class="page-container" style="max-width:36rem">
	{#if problem}
		<div class="card card-border bg-base-100 shadow-sm p-6 text-center vstack" role="alert">
			<h1 class="m-0">{tr("Can’t join {0}", [code])}</h1>
			<p class="muted m-0">{problem}</p>
			<a class="btn mx-auto" href="/my">{tr("Back to my page")}</a>
		</div>
	{:else}<Skeleton lines={3} />{/if}
</div>
