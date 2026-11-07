<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A classroom report for paper: Print, Download PDF (locked, D-141), both
	// with the classroom's saved header and footer, logo and colour (D-143),
	// which the form beside them changes.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { numberSheet } from './reportView';
	import { ACCENTS, addRange, bandsOf, exportPdf, logoDataUrl, MAX_RANGES, printReportHTML, rangeLabel, rangesProblem, type Bands, type ReportHeader, type Sheet } from './reportPdf';

	let { classroomId, title, fileName, sheets, disabled = false, onbands }: { classroomId: string; title: string; fileName: string; sheets: () => Sheet[]; disabled?: boolean; onbands?: (b: Bands) => void } = $props();
	let dialog = $state<HTMLDialogElement>();
	let h = $state<ReportHeader | null>(null);
	let busy = $state(false);
	let error = $state('');
	const url = $derived(`/api/teacher/classrooms/${classroomId}/report-header`);
	const bandsProblem = $derived(h ? rangesProblem(h.color_ranges) : '');
	const canAdd = $derived(!!h && addRange(h.color_ranges) !== h.color_ranges);

	// The saved header, and tells the page where its colours change: the sheets depend on it.
	async function saved() {
		const x = await api.get<ReportHeader>(url);
		onbands?.(bandsOf(x));
		return x;
	}
	$effect(() => {
		void url;
		saved().catch(() => {}); // the page keeps the usual bands
	});

	// The window must open in the click itself, or the browser blocks it.
	async function print() {
		const w = window.open('', '_blank');
		if (!w) return toast(tr("Allow pop-ups for this site to print"), 'error');
		try {
			const header = await saved();
			w.document.write(printReportHTML(sheets().map(numberSheet), header, title));
			w.document.close();
			// Called from here: the site's content policy stops a script inside the page.
			const img = w.document.querySelector('img');
			if (img && !img.complete) await new Promise((done) => ((img.onload = img.onerror = done)));
			w.focus();
			w.print();
		} catch (e) {
			w.close();
			toast(e instanceof ApiError ? e.message : tr("The report couldn’t be printed. Try again."), 'error');
		}
	}
	async function download() {
		busy = true;
		try {
			const header = await saved();
			await exportPdf(sheets().map(numberSheet), header, title, fileName);
			toast(tr("PDF downloaded"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("The PDF couldn’t be made. Try again."), 'error');
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
			toast(e instanceof ApiError ? e.message : tr("The report’s header didn’t load"), 'error');
		}
	}
	async function chooseLogo(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file || !h) return;
		error = '';
		try {
			h.logo = await logoDataUrl(file);
		} catch {
			error = tr("That file isn’t a picture that can be read. Try a PNG or JPEG.");
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
			const header = await api.put<ReportHeader>(url, h);
			onbands?.(bandsOf(header));
			if (andDownload) await exportPdf(sheets().map(numberSheet), header, title, fileName);
			dialog?.close();
			toast(andDownload ? tr("PDF downloaded") : tr("Header and footer saved"));
		} catch (err) {
			error = err instanceof ApiError ? err.message : tr("The PDF couldn’t be made. Try again.");
		} finally {
			busy = false;
		}
	}
</script>

<button class="btn btn-sm" {disabled} onclick={print}><Icon name="table" size={15} />{tr("Print")}</button>
<button class="btn btn-sm" disabled={disabled || busy} onclick={download}><Icon name="download" size={15} />{busy ? tr("Making the PDF…") : tr("Download PDF")}</button>
<button class="btn btn-sm" onclick={openForm}><Icon name="settings" size={15} />{tr("Header and footer")}</button>

