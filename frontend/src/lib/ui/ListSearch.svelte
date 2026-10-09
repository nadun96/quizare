<script lang="ts" generics="T">
	// Search box for a server-paginated list (TS-FR-93): searches every page,
	// not just the rows shown, and goes back to page 1. Typing waits 300 ms
	// before asking the server; Enter searches at once.
	import type { Paged } from '../paged.svelte';
	import Icon from './Icon.svelte';

	let { list, placeholder = 'Search', label = 'Search' }: { list: Paged<T>; placeholder?: string; label?: string } = $props();
	let text = $state('');
	// Start from the list's search (from the address), and follow it if it changes elsewhere.
	$effect.pre(() => {
		text = list.q;
	});
	let timer: ReturnType<typeof setTimeout> | null = null;
	function go() {
		if (timer) clearTimeout(timer);
		timer = null;
		if (text.trim() !== list.q.trim()) list.search(text);
	}
	function typed() {
		if (timer) clearTimeout(timer);
		timer = setTimeout(go, 300);
	}
</script>

<label class="input input-sm w-full max-w-xs">
	<Icon name="search" size={14} />
	<input type="search" aria-label={label} {placeholder} bind:value={text} oninput={typed} onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), go())} />
</label>
