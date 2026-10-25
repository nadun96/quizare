// Recording the presenter in the browser of the teacher who pressed Record
// (D-80, TS-FR-75 to TS-FR-77). Each frame draws the current broadcaster's
// screen onto a canvas, and their voice and screen sound are mixed into one
// track, so the recording follows the broadcast as it changes hands without
// stopping. Parts are uploaded every few seconds, in order; if this tab
// closes, what was uploaded is kept. Cameras are never recorded.
import { ApiError } from '../api';
import type { TutorClient } from './client';

export type Presenter = { screen?: MediaStreamTrack; audio: MediaStreamTrack[]; name: string };

const W = 1280;
const H = 720;
const FPS = 15;
const PART_MS = 5_000;

/** The first container and codecs this browser can record, best first. */
export function pickMime(supported: (t: string) => boolean = (t) => typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported(t)): string | null {
	for (const t of ['video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/webm', 'video/mp4;codecs=avc1,mp4a', 'video/mp4']) if (supported(t)) return t;
	return null;
}

/** Where a w×h picture goes inside the frame, keeping its shape. */
export function fit(w: number, h: number, boxW = W, boxH = H): { x: number; y: number; w: number; h: number } {
	if (!w || !h) return { x: 0, y: 0, w: boxW, h: boxH };
	const s = Math.min(boxW / w, boxH / h);
	return { x: (boxW - w * s) / 2, y: (boxH - h * s) / 2, w: w * s, h: h * s };
}

export class PresenterRecorder {
	state = $state<'idle' | 'starting' | 'recording' | 'paused' | 'stopping'>('idle');
	id = $state('');
	/** Bytes uploaded, for the teacher to see it working. */
	sent = $state(0);
	private rec: MediaRecorder | null = null;
	private canvas: HTMLCanvasElement | null = null;
	private video: HTMLVideoElement | null = null;
	private videoTrack: MediaStreamTrack | null = null;
	private audio: AudioContext | null = null;
	private mix: MediaStreamAudioDestinationNode | null = null;
	private sources = new Map<string, MediaStreamAudioSourceNode>();
	private timer: ReturnType<typeof setInterval> | null = null;
	private queue: Blob[] = [];
	private next = 0;
	private pumping: Promise<void> | null = null;

	constructor(
		private client: TutorClient,
		private sessionId: string,
		private presenter: () => Presenter,
		private onProblem: (message: string) => void
	) {}

	private get path() {
		return `/api/sessions/${this.sessionId}/recordings`;
	}

	async start() {
		const mime = pickMime();
		if (!mime) return this.onProblem('This browser can’t record. Use a recent Chrome, Edge or Firefox.');
		this.state = 'starting';
		try {
			const r = await this.client.post<{ id: string }>(this.path, { mime });
			this.id = r.id;
		} catch (e) {
			this.state = 'idle';
			return this.onProblem(e instanceof ApiError ? e.message : 'Recording didn’t start');
		}
		this.canvas = document.createElement('canvas');
		this.canvas.width = W;
		this.canvas.height = H;
		this.video = Object.assign(document.createElement('video'), { muted: true, playsInline: true, autoplay: true });
		this.audio = new AudioContext();
		this.mix = this.audio.createMediaStreamDestination();
		// A silent source keeps the audio track running while nobody speaks.
		const silence = this.audio.createConstantSource();
		silence.offset.value = 0;
		silence.connect(this.mix);
		silence.start();
		this.draw();
		this.timer = setInterval(() => this.draw(), 1000 / FPS);
		const stream = new MediaStream([...this.canvas.captureStream(FPS).getVideoTracks(), ...this.mix.stream.getAudioTracks()]);
		this.rec = new MediaRecorder(stream, { mimeType: mime, videoBitsPerSecond: 1_500_000, audioBitsPerSecond: 96_000 });
		this.rec.ondataavailable = (e) => {
			if (e.data.size) {
				this.queue.push(e.data);
				this.pump();
			}
		};
		this.rec.start(PART_MS);
		this.state = 'recording';
	}

