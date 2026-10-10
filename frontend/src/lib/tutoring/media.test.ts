import { describe, expect, it, vi } from 'vitest';

// A stand-in for livekit-client: rooms that connect at once and emit what the test says.
const rooms: FakeRoom[] = [];
class FakeRoom {
	handlers = new Map<string, ((...a: unknown[]) => void)[]>();
	remoteParticipants = new Map();
	canPlaybackAudio = true;
	localParticipant = {
		permissions: { canPublish: true, canPublishSources: ['camera'] },
		trackPublications: new Map<string, { track: unknown; trackSid: string }>(),
		async publishTrack(track: unknown) {
			const pub = { track, trackSid: 'TR_' + this.trackPublications.size };
			this.trackPublications.set(pub.trackSid, pub);
			return pub;
		},
		async unpublishTrack(track: unknown) {
			for (const [k, p] of this.trackPublications) if (p.track === track) this.trackPublications.delete(k);
		}
	};
	constructor() {
		rooms.push(this);
	}
	on(e: string, f: (...a: unknown[]) => void) {
		this.handlers.set(e, [...(this.handlers.get(e) ?? []), f]);
		return this;
	}
	emit(e: string) {
		for (const f of this.handlers.get(e) ?? []) f();
	}
	async connect() {}
	async disconnect() {
		this.emit('disconnected');
	}
}
vi.mock('livekit-client', () => ({
	Room: FakeRoom,
	VideoPresets: { h720: { resolution: {} } },
	createLocalVideoTrack: async () => ({ kind: 'video', mediaStreamTrack: new EventTarget() }),
	RoomEvent: new Proxy({}, { get: (_t, k) => String(k).replace(/^./, (c) => c.toLowerCase()) })
}));

const { SessionMedia } = await import('./media.svelte');
const flush = () => new Promise((r) => setTimeout(r));
const tokens = { url: 'ws://lk', main: 'm', stage: 's' };

describe('SessionMedia (D-60)', () => {
	it('ignores events from a connection it has left', async () => {
		rooms.length = 0;
		const m = new SessionMedia(() => undefined);
		await m.connect(tokens, 'main');
		const [oldMain, oldStage] = rooms;
		await m.connect(tokens, 'main'); // a rejoin closes the old rooms
		expect(m.state).toBe('connected');
		expect(m.hasStage).toBe(true);
		// LiveKit closing the older connection late (DUPLICATE_IDENTITY) changes nothing.
		oldMain.emit('disconnected');
		oldMain.emit('reconnecting');
		oldStage.emit('disconnected');
		expect(m.state).toBe('connected');
		expect(m.hasStage).toBe(true);
		// The current room still counts.
		rooms[2].emit('reconnecting');
		expect(m.state).toBe('reconnecting');
		rooms[2].emit('disconnected');
		expect(m.state).toBe('disconnected');
	});

	it('takes out a camera that stopped on its own, and says so', async () => {
		rooms.length = 0;
		const said: string[] = [];
		const m = new SessionMedia(() => undefined, (msg) => said.push(msg));
		await m.connect(tokens, 'main');
		await m.addCamera(undefined, 'Document camera');
		const lp = rooms[0].localParticipant;
		const [pub] = lp.trackPublications.values();
		expect(lp.trackPublications.size).toBe(1);
		(pub.track as { mediaStreamTrack: EventTarget }).mediaStreamTrack.dispatchEvent(new Event('ended'));
		await flush();
		expect(lp.trackPublications.size).toBe(0);
		expect(said).toEqual(['Document camera stopped. Add it again from "Add a camera".']);
	});
});
