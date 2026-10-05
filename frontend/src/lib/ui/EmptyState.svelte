<script lang="ts">
	// What to do next when a list is empty (research: empty states explain the next step).
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	let { icon = 'info', title, children }: { icon?: IconName; title: string; children?: Snippet } = $props();
</script>

<div class="px-4 py-8 text-center">
	<div class="mx-auto mb-3 grid size-13 place-items-center rounded-full bg-base-200 text-primary"><Icon name={icon} size={24} /></div>
	<p class="m-0 font-semibold">{title}</p>
	{#if children}<div class="small muted mt-1">{@render children()}</div>{/if}
</div>
