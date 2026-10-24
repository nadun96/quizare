// A peer-to-peer meeting (D-69): every browser sends its microphone, camera
// and screen straight to every other one. The server only passes the set-up
// messages between them (/ws/meet/{code}); media never goes through it.
// Each pair follows WebRTC's "perfect negotiation", so either side can add or
// remove a track at any time without the two offers colliding.
import { BoardState, type BoardEvent, type BoardView, type NewStroke, type Stroke } from '../board/strokes.svelte';
import { LiveSocket, type Message } from '../socket';
import { applyRoster, CLOSE_FULL, polite, streamKind, type Ending, type Meeting, type Person, type StreamIds } from './mesh';

export type RemoteMedia = { cam: MediaStream | null; screen: MediaStream | null; link: RTCPeerConnectionState; rev: number };

type Link = {
	pc: RTCPeerConnection;
	polite: boolean;
	makingOffer: boolean;
	ignoreOffer: boolean;
	ids?: StreamIds;
	queue: Promise<void>;
};

// Small enough that eight people each sending to seven others still fits a
// school connection.
const CAMERA: MediaTrackConstraints = { width: { ideal: 640 }, height: { ideal: 360 }, frameRate: { ideal: 24, max: 30 } };

export class MeetingCall {
	people = $state.raw<Person[]>([]);
	me = $state.raw<Person | null>(null);
	meeting = $state.raw<Meeting | null>(null);
	remotes = $state.raw<Record<string, RemoteMedia>>({});
	ending = $state<Ending | null>(null);
	connected = $state(false);
	mic = $state(false);
	cam = $state(false);
	screen = $state(false);
	hand = $state(false);
	/** Changes whenever this person's own camera or screen stream does. */
	localRev = $state(0);
	/** What this person sends: one stream for the microphone and camera, one for the screen. */
	readonly local = new MediaStream();
	readonly screenStream = new MediaStream();
	/** The meeting's whiteboard (D-72): shown or hidden for everyone by the host. */
	readonly board = new BoardState();
	private requests = new Map<string, { resolve: (m: Message) => void; reject: (e: Error) => void; timer: ReturnType<typeof setTimeout> }>();
	private seq = 0;
	private links = new Map<string, Link>();
	private socket: LiveSocket | null = null;
	private ice: RTCIceServer[] = [];

	constructor(
		private code: string,
		/** Tells the person about something that went wrong with their devices. */
		private notify: (message: string) => void = () => {},
		private makeSocket: (path: string) => LiveSocket = (p) => new LiveSocket(p),
		private makePC: (c: RTCConfiguration) => RTCPeerConnection = (c) => new RTCPeerConnection(c)
	) {}

	/** Joins, sending whatever the person turned on in the lobby. */
	join(ice: RTCIceServer[]) {
		this.ice = ice;
		const s = this.makeSocket('/ws/meet/' + encodeURIComponent(this.code));
		this.socket = s;
		s.onStatus = (c) => (this.connected = c);
		s.shouldReconnect = (code) => {
			if (code === CLOSE_FULL) this.finish('full');
			return !this.ending;
		};
		s.onMessage = (m) => this.onMessage(m);
		s.open();
	}

	private onMessage(m: Message) {
		switch (m.type) {
			case 'welcome':
				// A first join, or a reconnect: the server gave a new peer id, so
				// every connection starts over.
				for (const id of [...this.links.keys()]) this.drop(id);
				this.me = m.you as Person;
				this.meeting = m.meeting as Meeting;
				if (Array.isArray(m.ice_servers)) this.ice = m.ice_servers as RTCIceServer[];
				this.people = applyRoster(this.people, m);
				if (m.board) this.board.load(m.board as BoardView);
				this.sendState();
				for (const p of this.people) this.link(p.peer_id);
				return;
			case 'joined':
				this.people = applyRoster(this.people, m);
				this.link((m.peer as Person).peer_id);
				return;
			case 'left':
				this.people = applyRoster(this.people, m);
				this.drop(m.peer_id as string);
				return;
			case 'state':
				this.people = applyRoster(this.people, m);
				return;
			case 'signal':
				this.onSignal(m.from as string, (m.data ?? {}) as SignalData);
				return;
			case 'board':
				this.board.apply(m as unknown as BoardEvent);
				return;
			case 'board_access':
				this.board.open = !!m.open;
				this.board.mode = String(m.mode ?? 'host');
				this.board.canDraw = !!m.can_draw;
				return;
			case 'board_ack': {
				const r = this.requests.get(String(m.req));
				if (!r) return;
				this.requests.delete(String(m.req));
				clearTimeout(r.timer);
				if (m.error) r.reject(new Error(String(m.error)));
				else r.resolve(m);
				return;
			}
			case 'ended':
			case 'removed':
			case 'replaced':
				this.finish(m.type);
				return;
		}
	}

