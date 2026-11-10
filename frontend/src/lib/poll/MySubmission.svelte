<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A participant's presentation in a poll (D-165): what's asked, uploading
	// it (or their group's), and how it went.
	import { ApiError } from '$lib/api';
	import { ACCEPT } from '$lib/presentation/presentation';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { presClient, ratingText, type MyPresentations } from './present';

	let { code, refreshKey = 0, closed = false }: { code: string; refreshKey?: number; closed?: boolean } = $props();
	let v = $state<MyPresentations | null>(null);
	let owner = $state('');
	let title = $state('');
	let file = $state<File | null>(null);
	let busy = $state(false);
	let progress = $state({ done: 0, total: 0, stage: '' });

	async function load() {
		try {
			v = await presClient.mine(code);
			if (!owner && v.owners.length) owner = v.owners[0].id;
		} catch {
			v = null;
		}
	}
	$effect(() => {
		void refreshKey;
		if (code) load();
	});

	const current = $derived(v && owner ? v.submissions[owner] : undefined);
	const canUpload = $derived(!!v && !!owner && !!v.can_upload[owner] && !closed);
	const isGroup = $derived(v?.mode === 'poll_group' || v?.mode === 'class_group');

	async function send(e: SubmitEvent) {
		e.preventDefault();
		if (!file || !v) return;
		busy = true;
		try {
			await presClient.upload(code, file, title.trim(), owner, (done, total, stage) => (progress = { done, total, stage }));
			toast(tr("Your presentation is in"));
			file = null;
			title = '';
			await load();
		} catch (err) {
			toast(err instanceof ApiError || err instanceof Error ? err.message : tr("The upload didn’t work"), 'error');
		} finally {
			busy = false;
			progress = { done: 0, total: 0, stage: '' };
		}
	}
</script>

{#if v && v.mode !== 'off'}
	<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-5 vstack gap-2" aria-labelledby="pres-h">
		<h2 id="pres-h" class="m-0 flex items-center gap-2 text-base"><Icon name="monitor" size={16} />{isGroup ? tr("Your group’s presentation") : tr("Your presentation")}</h2>
		{#if v.instructions}<p class="m-0 small whitespace-pre-line">{v.instructions}</p>{/if}
		{#if !v.owners.length}
			<p class="m-0 small muted">{v.mode === 'poll_group' ? tr("You’re not in a group in this poll yet.") : tr("You’re not in a classroom group that presents.")}</p>
		{:else}
			{#if v.owners.length > 1}
				<label class="vstack gap-1 small">{tr("For")}
					<select class="select select-sm w-auto" bind:value={owner}>{#each v.owners as o (o.id)}<option value={o.id}>{o.name}</option>{/each}</select>
				</label>
			{:else if isGroup}
				<p class="m-0 small muted">{tr("For {0}", [v.owners[0].name])}</p>
			{/if}
			{#if current}
				<div class="rounded-box border border-base-300 p-3 small vstack gap-1">
					<p class="m-0"><strong>{current.title}</strong> · {current.slides} {current.slides === 1 ? tr("slide") : tr("slides")}{#if !current.ready} · <span class="text-warning">{tr("not finished uploading")}</span>{/if}</p>
					<p class="m-0 muted">{tr("Uploaded by {0}", [current.uploaded_by || '—'])}{#if current.presented_at} · {tr("presented")}{/if}{#if ratingText(current)} · {ratingText(current)}{/if}</p>
					{#if current.mark != null}<p class="m-0">{tr("Mark: {0} / {1}", [current.mark, v.out_of])}{#if current.comment} · {current.comment}{/if}</p>{/if}
				</div>
			{/if}
			{#if canUpload}
				<form class="vstack gap-2" onsubmit={send}>
					<div class="flex flex-wrap items-end gap-2">
						<label class="vstack gap-1 small grow">{tr("Title")}<input class="input input-sm w-full" bind:value={title} maxlength="200" placeholder={tr("From the file’s name")} /></label>
						<label class="vstack gap-1 small">{current ? tr("Replace with") : tr("File")}
							<input class="file-input file-input-sm" type="file" accept={ACCEPT} onchange={(e) => (file = e.currentTarget.files?.[0] ?? null)} />
						</label>
						<button class="btn btn-primary btn-sm" disabled={!file || busy}><Icon name="upload" size={15} />{busy ? tr("Uploading…") : tr("Upload")}</button>
					</div>
					<p class="m-0 text-xs muted">{v.can_convert ? tr("PDF or PowerPoint, up to 100 slides.") : tr("A PDF, up to 100 slides: save PowerPoint as PDF first.")}</p>
					{#if busy && progress.stage}
						<div class="small">{tr(progress.stage)}{#if progress.total} {progress.done} / {progress.total}{/if}</div>
						{#if progress.total}<progress class="progress progress-primary w-full" value={progress.done} max={progress.total}></progress>{/if}
					{/if}
				</form>
			{:else if !closed && v.upload_by === 'leader' && !current}
				<p class="m-0 small muted">{tr("Your group’s leader uploads it.")}</p>
			{/if}
			<p class="m-0 text-xs muted">{tr("Your teacher chooses who presents and when; you’ll get the controls on this screen.")}</p>
		{/if}
	</section>
{/if}
