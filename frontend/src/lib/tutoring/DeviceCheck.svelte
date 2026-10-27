<script lang="ts">
	// The device check (TS-FR-05): choose speakers and hear a test sound;
	// people who may share also choose a microphone and camera and see a
	// preview. Nothing turns on by itself: the preview starts only on the
	// person's click and the browser's permission prompt (PO-3).
	import { onDestroy } from 'svelte';
	import { deviceName, level, loadChoice, pick, saveChoice, type DeviceChoice } from './devices';

	let { open = $bindable(false), canPublish, onsave }: { open?: boolean; canPublish: boolean; onsave?: (c: DeviceChoice) => void } = $props();

	let dialog = $state<HTMLDialogElement>();
	let devices = $state<MediaDeviceInfo[]>([]);
	let choice = $state<DeviceChoice>(loadChoice());
	let previewing = $state(false);
	let problem = $state('');
	let mic = $state(0);
	let video = $state<HTMLVideoElement>();
	let stream: MediaStream | null = null;
	let audio: AudioContext | null = null;
	let frame = 0;
	const canChooseSpeaker = typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype;

	const of = (kind: MediaDeviceKind) => devices.filter((d) => d.kind === kind);
	const speakers = $derived(of('audiooutput'));
	const mics = $derived(of('audioinput'));
	const cameras = $derived(of('videoinput'));

	async function list() {
		try {
			devices = (await navigator.mediaDevices?.enumerateDevices()) ?? [];
		} catch {
			devices = [];
		}
		choice = { speaker: pick(speakers, choice.speaker), mic: pick(mics, choice.mic), camera: pick(cameras, choice.camera) };
	}

	$effect(() => {
		if (!dialog) return;
		if (open && !dialog.open) {
			dialog.showModal();
			list();
		} else if (!open && dialog.open) dialog.close();
	});

	/** A short two-note tone through the chosen speakers. */
	async function testSound() {
		const ctx = new AudioContext();
		const dest = ctx.createMediaStreamDestination();
		const el = new Audio();
		el.srcObject = dest.stream;
		if (canChooseSpeaker && choice.speaker) await (el as HTMLAudioElement & { setSinkId(id: string): Promise<void> }).setSinkId(choice.speaker).catch(() => {});
		await el.play().catch(() => {});
		for (const [i, f] of [660, 880].entries()) {
			const o = ctx.createOscillator();
			const g = ctx.createGain();
			o.frequency.value = f;
			g.gain.setValueAtTime(0.0001, ctx.currentTime + i * 0.35);
			g.gain.exponentialRampToValueAtTime(0.3, ctx.currentTime + i * 0.35 + 0.03);
			g.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + i * 0.35 + 0.3);
			o.connect(g).connect(dest);
			o.start(ctx.currentTime + i * 0.35);
			o.stop(ctx.currentTime + i * 0.35 + 0.32);
		}
		setTimeout(() => {
			el.pause();
			ctx.close();
		}, 1000);
	}

	async function preview() {
		stopPreview();
		problem = '';
		try {
			stream = await navigator.mediaDevices.getUserMedia({
				audio: choice.mic ? { deviceId: { exact: choice.mic } } : true,
				video: choice.camera ? { deviceId: { exact: choice.camera } } : true
			});
		} catch (e) {
			problem = e instanceof DOMException && e.name === 'NotAllowedError' ? 'The browser wasn’t allowed to use them. Allow the camera and microphone for this site, then try again.' : 'They couldn’t start. Another app may be using them.';
			return;
		}
		previewing = true;
		await list(); // names appear once allowed
		if (video) video.srcObject = stream;
		audio = new AudioContext();
		const an = audio.createAnalyser();
		an.fftSize = 512;
		audio.createMediaStreamSource(stream).connect(an);
		const buf = new Uint8Array(an.fftSize);
		const tick = () => {
			an.getByteTimeDomainData(buf);
			mic = level(buf);
			frame = requestAnimationFrame(tick);
		};
		tick();
	}

	function stopPreview() {
		cancelAnimationFrame(frame);
		stream?.getTracks().forEach((t) => t.stop());
		stream = null;
		audio?.close();
		audio = null;
		previewing = false;
		mic = 0;
	}

	function close(save: boolean) {
		stopPreview();
		if (save) {
			saveChoice(choice);
			onsave?.(choice);
		}
		open = false;
	}
	onDestroy(stopPreview);
</script>

<dialog bind:this={dialog} class="modal" onclose={() => open && close(false)} aria-labelledby="devcheck-h">
	<div class="modal-box vstack max-w-lg">
		<h2 id="devcheck-h" class="m-0 text-lg">Check your devices</h2>

		<section class="vstack" aria-label="Speakers">
			{#if canChooseSpeaker && speakers.length}
				<label for="dc-speaker" class="font-semibold">Speakers</label>
				<select id="dc-speaker" class="select select-sm w-full" bind:value={choice.speaker}>
					{#each speakers as d, i (d.deviceId)}<option value={d.deviceId}>{deviceName(d, i)}</option>{/each}
				</select>
			{:else}
				<p class="m-0 small muted">Sound plays through your system's speakers or headphones; this browser can't choose them here.</p>
			{/if}
			<div><button class="btn btn-sm" onclick={testSound}>Play a test sound</button></div>
		</section>

		{#if canPublish}
			<section class="vstack" aria-label="Microphone and camera">
				<div class="flex flex-wrap gap-3">
					<div class="grow">
						<label for="dc-mic" class="font-semibold">Microphone</label>
						<select id="dc-mic" class="select select-sm w-full" bind:value={choice.mic} onchange={() => previewing && preview()}>
							{#each mics as d, i (d.deviceId)}<option value={d.deviceId}>{deviceName(d, i)}</option>{:else}<option value="">Default</option>{/each}
						</select>
					</div>
					<div class="grow">
						<label for="dc-cam" class="font-semibold">Camera</label>
						<select id="dc-cam" class="select select-sm w-full" bind:value={choice.camera} onchange={() => previewing && preview()}>
							{#each cameras as d, i (d.deviceId)}<option value={d.deviceId}>{deviceName(d, i)}</option>{:else}<option value="">Default</option>{/each}
						</select>
					</div>
				</div>
				{#if previewing}
					<!-- svelte-ignore a11y_media_has_caption -->
					<video bind:this={video} autoplay playsinline muted class="w-full rounded-box bg-black aspect-video object-cover"></video>
					<div class="flex items-center gap-2">
						<span class="small">Microphone</span>
						<progress class="progress progress-success w-full" value={Math.round(mic * 100)} max="100" aria-label="Microphone level"></progress>
					</div>
					<div><button class="btn btn-sm" onclick={stopPreview}>Stop the preview</button></div>
				{:else}
					<p class="m-0 small muted">They stay off until you ask: the browser asks for permission first.</p>
					<div><button class="btn btn-sm" onclick={preview}>Try my microphone and camera</button></div>
				{/if}
				{#if problem}<p class="m-0 small text-error" role="alert">{problem}</p>{/if}
			</section>
		{/if}

		<div class="modal-action">
			<button class="btn btn-ghost" onclick={() => close(false)}>Cancel</button>
			<button class="btn btn-primary" onclick={() => close(true)}>Use these</button>
		</div>
	</div>
</dialog>
