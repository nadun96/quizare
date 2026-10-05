<script lang="ts">
	// Audio or video recorded in the browser (MediaRecorder), capped at
	// maxSeconds, previewed, then uploaded. Needs HTTPS (or localhost) and the
	// participant's permission; every failure says what to do.
	import { onDestroy } from 'svelte';
	import Icon from '../../ui/Icon.svelte';
	import { fmtSize } from '../meta';
	import type { FileRef } from '../types';

	let { kind, maxSeconds = 30, value, disabled = false, label, upload }: {
		kind: 'audio' | 'video'; maxSeconds?: number; value?: FileRef; disabled?: boolean; label: string; upload: (b: Blob, name: string, onProgress: (p: number) => void) => Promise<void>;
	} = $props();

	type Phase = 'idle' | 'asking' | 'recording' | 'review' | 'uploading';
	let phase = $state<Phase>('idle');
	let error = $state('');
	let elapsed = $state(0);
	let blob = $state<Blob | null>(null);
	let url = $state('');
	let progress = $state(0);
	let live = $state<HTMLVideoElement>();
	let stream: MediaStream | null = null;
	let rec: MediaRecorder | null = null;
	let timer: ReturnType<typeof setInterval> | null = null;

	const supported = typeof window !== 'undefined' && !!navigator.mediaDevices?.getUserMedia && typeof MediaRecorder !== 'undefined';
	function mime(): string {
		const prefs = kind === 'audio' ? ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg'] : ['video/webm;codecs=vp8,opus', 'video/webm', 'video/mp4'];
		return prefs.find((m) => MediaRecorder.isTypeSupported?.(m)) ?? '';
	}
	function stopStream() {
		stream?.getTracks().forEach((t) => t.stop());
		stream = null;
	}
	async function start() {
		error = '';
		phase = 'asking';
		try {
			stream = await navigator.mediaDevices.getUserMedia(kind === 'audio' ? { audio: true } : { audio: true, video: { width: { ideal: 640 }, height: { ideal: 360 }, frameRate: { ideal: 15 } } });
		} catch (e) {
			phase = 'idle';
			error = (e as DOMException)?.name === 'NotAllowedError' ? `Allow ${kind === 'audio' ? 'microphone' : 'camera and microphone'} access in your browser, then try again.` : `No ${kind === 'audio' ? 'microphone' : 'camera'} was found.`;
			return;
		}
		const type = mime();
		rec = new MediaRecorder(stream, { ...(type ? { mimeType: type } : {}), audioBitsPerSecond: 64_000, ...(kind === 'video' ? { videoBitsPerSecond: 1_200_000 } : {}) });
		const chunks: Blob[] = [];
		rec.ondataavailable = (e) => e.data.size && chunks.push(e.data);
		rec.onstop = () => {
			stopStream();
			if (timer) clearInterval(timer);
			blob = new Blob(chunks, { type: rec?.mimeType || type || (kind === 'audio' ? 'audio/webm' : 'video/webm') });
			if (url) URL.revokeObjectURL(url);
			url = URL.createObjectURL(blob);
			phase = 'review';
		};
		elapsed = 0;
		rec.start(500);
		phase = 'recording';
		queueMicrotask(() => {
			if (live && kind === 'video') {
				live.srcObject = stream;
				void live.play().catch(() => {});
			}
		});
		timer = setInterval(() => {
			elapsed += 0.25;
			if (elapsed >= maxSeconds) stop();
		}, 250);
	}
	function stop() {
		if (rec && rec.state !== 'inactive') rec.stop();
	}
	async function send() {
		if (!blob) return;
		phase = 'uploading';
		progress = 0;
		const ext = blob.type.includes('mp4') ? 'mp4' : blob.type.includes('ogg') ? 'ogg' : 'webm';
		try {
			await upload(blob, `${kind}-answer.${ext}`, (p) => (progress = p));
			phase = 'idle';
			blob = null;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Upload failed';
			phase = 'review';
		}
	}
	onDestroy(() => {
		stop();
		stopStream();
		if (timer) clearInterval(timer);
		if (url) URL.revokeObjectURL(url);
	});
	const left = $derived(Math.max(0, Math.ceil(maxSeconds - elapsed)));
</script>

<div class="rec" role="group" aria-label={label}>
	{#if !supported}
		<p class="alert alert-soft alert-warning m-0"><Icon name="alert" />This browser can't record {kind}. Try a recent Chrome, Edge, Firefox or Safari.</p>
	{:else if phase === 'recording'}
		{#if kind === 'video'}<video class="preview" bind:this={live} muted playsinline aria-label="Camera preview"></video>{/if}
		<div class="flex flex-wrap items-center gap-3">
			<span class="rec-dot" aria-hidden="true"></span>
			<span class="font-semibold tabular" aria-live="off">Recording · {left}s left</span>
			<progress class="progress progress-error w-32" value={elapsed} max={maxSeconds} aria-hidden="true"></progress>
			<span class="spacer"></span>
			<button type="button" class="btn btn-error" onclick={stop}><Icon name="pause" size={16} />Stop</button>
		</div>
	{:else if phase === 'review' || phase === 'uploading'}
		<!-- The participant reviews their own recording; there are no captions to offer. -->
		<!-- svelte-ignore a11y_media_has_caption -->
		{#if kind === 'video'}<video class="preview" src={url} controls playsinline></video>{:else}<audio class="w-full" src={url} controls></audio>{/if}
		<div class="mt-2 flex flex-wrap items-center gap-2">
			<span class="small muted">{blob ? fmtSize(blob.size) : ''}</span>
			<span class="spacer"></span>
			<button type="button" class="btn btn-ghost" disabled={phase === 'uploading'} onclick={start}>Record again</button>
			<button type="button" class="btn btn-primary" disabled={phase === 'uploading'} onclick={send}>
				{#if phase === 'uploading'}<span class="loading loading-spinner loading-sm"></span>{/if}Use this recording
			</button>
		</div>
		{#if phase === 'uploading'}<progress class="progress progress-primary mt-2 w-full" value={progress} max="1" aria-label="Uploading"></progress>{/if}
	{:else}
		{#if value}
			<p class="m-0 mb-2 flex items-center gap-2"><span class="text-success"><Icon name="check-circle" /></span><span>Recording sent ({fmtSize(value.size)}).</span></p>
		{/if}
		<button type="button" class="btn {value ? '' : 'btn-primary'}" {disabled} onclick={start}>
			<span class="rec-dot static" aria-hidden="true"></span>{phase === 'asking' ? 'Waiting for permission…' : value ? 'Record a new answer' : `Start recording (up to ${maxSeconds}s)`}
		</button>
	{/if}
	{#if error}<p class="field-error" role="alert">{error}</p>{/if}
</div>

<style>
	.rec { border: 1px solid var(--color-base-300); border-radius: var(--radius-box); padding: 1rem; }
	.preview { width: 100%; max-height: 18rem; border-radius: var(--radius-field); background: #000; margin-bottom: 0.75rem; }
	.rec-dot { width: 0.75rem; height: 0.75rem; border-radius: 999px; background: var(--color-error); animation: blink 1s ease-in-out infinite; }
	.rec-dot.static { animation: none; }
	@keyframes blink { 50% { opacity: 0.35; } }
</style>
