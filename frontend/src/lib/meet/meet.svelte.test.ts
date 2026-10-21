import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { LiveSocket } from '../socket';
import { MeetingCall } from './call.svelte';
import { applyRoster, gridColumns, initials, polite, streamKind, type Person } from './mesh';

const person = (peer_id: string, more: Partial<Person> = {}): Person => ({ peer_id, user_id: 'u-' + peer_id, name: peer_id, host: false, mic: false, cam: false, screen: false, hand: false, ...more });

describe('meeting roster', () => {
	it('follows welcome, joined, state and left', () => {
		let r = applyRoster([], { type: 'welcome', peers: [person('a')] });
		r = applyRoster(r, { type: 'joined', peer: person('b') });
		r = applyRoster(r, { type: 'state', peer_id: 'b', mic: true, hand: true });
		expect(r.map((p) => p.peer_id)).toEqual(['a', 'b']);
		expect(r[1]).toMatchObject({ mic: true, hand: true, cam: false });
		// The same person from a new tab replaces their old entry.
		r = applyRoster(r, { type: 'joined', peer: { ...person('c'), user_id: 'u-a' } });
		expect(r.map((p) => p.peer_id)).toEqual(['b', 'c']);
		r = applyRoster(r, { type: 'left', peer_id: 'b' });
		expect(r.map((p) => p.peer_id)).toEqual(['c']);
		// A reconnect starts over.
		expect(applyRoster(r, { type: 'welcome', peers: [] })).toEqual([]);
	});

	it('agrees on who gives way, from both ends', () => {
		expect(polite('b', 'a')).toBe(true);
		expect(polite('a', 'b')).toBe(false);
	});

	it('lays out tiles and names them', () => {
		expect([1, 2, 4, 5, 9].map(gridColumns)).toEqual([1, 2, 2, 3, 3]);
		expect(initials('Ann Marie Perera')).toBe('AP');
		expect(initials('  kumar ')).toBe('K');
		expect(initials('')).toBe('?');
		expect(streamKind({ cam: 'x', screen: 'y' }, 'y')).toBe('screen');
		expect(streamKind({ cam: 'x', screen: 'y' }, 'x')).toBe('cam');
		expect(streamKind(undefined, 'z')).toBe('cam');
	});
});

// Stand-ins for the browser's WebRTC objects: enough to follow the messages.
class FakeStream {
	static n = 0;
	id = 'stream-' + FakeStream.n++;
	tracks: any[] = [];
	constructor(t: any[] = []) {
		this.tracks = [...t];
	}
	getTracks = () => this.tracks;
	getAudioTracks = () => this.tracks.filter((t) => t.kind === 'audio');
	getVideoTracks = () => this.tracks.filter((t) => t.kind === 'video');
	addTrack = (t: any) => this.tracks.push(t);
	removeTrack = (t: any) => (this.tracks = this.tracks.filter((x) => x !== t));
}

class FakePC {
	signalingState = 'stable';
	connectionState = 'new';
	iceConnectionState = 'new';
	localDescription: any = null;
	remote: any = null;
	closed = false;
	senders: any[] = [];
	onnegotiationneeded: any;
	onicecandidate: any;
	ontrack: any;
	onconnectionstatechange: any;
	oniceconnectionstatechange: any;
	constructor(public config: RTCConfiguration) {}
	addTrack(track: any) {
		const s = { track };
		this.senders.push(s);
		return s;
	}
	removeTrack(s: any) {
		this.senders = this.senders.filter((x) => x !== s);
	}
	getSenders = () => this.senders;
	async setLocalDescription() {
		const type = this.signalingState === 'have-remote-offer' ? 'answer' : 'offer';
		this.localDescription = { type, sdp: 'local-' + type };
		this.signalingState = type === 'offer' ? 'have-local-offer' : 'stable';
	}
	async setRemoteDescription(d: any) {
		this.remote = d;
		this.signalingState = d.type === 'offer' ? 'have-remote-offer' : 'stable';
	}
	async addIceCandidate() {}
	restartIce = vi.fn();
	close() {
		this.closed = true;
	}
}

function setup() {
	const sent: any[] = [];
	const pcs: FakePC[] = [];
	const sock: any = { send: (m: any) => sent.push(m), open: vi.fn(), close: vi.fn(), onMessage: () => {}, onStatus: () => {}, shouldReconnect: () => true };
	const call = new MeetingCall(
		'ABC',
		() => {},
		() => sock as LiveSocket,
		(c) => {
			const pc = new FakePC(c);
			pcs.push(pc);
			return pc as unknown as RTCPeerConnection;
		}
	);
	const flush = () => new Promise((r) => setTimeout(r, 0));
	return { call, sock, sent, pcs, flush, recv: (m: any) => sock.onMessage(m) };
}

