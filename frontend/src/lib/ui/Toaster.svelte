<script lang="ts">
	import { flip } from 'svelte/animate';
	import Icon from './Icon.svelte';
	import { flyIn, fadeIn, flipMs } from './motion';
	import { toasts } from './toast.svelte';

	const ICON = { success: 'check-circle', info: 'info', warning: 'alert', error: 'alert' } as const;
	const CLS = { success: 'alert-success', info: 'alert-info', warning: 'alert-warning', error: 'alert-error' } as const;
</script>

<div class="toast toast-end toast-bottom z-50 p-3 sm:p-4" role="status" aria-live="polite">
	{#each toasts.list as t (t.id)}
		<div class="alert {CLS[t.kind]} shadow-lg max-w-sm" in:flyIn={{ y: 16 }} out:fadeIn={{ duration: 150 }} animate:flip={{ duration: flipMs() }}>
			<Icon name={ICON[t.kind]} />
			<span>{t.text}</span>
			<button class="btn btn-ghost btn-sm btn-circle" aria-label="Dismiss" onclick={() => toasts.dismiss(t.id)}><Icon name="x" size={16} /></button>
		</div>
	{/each}
</div>
