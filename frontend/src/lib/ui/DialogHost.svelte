<script lang="ts">
	// The single confirmation dialog (lib/ui/dialog.svelte.ts). A native modal
	// <dialog>: focus moves into it, Escape cancels, focus returns afterwards.
	import { dialogs } from './dialog.svelte';
	import Icon from './Icon.svelte';

	let el = $state<HTMLDialogElement>();
	let confirmBtn = $state<HTMLButtonElement>();
	let cancelBtn = $state<HTMLButtonElement>();

	$effect(() => {
		const c = dialogs.current;
		if (!el) return;
		if (c && !el.open) {
			el.showModal();
			// Destructive actions focus Cancel first so Enter doesn't confirm by accident.
			queueMicrotask(() => (c.danger ? cancelBtn : confirmBtn)?.focus());
		} else if (!c && el.open) el.close();
	});
</script>

<dialog
	bind:this={el}
	class="modal modal-bottom sm:modal-middle"
	aria-labelledby="dlg-title"
	aria-describedby="dlg-body"
	oncancel={(e) => {
		e.preventDefault();
		dialogs.close(false);
	}}
>
	{#if dialogs.current}
		{@const c = dialogs.current}
		<div class="modal-box">
			<div class="flex items-start gap-3">
				{#if c.danger}<span class="text-error mt-1"><Icon name="alert" size={22} /></span>{/if}
				<div>
					<h2 id="dlg-title" class="text-lg font-bold mt-0">{c.title}</h2>
					{#if c.body}<p id="dlg-body" class="muted mb-0">{c.body}</p>{/if}
				</div>
			</div>
			<div class="modal-action">
				<button class="btn" bind:this={cancelBtn} onclick={() => dialogs.close(false)}>{c.cancel ?? 'Cancel'}</button>
				<button class="btn {c.danger ? 'btn-error' : 'btn-primary'}" bind:this={confirmBtn} onclick={() => dialogs.close(true)}>{c.confirm ?? 'OK'}</button>
			</div>
		</div>
		<div class="modal-backdrop"><button tabindex="-1" aria-label="Close" onclick={() => dialogs.close(false)}>close</button></div>
	{/if}
</dialog>
