<script lang="ts">
	// A teacher's poll page in the cartoon look (D-151).
	import type { Snippet } from 'svelte';
	import '@fontsource-variable/fredoka';
	import './toon.css';
	import { toonVars } from './toon';

	let { children }: { children: Snippet } = $props();
</script>

<div class="toon toon-page" data-theme="quiz" style={toonVars}>
	{@render children()}
</div>

<style>
	.toon-page { min-height: calc(100dvh - 4rem); }
</style>