	/** Draws the presenter's screen, or a card saying it's coming, and keeps the voices mixed. */
	private draw() {
		const ctx = this.canvas?.getContext('2d');
		if (!ctx || !this.video) return;
		const p = this.presenter();
		if (p.screen !== this.videoTrack) {
			this.videoTrack = p.screen ?? null;
			this.video.srcObject = p.screen ? new MediaStream([p.screen]) : null;
			if (p.screen) this.video.play().catch(() => {});
		}
		ctx.fillStyle = '#111';
		ctx.fillRect(0, 0, W, H);
		if (this.videoTrack && this.video.videoWidth) {
			const r = fit(this.video.videoWidth, this.video.videoHeight);
			ctx.drawImage(this.video, r.x, r.y, r.w, r.h);
		} else {
			ctx.fillStyle = '#ddd';
			ctx.font = '32px system-ui, sans-serif';
			ctx.textAlign = 'center';
			ctx.fillText(`${p.name} isn’t sharing a screen`, W / 2, H / 2);
		}
		// Voices: connect new tracks, let go of ended ones.
		if (!this.audio || !this.mix) return;
		const live = new Set(p.audio.map((t) => t.id));
		for (const [id, node] of this.sources) {
			if (!live.has(id)) {
				node.disconnect();
				this.sources.delete(id);
			}
		}
		for (const t of p.audio) {
			if (this.sources.has(t.id)) continue;
			const node = this.audio.createMediaStreamSource(new MediaStream([t]));
			node.connect(this.mix);
			this.sources.set(t.id, node);
		}
	}

	/** Uploads parts one at a time, in order, retrying a failed one a few times. */
	private pump(): Promise<void> {
		this.pumping ??= (async () => {
			while (this.queue.length) {
				const part = this.queue[0];
				for (let attempt = 0; ; attempt++) {
					try {
						await this.client.put(`${this.path}/${this.id}/chunks/${this.next}`, part);
						break;
					} catch (e) {
						if (e instanceof ApiError && (e.code === 'recording_full' || e.code === 'recording_stopped')) {
							this.halt();
							this.onProblem(e.message);
							return;
						}
						if (attempt >= 4) {
							this.halt();
							this.onProblem('Recording stopped: parts couldn’t be uploaded. What was saved is kept.');
							return;
						}
						await new Promise((r) => setTimeout(r, 1000 * 2 ** attempt));
					}
				}
				this.queue.shift();
				this.next++;
				this.sent += part.size;
			}
		})().finally(() => (this.pumping = null));
		return this.pumping;
	}

	async pause(paused: boolean) {
		if (!this.rec || this.state === 'idle') return;
		if (paused) this.rec.pause();
		else this.rec.resume();
		this.state = paused ? 'paused' : 'recording';
		await this.client.post(`${this.path}/${this.id}/pause`, { paused }).catch(() => {});
	}

	/** Stops recording, uploads the last part, and tells the service it's done. */
	async stop() {
		if (!this.rec || this.state === 'idle' || this.state === 'stopping') return;
		this.state = 'stopping';
		const done = new Promise<void>((r) => this.rec!.addEventListener('stop', () => r(), { once: true }));
		this.rec.stop();
		await done;
		await this.pump();
		await this.client.post(`${this.path}/${this.id}/stop`).catch(() => {});
		this.halt();
	}

	/** Lets go of everything; anything uploaded stays. */
	private halt() {
		if (this.timer) clearInterval(this.timer);
		this.timer = null;
		if (this.rec && this.rec.state !== 'inactive') this.rec.stop();
		this.rec = null;
		for (const n of this.sources.values()) n.disconnect();
		this.sources.clear();
		this.audio?.close().catch(() => {});
		this.audio = null;
		this.queue = [];
		this.state = 'idle';
	}
}
