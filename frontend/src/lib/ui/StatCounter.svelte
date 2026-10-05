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

<div class="stat-card {tone}" class:bump role="listitem">
	<span class="stat-icon" aria-hidden="true"><Icon name={icon} size={18} /></span>
	<div>
		<div class="stat-num tabular" aria-hidden="true">{Math.round(shown.current)}</div>
		<div class="stat-label">{label}{#if sub} <span class="muted ml-1">· {sub}</span>{/if}</div>
		<span class="sr-only">{label}: {value}</span>
	</div>
</div>

<style>
	.stat-card { display: flex; gap: 0.75rem; align-items: center; padding: 0.875rem 1rem; border-radius: var(--radius-box); background: var(--color-base-100); border: 1px solid var(--color-base-300); transition: transform var(--motion) var(--ease-out), box-shadow var(--motion); }
	.stat-card.bump { transform: translateY(-2px); box-shadow: 0 6px 18px -10px color-mix(in oklab, var(--color-base-content) 40%, transparent); }
	.stat-icon { width: 2.25rem; height: 2.25rem; border-radius: 0.6rem; display: grid; place-items: center; background: var(--color-base-200); color: var(--color-muted); flex: none; }
	.primary .stat-icon { background: color-mix(in oklab, var(--color-primary) 14%, transparent); color: var(--color-primary); }
	.success .stat-icon { background: color-mix(in oklab, var(--color-success) 14%, transparent); color: var(--color-success); }
	.warning .stat-icon { background: color-mix(in oklab, var(--color-warning) 14%, transparent); color: var(--color-warning); }
	.error .stat-icon { background: color-mix(in oklab, var(--color-error) 14%, transparent); color: var(--color-error); }
	.error { border-color: color-mix(in oklab, var(--color-error) 40%, var(--color-base-300)); }
	.stat-num { font-size: 1.75rem; font-weight: 750; line-height: 1.1; }
	.stat-label { font-size: 0.85rem; color: var(--color-muted); font-weight: 600; }
</style>
