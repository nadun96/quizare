<script lang="ts">
	// File answer: drop or pick, checked for type and size before upload, with
	// progress. The server checks again by sniffing the bytes.
	import Icon from '../../ui/Icon.svelte';
	import { fmtSize } from '../meta';
	import type { FileRef } from '../types';

	let { accept = ['any'], maxMB = 5, value, disabled = false, label, upload }: {
		accept?: string[]; maxMB?: number; value?: FileRef; disabled?: boolean; label: string; upload: (f: File, onProgress: (p: number) => void) => Promise<void>;
	} = $props();

	const EXT: Record<string, string> = { image: '.png,.jpg,.jpeg,.gif,.webp', pdf: '.pdf', document: '.pdf,.txt,.docx,.xlsx,.pptx,.odt,.ods,.odp' };
	const acceptAttr = $derived(accept.includes('any') ? [EXT.image, EXT.document].join(',') : accept.map((a) => EXT[a]).join(','));
	const kinds = $derived(accept.includes('any') ? 'images, PDFs or documents' : accept.map((a) => (a === 'image' ? 'images' : a === 'pdf' ? 'PDFs' : 'documents')).join(', '));
	let progress = $state<number | null>(null);
	let error = $state('');
	let over = $state(false);
	let picker = $state<HTMLInputElement>();

	async function take(f: File | undefined) {
		error = '';
		if (!f) return;
		if (f.size > maxMB * 1024 * 1024) {
			error = `That file is ${fmtSize(f.size)}; the limit is ${maxMB} MB.`;
			return;
		}
		const ext = '.' + (f.name.split('.').pop() ?? '').toLowerCase();
		if (!acceptAttr.split(',').includes(ext)) {
			error = `Please choose ${kinds}.`;
			return;
		}
		progress = 0;
		try {
			await upload(f, (p) => (progress = p));
		} catch (e) {
			error = e instanceof Error ? e.message : 'Upload failed';
		} finally {
			progress = null;
		}
	}
</script>

<div
	class="drop"
	class:over
	class:disabled
	role="group"
	aria-label={label}
	ondragover={(e) => {
		e.preventDefault();
		over = !disabled;
	}}
	ondragleave={() => (over = false)}
	ondrop={(e) => {
		e.preventDefault();
		over = false;
		if (!disabled) take(e.dataTransfer?.files?.[0]);
	}}
>
	{#if value}
		<div class="flex items-center gap-3">
			<span class="grid size-10 place-items-center rounded-lg bg-success/15 text-success" aria-hidden="true"><Icon name="check" /></span>
			<div class="min-w-0 flex-1 text-left">
				<p class="m-0 truncate font-semibold">{value.name}</p>
				<p class="small muted m-0">{fmtSize(value.size)} · uploaded</p>
			</div>
			{#if !disabled}<button type="button" class="btn btn-sm" onclick={() => picker?.click()}>Replace</button>{/if}
		</div>
	{:else}
		<span class="mx-auto mb-2 grid size-12 place-items-center rounded-full bg-primary/10 text-primary" aria-hidden="true"><Icon name="plus" size={22} /></span>
		<p class="m-0 font-semibold">Drop a file here, or</p>
		<button type="button" class="btn btn-primary btn-sm mt-2" {disabled} onclick={() => picker?.click()}>Choose a file</button>
		<p class="small muted m-0 mt-2">{kinds[0].toUpperCase() + kinds.slice(1)}, up to {maxMB} MB</p>
	{/if}
	<input bind:this={picker} type="file" class="sr-only" accept={acceptAttr} tabindex="-1" aria-hidden="true" onchange={(e) => take(e.currentTarget.files?.[0])} />
	{#if progress !== null}<progress class="progress progress-primary mt-3 w-full" value={progress} max="1" aria-label="Uploading"></progress>{/if}
	{#if error}<p class="field-error" role="alert">{error}</p>{/if}
</div>

<style>
	.drop { border: 2px dashed var(--color-field); border-radius: var(--radius-box); padding: 1.25rem; text-align: center; transition: background-color var(--motion-fast), border-color var(--motion-fast); }
	.drop.over { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 6%, transparent); }
	.drop.disabled { opacity: 0.7; }
</style>
