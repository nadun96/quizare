<script lang="ts">
	// Download a classroom report as a locked PDF (D-141), in one click with the
	// classroom's saved header and footer, which a form beside it changes.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { exportPdf, type ReportHeader, type Sheet } from './reportPdf';

	let { classroomId, title, fileName, sheets, disabled = false }: { classroomId: string; title: string; fileName: string; sheets: () => Sheet[]; disabled?: boolean } = $props();
	let dialog = $state<HTMLDialogElement>();
	let h = $state<ReportHeader | null>(null);
	let busy = $state(false);
	let error = $state('');
	const url = $derived(`/api/teacher/classrooms/${classroomId}/report-header`);

	async function download() {
		busy = true;
		try {
			await exportPdf(sheets(), await api.get<ReportHeader>(url), title, fileName);
			toast('PDF downloaded');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The PDF couldn’t be made. Try again.', 'error');
		} finally {
			busy = false;
		}
	}
	async function openForm() {
		error = '';
		try {
			h = await api.get<ReportHeader>(url);
			dialog?.showModal();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The report’s header didn’t load', 'error');
		}
	}
	// Saves, and downloads too unless the teacher only wanted to save.
	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!h) return;
		const andDownload = (e.submitter as HTMLButtonElement | null)?.value === 'download';
		busy = true;
		error = '';
		try {
			const saved = await api.put<ReportHeader>(url, h);
			if (andDownload) await exportPdf(sheets(), saved, title, fileName);
			dialog?.close();
			toast(andDownload ? 'PDF downloaded' : 'Header and footer saved');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'The PDF couldn’t be made. Try again.';
		} finally {
			busy = false;
		}
	}
</script>

<button class="btn btn-sm" disabled={disabled || busy} onclick={download}><Icon name="download" size={15} />{busy ? 'Making the PDF…' : 'Download PDF'}</button>
<button class="btn btn-sm" onclick={openForm}><Icon name="settings" size={15} />Header and footer</button>

<dialog bind:this={dialog} class="modal" aria-labelledby="pdf-title">
	{#if h}
		<form class="modal-box vstack gap-2 max-w-lg" onsubmit={submit}>
			<h2 id="pdf-title" class="m-0 text-lg">Header and footer</h2>
			<p class="m-0 small muted">Saved for this classroom’s reports. In a downloaded PDF each page is a picture and the file is locked against editing, so its text can’t be selected.</p>
			<label class="vstack gap-1 small">Title<input class="input w-full" bind:value={h.title} maxlength="300" placeholder={title} /></label>
			<label class="vstack gap-1 small">Subtitle<input class="input w-full" bind:value={h.subtitle} maxlength="300" placeholder="Term 1, 2026" /></label>
			<label class="vstack gap-1 small">Institution<input class="input w-full" bind:value={h.institution} maxlength="300" /></label>
			<div class="flex flex-wrap gap-2 items-end">
				<label class="vstack gap-1 small">Date
					<select class="select" bind:value={h.date_mode}><option value="today">The day it’s downloaded</option><option value="custom">A date I choose</option><option value="none">No date</option></select>
				</label>
				{#if h.date_mode === 'custom'}<label class="vstack gap-1 small">On<input class="input" type="date" bind:value={h.date_value} required /></label>{/if}
			</div>
			<label class="flex items-center gap-2 small"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={h.show_teacher} />Show the teacher{#if h.teacher}: {h.teacher}{/if}</label>
			<label class="vstack gap-1 small">Footer<input class="input w-full" bind:value={h.footer} maxlength="300" placeholder="© 2026 Your institution. All rights reserved." /></label>
			<label class="flex items-center gap-2 small"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={h.page_numbers} />Page numbers</label>
			{#if error}<p class="m-0 small text-error" role="alert">{error}</p>{/if}
			<div class="flex flex-wrap justify-end gap-2">
				<button type="button" class="btn" onclick={() => dialog?.close()}>Cancel</button>
				<button class="btn" value="save" disabled={busy}>Save</button>
				<button class="btn btn-primary" value="download" disabled={busy || disabled}>{busy ? 'Making the PDF…' : 'Save and download'}</button>
			</div>
		</form>
	{/if}
</dialog>
