<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// When the server has a newer version (D-137): the next page change loads
	// it in full, and a small notice offers to reload now, in case the teacher
	// stays on this page. Never shown over a quiz, tutoring or a projector.
	import { beforeNavigate } from '$app/navigation';
	import { updated } from '$app/state';
	import Icon from './Icon.svelte';
	import { reloadTarget } from './update';

	let { bare }: { bare: boolean } = $props();
	let dismissed = $state(false);
	beforeNavigate((nav) => {
		const href = reloadTarget(updated.current, nav);
		if (href) location.href = href;
	});
</script>

{#if updated.current && !bare && !dismissed}
	<div class="notice alert alert-info shadow-lg" role="status">
		<Icon name="info" size={18} />
		<span>{tr("A new version is available. Save your work, then reload.")}</span>
		<button class="btn btn-sm btn-primary" onclick={() => location.reload()}>{tr("Reload")}</button>
		<button class="btn btn-sm btn-ghost btn-square" aria-label={tr("Not now")} onclick={() => (dismissed = true)}><Icon name="x" size={16} /></button>
	</div>
{/if}

<style>
	.notice { position: fixed; left: 50%; bottom: 1rem; transform: translateX(-50%); z-index: 60; width: max-content; max-width: calc(100vw - 2rem); }
	@media (max-width: 640px) { .notice { bottom: 4.75rem; } }
</style>
