<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A one-line field that can hold formulas (D-112): options, matching
	// pairs, drag items and boxes. Σ wraps the selection in $…$ (or starts an
	// empty formula at the caret); what's between dollar signs shows typeset
	// underneath, so the teacher sees what students will see.
	import Icon from '../ui/Icon.svelte';
	import MathText from './MathText.svelte';
	import { textHasMath } from './plain';

	let { value = $bindable(''), label, required = false, placeholder = '' }: { value: string; label: string; required?: boolean; placeholder?: string } = $props();
	let input = $state<HTMLInputElement>();

	function formula() {
		if (!input) return;
		const a = input.selectionStart ?? value.length;
		const b = input.selectionEnd ?? value.length;
		const inner = value.slice(a, b);
		value = value.slice(0, a) + '$' + (inner || 'x') + '$' + value.slice(b);
		requestAnimationFrame(() => {
			input!.focus();
			// Select what's inside, ready to type over.
			input!.setSelectionRange(a + 1, a + 1 + (inner || 'x').length);
		});
	}
</script>

<span class="ff">
	<span class="ff-row">
		<input class="input w-full" bind:this={input} bind:value aria-label={label} {required} {placeholder} />
		<button type="button" class="btn btn-sm btn-ghost btn-square" title={tr("Formula: wrap in $…$")} aria-label={tr("Insert a formula in {0}", [label])} onclick={formula}><Icon name="sigma" size={16} /></button>
	</span>
	{#if textHasMath(value)}<span class="ff-preview small"><MathText text={value} /></span>{/if}
</span>

<style>
	.ff { display: grid; gap: 0.25rem; flex: 1; min-width: 0; }
	.ff-row { display: flex; gap: 0.25rem; align-items: center; }
	.ff-preview { padding: 0.25rem 0.6rem; background: var(--color-base-200); border-left: 3px solid var(--color-primary); overflow-x: auto; }
</style>
