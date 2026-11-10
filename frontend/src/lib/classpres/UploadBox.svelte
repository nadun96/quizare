<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Uploading a group's presentation for a task (D-166).
	import { ApiError } from '$lib/api';
	import { ACCEPT } from '$lib/presentation/presentation';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { uploadGroupPres } from './classpres';

	let { classroomId, taskId, groupId, replacing = false, canConvert = false, disabled = false, onuploaded }: {
		classroomId: string;
		taskId: string;
		groupId: string;
		replacing?: boolean;
		canConvert?: boolean;
		disabled?: boolean;
		onuploaded?: () => void;
	} = $props();
	let title = $state('');
	let file = $state<File | null>(null);
	let busy = $state(false);
	let progress = $state({ done: 0, total: 0, stage: '' });
	let formEl = $state<HTMLFormElement | null>(null);

	async function send(e: SubmitEvent) {
		e.preventDefault();
		if (!file) return;
		busy = true;
		try {
			await uploadGroupPres(classroomId, taskId, groupId, file, title.trim(), (done, total, stage) => (progress = { done, total, stage }));
			toast(tr("Your group’s presentation is in"));
			file = null;
			title = '';
			formEl?.reset();
			onuploaded?.();
		} catch (err) {
			toast(err instanceof ApiError || err instanceof Error ? err.message : tr("The upload didn’t work"), 'error');
		} finally {
			busy = false;
			progress = { done: 0, total: 0, stage: '' };
		}
	}
</script>

<form class="vstack gap-2" onsubmit={send} bind:this={formEl}>
	<div class="flex flex-wrap items-end gap-2">
		<label class="vstack gap-1 small grow">{tr("Title")}<input class="input input-sm w-full" bind:value={title} maxlength="200" placeholder={tr("From the file’s name")} /></label>
		<label class="vstack gap-1 small">{replacing ? tr("Replace with") : tr("File")}
			<input class="file-input file-input-sm" type="file" accept={ACCEPT} onchange={(e) => (file = e.currentTarget.files?.[0] ?? null)} />
		</label>
		<button class="btn btn-primary btn-sm" disabled={!file || busy || disabled}><Icon name="upload" size={15} />{busy ? tr("Uploading…") : tr("Upload")}</button>
	</div>
	<p class="m-0 text-xs muted">{canConvert ? tr("PDF or PowerPoint, up to 100 slides.") : tr("A PDF, up to 100 slides: save PowerPoint as PDF first.")}{#if replacing} {tr("A new upload needs your teacher’s permission again to present.")}{/if}</p>
	{#if busy && progress.stage}
		<div class="small">{tr(progress.stage)}{#if progress.total} {progress.done} / {progress.total}{/if}</div>
		{#if progress.total}<progress class="progress progress-primary w-full" value={progress.done} max={progress.total}></progress>{/if}
	{/if}
</form>
