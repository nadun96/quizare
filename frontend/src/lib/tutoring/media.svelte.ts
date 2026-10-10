// The session's media through LiveKit (D-58, D-60). Two rooms: the broadcast
// everyone watches, where only the broadcaster publishes, and backstage,
// where allowed students and co-teachers publish and only teachers watch.
// LiveKit enforces both from the tokens; this file only follows them.
// livekit-client is loaded only on the tutoring page (NFR-09). Devices start
// only after the person's own click and the browser's prompt (PO-3).
import type { LocalTrack, Participant, Room, Track, TrackPublication } from 'livekit-client';

export type Tile = {
	key: string;
	participant: string; // identity
	name: string;
	label: string; // "Screen 2", "Document camera"
	source: 'camera' | 'screen_share';
	track: Track;
	local: boolean;
	/** From backstage: seen by the teachers only. */
	backstage: boolean;
};

export type MediaState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'disconnected' | 'error';
export type Tokens = { url: string; main: string; stage?: string };
type LK = typeof import('livekit-client');
type Which = 'main' | 'stage';

/** How long to wait before joining the video again after the connection is lost for good: 2 s, doubling, at most 30 s. */
export function rejoinDelay(attempt: number): number {
	return Math.min(30_000, 2_000 * 2 ** Math.max(0, attempt));
}

export class SessionMedia {
	state = $state<MediaState>('idle');
	error = $state('');
	tiles = $state.raw<Tile[]>([]);
	micOn = $state(false);
	/** What this person may publish in the room they publish to. */
	canMic = $state(false);
	canCamera = $state(false);
	canScreen = $state(false);
	/** Where this person's devices go: the broadcast, or backstage. */
	target = $state<Which>('main');
	/** The browser blocked sound until a click (autoplay rules). */
	needsClick = $state(false);
	hasStage = $state(false);
	private lk: LK | null = null;
	private rooms: Partial<Record<Which, Room>> = {};
	private audio = new Map<string, HTMLMediaElement>();
	private labels = new Map<string, string>(); // track sid → label

	constructor(
		private audioHost: () => HTMLElement | undefined,
		/** Tells the person something they didn't do themselves happened to their devices. */
		private notify: (message: string) => void = () => {}
	) {}

	private async room(which: Which, url: string, token: string) {
		const lk = (this.lk ??= await import('livekit-client'));
		const room = new lk.Room({ adaptiveStream: true, dynacast: true, disconnectOnPageLeave: true });
		const E = lk.RoomEvent;
		// Events from a room this person has since left (a rejoin, or LiveKit
		// closing an older connection) must not touch the current one.
		const current = () => this.rooms[which] === room;
		room
			.on(E.TrackSubscribed, (t) => this.attach(t))
			.on(E.TrackUnsubscribed, (t) => this.detach(t))
			.on(E.LocalTrackPublished, () => this.refresh())
			.on(E.LocalTrackUnpublished, (pub) => {
				pub.track?.stop();
				this.refresh();
			})
			.on(E.TrackMuted, () => this.refresh())
			.on(E.TrackUnmuted, () => this.refresh())
			.on(E.ParticipantDisconnected, () => this.refresh())
			// A track taken away by the server (a permission revoked) arrives as unpublished.
			.on(E.TrackUnpublished, () => this.refresh())
			.on(E.TrackPublished, () => this.refresh())
			.on(E.ParticipantPermissionsChanged, () => this.readPermissions())
			.on(E.AudioPlaybackStatusChanged, () => (this.needsClick = !room.canPlaybackAudio));
		if (which === 'main') {
			room
				.on(E.Reconnecting, () => current() && (this.state = 'reconnecting'))
				.on(E.Reconnected, () => current() && (this.state = 'connected'))
				.on(E.Disconnected, () => {
					if (!current()) return;
					this.state = 'disconnected';
					this.clear();
				});
		} else {
			room.on(E.Disconnected, () => {
				if (!current()) return;
				delete this.rooms.stage;
				this.hasStage = false;
				this.refresh();
			});
		}
		await room.connect(url, token, { autoSubscribe: true });
		this.rooms[which] = room;
		for (const p of room.remoteParticipants.values()) for (const pub of p.trackPublications.values()) if (pub.track) this.attach(pub.track);
		return room;
	}