describe('MeetingCall', () => {
	beforeEach(() => {
		vi.stubGlobal('MediaStream', FakeStream);
	});
	afterEach(() => vi.unstubAllGlobals());

	it('connects to everyone already there and sends its state', () => {
		const { call, sent, pcs, recv } = setup();
		call.join([{ urls: ['stun:x'] }]);
		recv({ type: 'welcome', you: person('m'), peers: [person('a'), person('z')], ice_servers: [{ urls: ['stun:y'] }], meeting: { title: 'T' } });
		expect(pcs.length).toBe(2);
		expect(pcs[0].config.iceServers).toEqual([{ urls: ['stun:y'] }]);
		expect(sent[0]).toEqual({ type: 'state', mic: false, cam: false, screen: false, hand: false });
		expect(call.people.length).toBe(2);
		recv({ type: 'joined', peer: person('b') });
		expect(pcs.length).toBe(3);
		recv({ type: 'left', peer_id: 'a' });
		expect(pcs[0].closed).toBe(true);
		expect(call.remotes.a).toBeUndefined();
	});

	it('answers offers, and the impolite side ignores a colliding one', async () => {
		const { call, sent, pcs, recv, flush } = setup();
		call.join([]);
		// 'm' > 'a': polite towards a; 'm' < 'z': impolite towards z.
		recv({ type: 'welcome', you: person('m'), peers: [person('a'), person('z')], ice_servers: [] });
		sent.length = 0;
		recv({ type: 'signal', from: 'a', data: { description: { type: 'offer', sdp: 'x' }, streams: { cam: 's1', screen: 's2' } } });
		await flush();
		expect(sent).toEqual([{ type: 'signal', to: 'a', data: { description: { type: 'answer', sdp: 'local-answer' }, streams: expect.any(Object) } }]);

		// Towards z: we're mid-offer when z's offer arrives, so we keep ours.
		const z = pcs[1];
		await z.onnegotiationneeded();
		expect(sent.at(-1).data.description.type).toBe('offer');
		sent.length = 0;
		recv({ type: 'signal', from: 'z', data: { description: { type: 'offer', sdp: 'y' } } });
		await flush();
		expect(sent).toEqual([]);
		expect(z.remote).toBeNull();
	});

	it('sorts incoming tracks into camera and screen', async () => {
		const { call, pcs, recv, flush } = setup();
		call.join([]);
		recv({ type: 'welcome', you: person('m'), peers: [person('a')], ice_servers: [] });
		recv({ type: 'signal', from: 'a', data: { description: { type: 'offer', sdp: 'x' }, streams: { cam: 'cam-a', screen: 'scr-a' } } });
		await flush();
		const scr = Object.assign(new FakeStream([{ kind: 'video' }]), { id: 'scr-a' });
		pcs[0].ontrack({ track: scr.tracks[0], streams: [scr] });
		expect(call.remotes.a.screen).toBe(scr);
		expect(call.remotes.a.cam).toBeNull();
	});

	it('stops for good when ended, removed or full, and never reconnects', () => {
		for (const why of ['ended', 'removed', 'replaced'] as const) {
			const { call, sock, pcs, recv } = setup();
			call.join([]);
			recv({ type: 'welcome', you: person('m'), peers: [person('a')], ice_servers: [] });
			recv({ type: why });
			expect(call.ending).toBe(why);
			expect(sock.close).toHaveBeenCalled();
			expect(pcs[0].closed).toBe(true);
			expect(sock.shouldReconnect(1006)).toBe(false);
		}
		const { call, sock } = setup();
		call.join([]);
		expect(sock.shouldReconnect(1006)).toBe(true);
		expect(sock.shouldReconnect(4009)).toBe(false);
		expect(call.ending).toBe('full');
	});

	it('mutes the microphone without stopping it, and stops the camera', async () => {
		const audio = { kind: 'audio', enabled: true, stop: vi.fn() };
		const video = { kind: 'video', enabled: true, stop: vi.fn() };
		vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: async (c: any) => new FakeStream([c.audio ? audio : video]) } });
		const { call, pcs, recv, sent } = setup();
		await call.setMic(true);
		await call.setCam(true);
		call.join([]);
		recv({ type: 'welcome', you: person('m'), peers: [person('a')], ice_servers: [] });
		expect(pcs[0].senders.map((s) => s.track)).toEqual([audio, video]);
		await call.setMic(false);
		expect(audio.enabled).toBe(false);
		expect(audio.stop).not.toHaveBeenCalled();
		await call.setCam(false);
		expect(video.stop).toHaveBeenCalled();
		expect(pcs[0].senders.map((s) => s.track)).toEqual([audio]);
		expect(sent.at(-1)).toEqual({ type: 'state', mic: false, cam: false, screen: false, hand: false });
	});

	it('tells the person when the browser blocks a device', async () => {
		const told: string[] = [];
		vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: async () => { throw Object.assign(new Error('no'), { name: 'NotAllowedError' }); } } });
		const call = new MeetingCall('ABC', (m) => told.push(m));
		await call.setCam(true);
		expect(call.cam).toBe(false);
		expect(told[0]).toMatch(/blocked your camera/);
	});
});
