<script lang="ts">
	// A short piece of text with formulas in it (D-112): an option, a match
	// item, an answer, a caption. Everything shows literally except $…$
	// (inline) and $$…$$ (display), which KaTeX typesets, loaded only when
	// there is a formula. Text with no dollar sign stays plain text.
	import { hasMath, mathTextHTML, typeset } from './plain';

	let { text = '', class: cls = '' }: { text?: string; class?: string } = $props();
	const html = $derived(text.includes('$') ? mathTextHTML(text) : '');
	let el = $state<HTMLElement>();
	$effect(() => {
		if (el && html && hasMath(html)) void typeset(el);
	});
</script>

{#if html}
	{#key html}<span class="mtext {cls}" bind:this={el}>{@html html}</span>{/key}
{:else}
	<span class={cls || undefined}>{text}</span>
{/if}