	private send(m: Record<string, unknown>) {
		this.socket?.send(m);
	}

	/** A whiteboard request: resolves with the server's answer. */
	private request(type: string, body: Record<string, unknown>): Promise<Message> {
		const req = 'r' + ++this.seq;
		return new Promise((resolve, reject) => {
			if (!this.socket?.send({ type, req, ...body })) return reject(new Error('Not connected; try again in a moment'));
			const timer = setTimeout(() => {
				this.requests.delete(req);
				reject(new Error('The board didn’t answer; try again'));
			}, 10_000);
			this.requests.set(req, { resolve, reject, timer });
		});
	}

	/** What the whiteboard component calls to draw, erase and clear. */
	readonly boardClient = {
		add: async (strokes: NewStroke[]) => ((await this.request('board_add', { strokes })).strokes ?? []) as Stroke[],
		erase: async (ids: number[], gesture = '') => ((await this.request('board_erase', { ids, gesture })).removed ?? []) as number[],
		clear: async () => {
			await this.request('board_clear', {});
		}
	};

	/** Host only (the server checks): show or hide the board, and who draws. */
	setBoard(open: boolean, mode = this.board.mode) {
		this.send({ type: 'board_access', open, mode });
	}

	private sendState() {
		this.send({ type: 'state', mic: this.mic, cam: this.cam, screen: this.screen, hand: this.hand });
	}

	private ids(): StreamIds {
		return { cam: this.local.id, screen: this.screenStream.id };
	}

	private setRemote(id: string, f: (r: RemoteMedia) => Partial<RemoteMedia>) {
		const r = this.remotes[id] ?? { cam: null, screen: null, link: 'new', rev: 0 };
		this.remotes = { ...this.remotes, [id]: { ...r, ...f(r), rev: r.rev + 1 } };
	}

	/** The connection to one other person, made on first need. */
	private link(id: string): Link {
		const have = this.links.get(id);
		if (have) return have;
		const pc = this.makePC({ iceServers: this.ice });
		const l: Link = { pc, polite: polite(this.me?.peer_id ?? '', id), makingOffer: false, ignoreOffer: false, queue: Promise.resolve() };
		this.links.set(id, l);
		this.setRemote(id, () => ({}));
		pc.onnegotiationneeded = async () => {
			try {
				l.makingOffer = true;
				await pc.setLocalDescription();
				this.send({ type: 'signal', to: id, data: { description: pc.localDescription, streams: this.ids() } });
			} catch {
				// A newer negotiation took over; it sends its own offer.
			} finally {
				l.makingOffer = false;
			}
		};
		pc.onicecandidate = (e) => {
			if (e.candidate) this.send({ type: 'signal', to: id, data: { candidate: e.candidate } });
		};
		pc.oniceconnectionstatechange = () => {
			if (pc.iceConnectionState === 'failed') pc.restartIce();
		};
		pc.onconnectionstatechange = () => this.links.get(id) === l && this.setRemote(id, () => ({ link: pc.connectionState }));
		pc.ontrack = (e) => {
			const stream = e.streams[0] ?? new MediaStream([e.track]);
			const kind = streamKind(l.ids, stream.id);
			const update = () => this.links.get(id) === l && this.setRemote(id, () => ({ [kind]: stream.getTracks().length ? stream : null }));
			stream.onremovetrack = update;
			stream.onaddtrack = update;
			update();
		};
		for (const t of this.local.getTracks()) pc.addTrack(t, this.local);
		for (const t of this.screenStream.getTracks()) pc.addTrack(t, this.screenStream);
		return l;
	}

	private drop(id: string) {
		this.links.get(id)?.pc.close();
		this.links.delete(id);
		const { [id]: _, ...rest } = this.remotes;
		this.remotes = rest;
	}

