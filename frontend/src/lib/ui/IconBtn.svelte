<script lang="ts">
	// An icon control with its label (D-48): a daisyUI tooltip on hover and
	// keyboard focus, or the label shown next to the icon when the user turns
	// on "Always show button labels". Screen readers always get the label.
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import Icon, { type IconName } from './Icon.svelte';
	import { prefs } from './prefs.svelte';

	type Props = Omit<HTMLButtonAttributes, 'class'> & {
		icon: IconName;
		/** Short visible label ("Rename"). */
		label: string;
		/** Fuller name for the tooltip and screen readers ("Rename Group 1"); must contain the label. */
		hint?: string;
		/** daisyUI button classes, e.g. "btn-ghost btn-sm". */
		class?: string;
		size?: number;
		tip?: 'top' | 'bottom' | 'left' | 'right';
		/** Render a link instead of a button. */
		href?: string;
		target?: string;
		/** Show the label whatever the preference (for primary actions). */
		showLabel?: boolean;
	};
	let { icon, label, hint, class: cls = 'btn-ghost btn-sm', size = 16, tip = 'top', href, target, showLabel = false, ...rest }: Props = $props();
	const name = $derived(hint ?? label);
	const always = $derived(showLabel || prefs.labels === 'always');
</script>

{#snippet inner()}
	<Icon name={icon} {size} />
	{#if always}<span>{label}</span>{/if}
{/snippet}

{#if always}
	{#if href}
		<a class="btn gap-1 {cls}" {href} {target} rel={target ? 'noopener' : undefined} aria-label={hint}>{@render inner()}</a>
	{:else}
		<button type="button" class="btn gap-1 {cls}" aria-label={hint} {...rest}>{@render inner()}</button>
	{/if}
{:else}
	<span class="tooltip tooltip-{tip}" data-tip={name}>
		{#if href}
			<a class="btn btn-square {cls}" {href} {target} rel={target ? 'noopener' : undefined} aria-label={name}>{@render inner()}</a>
		{:else}
			<button type="button" class="btn btn-square {cls}" aria-label={name} {...rest}>{@render inner()}</button>
		{/if}
	</span>
{/if}