	/** Joins the broadcast, and backstage when the tokens include it. */
	async connect(t: Tokens, publishTo: Which) {
		// A rejoin: leave whatever is left of the last connection first.
		const old = Object.values(this.rooms);
		this.rooms = {};
		this.hasStage = false;
		for (const r of old) await r?.disconnect();
		this.state = 'connecting';
		this.error = '';
		this.target = publishTo;
		try {
			const main = await this.room('main', t.url, t.main);
			if (t.stage) await this.room('stage', t.url, t.stage);
			this.hasStage = !!this.rooms.stage;
			this.state = 'connected';
			this.needsClick = !main.canPlaybackAudio;
			this.readPermissions();
		} catch (e) {
			this.state = 'error';
			this.error = e instanceof Error ? e.message : 'Could not connect to the video server';
		}
	}

	/**
	 * Follows a change of role: someone who becomes (or stops being) the
	 * broadcaster publishes elsewhere, and a student newly allowed to share
	 * joins backstage. Their devices stop; they turn them on again
	 * themselves (PO-3).
	 */
	async update(t: Tokens, publishTo: Which) {
		if (publishTo !== this.target) {
			await this.stopAll(this.target);
			this.target = publishTo;
		}
		if (t.stage && !this.rooms.stage) {
			await this.room('stage', t.url, t.stage);
			this.hasStage = true;
		}
		this.readPermissions();
	}

	/** Whether the room this person publishes to is joined. */
	joined(which: Which = this.target): boolean {
		return !!this.rooms[which];
	}

	private readPermissions() {
		const p = this.rooms[this.target]?.localParticipant.permissions;
		const src = new Set((p?.canPublishSources ?? []).map(String));
		const any = !!p?.canPublish;
		// Numbers or names, depending on the protocol version: 1 camera, 2 microphone, 3 screen.
		this.canCamera = any && (src.has('1') || src.has('CAMERA') || src.has('camera'));
		this.canMic = any && (src.has('2') || src.has('MICROPHONE') || src.has('microphone'));
		this.canScreen = any && (src.has('3') || src.has('SCREEN_SHARE') || src.has('screen_share'));
		this.refresh();
	}

	async startAudio() {
		for (const r of Object.values(this.rooms)) await r?.startAudio();
		this.needsClick = !(this.rooms.main?.canPlaybackAudio ?? true);
	}

	private attach(t: Track) {
		if (t.kind === 'audio') {
			const el = t.attach();
			this.audio.set(t.sid ?? crypto.randomUUID(), el);
			this.audioHost()?.append(el);
		}
		this.refresh();
	}

	private detach(t: Track) {
		if (t.kind === 'audio') for (const el of t.detach()) el.remove();
		this.refresh();
	}

	private clear() {
		for (const el of this.audio.values()) el.remove();
		this.audio.clear();
		this.tiles = [];
		this.micOn = false;
	}

	/** Rebuilds the list of video tiles from both rooms. */
	refresh() {
		const tiles: Tile[] = [];
		let mic = false;
		for (const which of ['main', 'stage'] as const) {
			const room = this.rooms[which];
			if (!room) continue;
			const add = (p: Participant, pub: TrackPublication, local: boolean) => {
				if (!pub.track || pub.kind !== 'video' || pub.isMuted) return;
				const source = pub.source === 'screen_share' ? 'screen_share' : 'camera';
				tiles.push({
					key: which + ':' + pub.trackSid,
					participant: p.identity,
					name: p.name || p.identity,
					label: this.labels.get(pub.trackSid) ?? pub.trackName ?? (source === 'screen_share' ? 'Screen' : 'Camera'),
					source,
					track: pub.track,
					local,
					backstage: which === 'stage'
				});
			};
			for (const pub of room.localParticipant.trackPublications.values()) {
				add(room.localParticipant, pub, true);
				if (pub.source === 'microphone' && !pub.isMuted && pub.track) mic = true;
			}
			for (const p of room.remoteParticipants.values()) for (const pub of p.trackPublications.values()) add(p, pub, false);
		}
		// The broadcast first, screens before cameras: what students read (TS-FR-13 spotlight).
		tiles.sort((a, b) => Number(a.backstage) - Number(b.backstage) || (a.source === b.source ? 0 : a.source === 'screen_share' ? -1 : 1));
		this.tiles = tiles;
		this.micOn = mic;
	}