	private onSignal(from: string, data: SignalData) {
		const l = this.link(from);
		// One message at a time per connection: each step waits on the last.
		l.queue = l.queue
			.then(async () => {
				if (data.streams) l.ids = data.streams;
				const pc = l.pc;
				if (data.description) {
					const d = data.description;
					const collision = d.type === 'offer' && (l.makingOffer || pc.signalingState !== 'stable');
					l.ignoreOffer = !l.polite && collision;
					if (l.ignoreOffer) return;
					await pc.setRemoteDescription(d);
					if (d.type === 'offer') {
						await pc.setLocalDescription();
						this.send({ type: 'signal', to: from, data: { description: pc.localDescription, streams: this.ids() } });
					}
				} else if (data.candidate) {
					try {
						await pc.addIceCandidate(data.candidate);
					} catch (e) {
						if (!l.ignoreOffer) throw e;
					}
				}
			})
			.catch(() => {});
	}

	private addTrack(t: MediaStreamTrack, stream: MediaStream) {
		stream.addTrack(t);
		for (const l of this.links.values()) l.pc.addTrack(t, stream);
		this.localRev++;
	}

	private removeTrack(t: MediaStreamTrack, stream: MediaStream) {
		t.stop();
		stream.removeTrack(t);
		for (const l of this.links.values()) {
			const s = l.pc.getSenders().find((s) => s.track === t);
			if (s) l.pc.removeTrack(s);
		}
		this.localRev++;
	}

	private async device(get: () => Promise<MediaStream>, what: string): Promise<MediaStreamTrack | null> {
		try {
			return (await get()).getTracks()[0] ?? null;
		} catch (e) {
			const name = (e as DOMException)?.name;
			if (name !== 'AbortError') {
				this.notify(name === 'NotAllowedError' ? `The browser blocked your ${what}. Allow it in the address bar, then try again.` : `Couldn't start your ${what}.`);
			}
			return null;
		}
	}

	/** The microphone mutes rather than stopping, so unmuting is instant. */
	async setMic(on: boolean) {
		let t = this.local.getAudioTracks()[0];
		if (on && !t) {
			const got = await this.device(() => navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true } }), 'microphone');
			if (!got) return;
			this.addTrack(got, this.local);
			t = got;
		}
		if (t) t.enabled = on;
		this.mic = on && !!t;
		this.sendState();
	}

	/** The camera stops when turned off, so its light goes out. */
	async setCam(on: boolean) {
		const t = this.local.getVideoTracks()[0];
		if (on && !t) {
			const got = await this.device(() => navigator.mediaDevices.getUserMedia({ video: CAMERA }), 'camera');
			if (!got) return;
			got.onended = () => this.setCam(false);
			this.addTrack(got, this.local);
		} else if (!on && t) {
			this.removeTrack(t, this.local);
		}
		this.cam = !!this.local.getVideoTracks()[0];
		this.sendState();
	}

	async setScreen(on: boolean) {
		const t = this.screenStream.getVideoTracks()[0];
		if (on && !t) {
			const got = await this.device(() => navigator.mediaDevices.getDisplayMedia({ video: { frameRate: { ideal: 10, max: 15 } } }), 'screen sharing');
			if (!got) return;
			got.contentHint = 'detail';
			// The browser's own "Stop sharing" button.
			got.onended = () => this.setScreen(false);
			this.addTrack(got, this.screenStream);
		} else if (!on && t) {
			this.removeTrack(t, this.screenStream);
		}
		this.screen = !!this.screenStream.getVideoTracks()[0];
		this.sendState();
	}

	setHand(up: boolean) {
		this.hand = up;
		this.sendState();
	}

	/** Host only: the server checks. */
	remove(peerId: string) {
		this.send({ type: 'remove', peer_id: peerId });
	}

	endForAll() {
		this.send({ type: 'end' });
	}

	leave() {
		this.finish('left');
	}

	private finish(why: Ending) {
		if (this.ending) return;
		this.ending = why;
		this.socket?.close();
		for (const r of this.requests.values()) {
			clearTimeout(r.timer);
			r.reject(new Error('The meeting is over'));
		}
		this.requests.clear();
		for (const id of [...this.links.keys()]) this.drop(id);
		for (const t of [...this.local.getTracks(), ...this.screenStream.getTracks()]) t.stop();
		this.mic = this.cam = this.screen = this.hand = false;
		this.localRev++;
	}
}

type SignalData = { description?: RTCSessionDescriptionInit; candidate?: RTCIceCandidateInit; streams?: StreamIds };