<dialog bind:this={dialog} class="modal" aria-labelledby="pdf-title">
	{#if h}
		<form class="modal-box vstack gap-2 max-w-lg" onsubmit={submit}>
			<h2 id="pdf-title" class="m-0 text-lg">{tr("Header and footer")}</h2>
			<p class="m-0 small muted">{tr("Saved for this classroom’s reports, printed or downloaded. In a downloaded PDF each page is a picture and the file is locked against editing, so its text can’t be selected.")}</p>
			<div class="flex flex-wrap items-center gap-2">
				{#if h.logo}<img class="logo" src={h.logo} alt={tr("The report’s logo")} />{/if}
				<label class="btn btn-sm">{h.logo ? tr("Change logo") : tr("Add a logo")}<input class="sr-only" type="file" accept="image/*" onchange={chooseLogo} /></label>
				{#if h.logo}<button type="button" class="btn btn-ghost btn-sm" onclick={() => h && (h.logo = '')}>{tr("Remove")}</button>{/if}
			</div>
			<label class="vstack gap-1 small">{tr("Title")}<input class="input w-full" bind:value={h.title} maxlength="300" placeholder={title} /></label>
			<label class="vstack gap-1 small">{tr("Subtitle")}<input class="input w-full" bind:value={h.subtitle} maxlength="300" placeholder={tr("Term 1, 2026")} /></label>
			<label class="vstack gap-1 small">{tr("Institution")}<input class="input w-full" bind:value={h.institution} maxlength="300" /></label>
			<fieldset class="vstack gap-1 small">
				<legend class="mb-1">{tr("Report colour")}</legend>
				<div class="flex flex-wrap items-center gap-2">
					{#each ACCENTS as a (a.v)}
						<label class="swatch" style:background={a.v} class:chosen={h.accent === a.v} title={tr(a.label)}><input class="sr-only" type="radio" name="accent" value={a.v} bind:group={h.accent} aria-label={tr(a.label)} /></label>
					{/each}
					<label class="flex items-center gap-1">{tr("Other")} <input class="picker" type="color" bind:value={h.accent} aria-label={tr("Another colour")} /></label>
				</div>
			</fieldset>
			<label class="flex items-center gap-2 small"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={h.color_marks} />{tr("Colour marks by how high they are in printed and downloaded reports")}</label>
			<fieldset class="vstack gap-2 small">
				<legend class="mb-1">{tr("Mark colours")}</legend>
				{#each h.color_ranges as r, i (i)}
					{@const last = i === h.color_ranges.length - 1}
					<div class="flex flex-wrap items-center gap-2">
						<input class="picker" type="color" bind:value={r.color} aria-label={tr("Colour of range {0}", [i + 1])} />
						<label class="flex items-center gap-1">{tr("From")} <input class="input input-sm w-20 tabular" type="number" min="0" max="100" step="1" bind:value={r.from} readonly={last} required aria-label={tr("Where range {0} starts, in percent", [i + 1])} />%</label>
						<span class="muted grow">{bandsProblem ? '' : rangeLabel(h.color_ranges, i)}</span>
						{#if !last}<button type="button" class="btn btn-ghost btn-xs" aria-label={tr("Remove range {0}", [i + 1])} onclick={() => h && h.color_ranges.splice(i, 1)}><Icon name="x" size={14} /></button>{/if}
					</div>
				{/each}
				<div><button type="button" class="btn btn-xs" disabled={!canAdd} title={canAdd ? '' : tr("At most {0} ranges", [MAX_RANGES])} onclick={() => h && (h.color_ranges = addRange(h.color_ranges))}>{tr("Add a range")}</button></div>
			</fieldset>
			{#if bandsProblem}<p class="m-0 small text-error" role="alert">{bandsProblem}</p>{/if}
			<div class="flex flex-wrap gap-2 items-end">
				<label class="vstack gap-1 small">{tr("Date")}
					<select class="select" bind:value={h.date_mode}><option value="today">{tr("The day it’s made")}</option><option value="custom">{tr("A date I choose")}</option><option value="none">{tr("No date")}</option></select>
				</label>
				{#if h.date_mode === 'custom'}<label class="vstack gap-1 small">{tr("On")}<input class="input" type="date" bind:value={h.date_value} required /></label>{/if}
			</div>
			<label class="flex items-center gap-2 small"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={h.show_teacher} />{tr("Show the teacher")}{#if h.teacher}: {h.teacher}{/if}</label>
			<label class="vstack gap-1 small">{tr("Footer")}<input class="input w-full" bind:value={h.footer} maxlength="300" placeholder={tr("© 2026 Your institution. All rights reserved.")} /></label>
			<label class="flex items-center gap-2 small"><input class="checkbox checkbox-sm" type="checkbox" bind:checked={h.page_numbers} />{tr("Page numbers")}</label>
			{#if error}<p class="m-0 small text-error" role="alert">{error}</p>{/if}
			<div class="flex flex-wrap justify-end gap-2">
				<button type="button" class="btn" onclick={() => dialog?.close()}>{tr("Cancel")}</button>
				<button class="btn" value="save" disabled={busy || !!bandsProblem}>{tr("Save")}</button>
				<button class="btn btn-primary" value="download" disabled={busy || disabled || !!bandsProblem}>{busy ? tr("Making the PDF…") : tr("Save and download")}</button>
			</div>
		</form>
	{/if}
</dialog>

<style>
	.logo { max-height: 3rem; max-width: 10rem; }
	.swatch { display: block; width: 1.75rem; height: 1.75rem; cursor: pointer; border: 2px solid transparent; outline: 1px solid var(--color-base-300); }
	.swatch.chosen { border-color: var(--color-base-100); outline: 2px solid var(--color-base-content); }
	.swatch:focus-within { outline: 2px solid var(--color-primary); outline-offset: 1px; }
	.picker { width: 2rem; height: 1.75rem; padding: 0; cursor: pointer; }
</style>
