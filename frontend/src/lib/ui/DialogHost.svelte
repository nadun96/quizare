<script lang="ts">
	// The single confirmation and prompt dialog (lib/ui/dialog.svelte.ts). A
	// native modal <dialog>: focus moves into it (the field, for prompts),
	// Enter saves, Escape cancels, and focus returns afterwards.
	import { dialogs } from './dialog.svelte';
	import Icon from './Icon.svelte';

	let el = $state<HTMLDialogElement>();
	let confirmBtn = $state<HTMLButtonElement>();
	let cancelBtn = $state<HTMLButtonElement>();
	let field = $state<HTMLInputElement | HTMLTextAreaElement>();

	$effect(() => {
		const c = dialogs.current;
		if (!el) return;
		if (c && !el.open) {
			el.showModal();
			// Destructive actions focus Cancel first so Enter doesn't confirm by accident.
			queueMicrotask(() => {
				if (c.prompt) {
					field?.focus();
					field?.select();
				} else (c.danger ? cancelBtn : confirmBtn)?.focus();
			});
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
			{#if c.prompt}
				<form class="mt-3" onsubmit={(e) => { e.preventDefault(); dialogs.close(true); }}>
					<label class="sr-only" for="dlg-field">{c.label ?? c.title}</label>
					{#if c.multiline}
						<textarea id="dlg-field" class="textarea w-full" rows="4" bind:this={field} bind:value={dialogs.value} maxlength={c.maxlength} placeholder={c.placeholder}></textarea>
					{:else}
						<input id="dlg-field" class="input w-full" bind:this={field} bind:value={dialogs.value} maxlength={c.maxlength} placeholder={c.placeholder} inputmode={c.inputmode} autocomplete="off" />
					{/if}
				</form>
			{/if}
			<div class="modal-action">
				<button class="btn" bind:this={cancelBtn} onclick={() => dialogs.close(false)}>{c.cancel ?? 'Cancel'}</button>
				<button class="btn {c.danger ? 'btn-error' : 'btn-primary'}" bind:this={confirmBtn} disabled={c.prompt && !c.allowEmpty && !dialogs.value.trim()} onclick={() => dialogs.close(true)}>{c.confirm ?? 'OK'}</button>
			</div>
		</div>
		<div class="modal-backdrop"><button tabindex="-1" aria-label="Close" onclick={() => dialogs.close(false)}>close</button></div>
	{/if}
</dialog>
