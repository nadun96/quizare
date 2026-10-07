<script lang="ts">
	// A dashboard number that counts to its new value (readable from across the
	// room; motion explains that something changed). Tabular figures, no jitter.
	import { Tween } from 'svelte/motion';
	import { cubicOut } from 'svelte/easing';
	import Icon, { type IconName } from './Icon.svelte';
	import { reduced } from './motion';

	let { label, value, icon, tone = '', sub = '' }: { label: string; value: number; icon: IconName; tone?: '' | 'primary' | 'success' | 'warning' | 'error'; sub?: string } = $props();

	const shown = new Tween(0, { duration: 400, easing: cubicOut });
	$effect(() => {
		shown.set(value, reduced() ? { duration: 0 } : undefined);
	});
	let bump = $state(false);
	let prev: number | null = null;
	$effect(() => {
		if (prev !== null && value !== prev && !reduced()) {
			bump = true;
			setTimeout(() => (bump = false), 450);
		}
		prev = value;
	});
</script>

<div class="stat stat-card rounded-box border bg-base-100 {tone}" class:bump role="listitem">
	<div class="stat-figure"><span class="stat-icon" aria-hidden="true"><Icon name={icon} size={18} /></span></div>
	<div class="stat-title">{label}</div>
	<div class="stat-value tabular" aria-hidden="true">{Math.round(shown.current)}</div>
	{#if sub}<div class="stat-desc">{sub}</div>{/if}
	<span class="sr-only">{label}: {value}{sub ? `, ${sub}` : ''}</span>
</div>

<style>
	/* daisyUI stat; the border, icon tint and "bump" on change are ours. */
	.stat-card { border-color: var(--color-base-300); padding: 0.875rem 1rem; transition: transform var(--motion) var(--ease-out), box-shadow var(--motion); }
	.stat-card.bump { transform: translateY(-2px); box-shadow: 0 6px 18px -10px color-mix(in oklab, var(--color-base-content) 40%, transparent); }
	.stat-icon { width: 2.25rem; height: 2.25rem; border-radius: 0.6rem; display: grid; place-items: center; background: var(--color-base-200); color: var(--color-muted); }
	.primary .stat-icon { background: color-mix(in oklab, var(--color-primary) 14%, transparent); color: var(--color-primary); }
	.success .stat-icon { background: color-mix(in oklab, var(--color-success) 14%, transparent); color: var(--color-success); }
	.warning .stat-icon { background: color-mix(in oklab, var(--color-warning) 14%, transparent); color: var(--color-warning); }
	.error .stat-icon { background: color-mix(in oklab, var(--color-error) 14%, transparent); color: var(--color-error); }
	.error { border-color: color-mix(in oklab, var(--color-error) 40%, var(--color-base-300)); }
	.stat-card .stat-value { font-size: 1.75rem; line-height: 1.1; }
</style>