	private get publisher() {
		const r = this.rooms[this.target];
		if (!r) throw new Error('Not connected yet');
		return r.localParticipant;
	}

	async setMic(on: boolean) {
		await this.publisher.setMicrophoneEnabled(on);
		this.refresh();
	}

	/** Adds one more screen, window or tab (TS-FR-10), with its sound where the browser offers it (TS-FR-12). */
	async addScreen() {
		const lk = this.lk!;
		const lp = this.publisher;
		const tracks: LocalTrack[] = await lk.createLocalScreenTracks({ audio: true, resolution: lk.ScreenSharePresets.h1080fps15.resolution });
		const n = [...lp.trackPublications.values()].filter((p) => p.source === 'screen_share').length + 1;
		for (const t of tracks) {
			const pub = await lp.publishTrack(t, {
				name: t.kind === 'video' ? `Screen ${n}` : `Screen ${n} sound`,
				// 1080p at a low frame rate keeps text sharp within ~1 Mbit/s (TS-NFR-05).
				screenShareEncoding: lk.ScreenSharePresets.h1080fps15.encoding,
				simulcast: true
			});
			if (t.kind === 'video') this.labels.set(pub.trackSid, `Screen ${n}`);
			t.mediaStreamTrack.addEventListener('ended', () => lp.unpublishTrack(t));
		}
		this.refresh();
	}

	/** Adds a camera (TS-FR-11); several can run at once if the device allows. */
	async addCamera(deviceId?: string, label?: string) {
		const lk = this.lk!;
		const lp = this.publisher;
		const t = await lk.createLocalVideoTrack({ deviceId, resolution: lk.VideoPresets.h720.resolution });
		const n = [...lp.trackPublications.values()].filter((p) => p.source === 'camera').length + 1;
		const name = label || (n === 1 ? 'Camera' : `Camera ${n}`);
		const pub = await lp.publishTrack(t, { name, simulcast: true });
		this.labels.set(pub.trackSid, name);
		// The camera stopped on its own (unplugged, taken by another app, the
		// browser's or the system's privacy switch): take it out rather than
		// leave a frozen tile, and say so.
		t.mediaStreamTrack.addEventListener('ended', () => {
			if (![...lp.trackPublications.values()].some((p) => p.track === t)) return;
			lp.unpublishTrack(t).finally(() => this.refresh());
			this.notify(`${name} stopped. Add it again from "Add a camera".`);
		});
		this.refresh();
	}

	/** Stops one of this person's own tracks. */
	async stop(tile: Tile) {
		const room = this.rooms[tile.backstage ? 'stage' : 'main'];
		const sid = tile.key.split(':')[1];
		const pub = room?.localParticipant.trackPublications.get(sid);
		if (room && pub?.track) {
			// A screen's sound goes with it.
			if (tile.source === 'screen_share') {
				for (const p of room.localParticipant.trackPublications.values()) if (p.trackName === `${tile.label} sound` && p.track) await room.localParticipant.unpublishTrack(p.track as LocalTrack);
			}
			await room.localParticipant.unpublishTrack(pub.track as LocalTrack);
		}
		this.refresh();
	}

	private async stopAll(which: Which) {
		const lp = this.rooms[which]?.localParticipant;
		if (!lp) return;
		for (const pub of lp.trackPublications.values()) if (pub.track) await lp.unpublishTrack(pub.track as LocalTrack);
		this.refresh();
	}

	/** Cameras the browser can see (labels appear once a permission was given). */
	async cameras(): Promise<MediaDeviceInfo[]> {
		const lk = (this.lk ??= await import('livekit-client'));
		return lk.Room.getLocalDevices('videoinput');
	}

	async disconnect() {
		for (const r of Object.values(this.rooms)) await r?.disconnect();
		this.rooms = {};
		this.hasStage = false;
		this.clear();
	}
}
