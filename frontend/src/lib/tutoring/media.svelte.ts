// The session's media through LiveKit (D-58). livekit-client is loaded only
// on the tutoring page, so no other page grows (NFR-09). Devices start only
// after the person's own click and the browser's prompt (PO-3, TS-FR-22);
// nothing here turns a device on by itself.
import type { LocalTrack, Participant, Room, Track, TrackPublication } from 'livekit-client';

export type Tile = {
	key: string;
	participant: string; // identity
	name: string;
	label: string; // "Screen 2", "Document camera"
	source: 'camera' | 'screen_share';
	track: Track;
	local: boolean;
};

export type MediaState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'disconnected' | 'error';

type LK = typeof import('livekit-client');

export class SessionMedia {
	state = $state<MediaState>('idle');
	error = $state('');
	tiles = $state.raw<Tile[]>([]);
	micOn = $state(false);
	/** What the media server currently lets this person publish. */
	canMic = $state(false);
	canCamera = $state(false);
	canScreen = $state(false);
	/** The browser blocked sound until a click (autoplay rules). */
	needsClick = $state(false);
	private lk: LK | null = null;
	private room: Room | null = null;
	private audio = new Map<string, HTMLMediaElement>();
	private labels = new Map<string, string>(); // track sid → label
	private counter = 0;

	constructor(private audioHost: () => HTMLElement | undefined) {}

	/**
	 * Joins the media room. teacherIds: a student's own camera and screen
	 * go to the teachers only, never to other students (TS-FR-37).
	 */
	async connect(url: string, token: string, opts: { student: boolean; teacherIds: string[] }) {
		this.state = 'connecting';
		this.error = '';
		try {
			const lk = (this.lk ??= await import('livekit-client'));
			const room = new lk.Room({ adaptiveStream: true, dynacast: true, disconnectOnPageLeave: true });
			this.room = room;
			const E = lk.RoomEvent;
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
				.on(E.ParticipantPermissionsChanged, () => this.readPermissions())
				.on(E.AudioPlaybackStatusChanged, () => (this.needsClick = !room.canPlaybackAudio))
				.on(E.Reconnecting, () => (this.state = 'reconnecting'))
				.on(E.Reconnected, () => (this.state = 'connected'))
				.on(E.Disconnected, () => {
					this.state = 'disconnected';
					this.clear();
				});
			await room.connect(url, token, { autoSubscribe: true });
			if (opts.student) {
				await room.localParticipant.setTrackSubscriptionPermissions(false, opts.teacherIds.map((id) => ({ participantIdentity: id, allowAll: true })));
			}
			this.state = 'connected';
			this.needsClick = !room.canPlaybackAudio;
			this.readPermissions();
			for (const p of room.remoteParticipants.values()) for (const pub of p.trackPublications.values()) if (pub.track) this.attach(pub.track);
			this.refresh();
		} catch (e) {
			this.state = 'error';
			this.error = e instanceof Error ? e.message : 'Could not connect to the video server';
		}
	}

	/** Teachers may change as co-teachers come; students then re-allow them. */
	async allowViewers(teacherIds: string[]) {
		await this.room?.localParticipant.setTrackSubscriptionPermissions(false, teacherIds.map((id) => ({ participantIdentity: id, allowAll: true })));
	}

	private readPermissions() {
		const p = this.room?.localParticipant.permissions;
		const src = new Set((p?.canPublishSources ?? []).map(String));
		const any = !!p?.canPublish;
		// Numbers or names, depending on the protocol version: 1 camera, 2 microphone, 3 screen.
		this.canCamera = any && (src.has('1') || src.has('CAMERA') || src.has('camera'));
		this.canMic = any && (src.has('2') || src.has('MICROPHONE') || src.has('microphone'));
		this.canScreen = any && (src.has('3') || src.has('SCREEN_SHARE') || src.has('screen_share'));
		if (!this.canMic) this.micOn = false;
		this.refresh();
	}

	async startAudio() {
		await this.room?.startAudio();
		this.needsClick = !(this.room?.canPlaybackAudio ?? true);
	}

	private attach(t: Track) {
		if (t.kind === 'audio') {
			const el = t.attach();
			this.audio.set(t.sid ?? String(this.counter++), el);
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

	/** Rebuilds the list of video tiles from the room. */
	refresh() {
		const room = this.room;
		if (!room) return;
		const tiles: Tile[] = [];
		const add = (p: Participant, pub: TrackPublication, local: boolean) => {
			if (!pub.track || pub.kind !== 'video' || pub.isMuted) return;
			const source = pub.source === 'screen_share' ? 'screen_share' : 'camera';
			tiles.push({
				key: pub.trackSid,
				participant: p.identity,
				name: p.name || p.identity,
				label: this.labels.get(pub.trackSid) ?? pub.trackName ?? (source === 'screen_share' ? 'Screen' : 'Camera'),
				source,
				track: pub.track,
				local
			});
		};
		for (const pub of room.localParticipant.trackPublications.values()) add(room.localParticipant, pub, true);
		for (const p of room.remoteParticipants.values()) for (const pub of p.trackPublications.values()) add(p, pub, false);
		// Screens first: they are what students read (TS-FR-13 spotlight).
		tiles.sort((a, b) => (a.source === b.source ? 0 : a.source === 'screen_share' ? -1 : 1));
		this.tiles = tiles;
		this.micOn = [...room.localParticipant.trackPublications.values()].some((p) => p.source === 'microphone' && !p.isMuted && p.track);
	}

	async setMic(on: boolean) {
		await this.room?.localParticipant.setMicrophoneEnabled(on);
		this.refresh();
	}

	/** Adds one more screen, window or tab (TS-FR-10), with its sound where the browser offers it (TS-FR-12). */
	async addScreen() {
		const lk = this.lk!;
		const tracks: LocalTrack[] = await lk.createLocalScreenTracks({ audio: true, resolution: lk.ScreenSharePresets.h1080fps15.resolution });
		const n = [...(this.room?.localParticipant.trackPublications.values() ?? [])].filter((p) => p.source === 'screen_share').length + 1;
		for (const t of tracks) {
			const pub = await this.room!.localParticipant.publishTrack(t, {
				name: t.kind === 'video' ? `Screen ${n}` : `Screen ${n} sound`,
				// 1080p at a low frame rate keeps text sharp within ~1 Mbit/s (TS-NFR-05).
				screenShareEncoding: lk.ScreenSharePresets.h1080fps15.encoding,
				simulcast: true
			});
			if (t.kind === 'video') this.labels.set(pub.trackSid, `Screen ${n}`);
			t.mediaStreamTrack.addEventListener('ended', () => this.room?.localParticipant.unpublishTrack(t));
		}
		this.refresh();
	}

	/** Adds a camera (TS-FR-11); several can run at once if the device allows. */
	async addCamera(deviceId?: string, label?: string) {
		const lk = this.lk!;
		const t = await lk.createLocalVideoTrack({ deviceId, resolution: lk.VideoPresets.h720.resolution });
		const n = [...(this.room?.localParticipant.trackPublications.values() ?? [])].filter((p) => p.source === 'camera').length + 1;
		const name = label || (n === 1 ? 'Camera' : `Camera ${n}`);
		const pub = await this.room!.localParticipant.publishTrack(t, { name, simulcast: true });
		this.labels.set(pub.trackSid, name);
		this.refresh();
	}

	/** Stops one of this person's own tracks. */
	async stop(tile: Tile) {
		const pub = this.room?.localParticipant.trackPublications.get(tile.key);
		if (pub?.track) {
			// A screen's sound goes with it.
			if (tile.source === 'screen_share') {
				for (const p of this.room!.localParticipant.trackPublications.values()) if (p.trackName === `${tile.label} sound` && p.track) await this.room!.localParticipant.unpublishTrack(p.track as LocalTrack);
			}
			await this.room!.localParticipant.unpublishTrack(pub.track as LocalTrack);
		}
		this.refresh();
	}

	/** Cameras the browser can see (labels appear once a permission was given). */
	async cameras(): Promise<MediaDeviceInfo[]> {
		const lk = (this.lk ??= await import('livekit-client'));
		return lk.Room.getLocalDevices('videoinput');
	}

	async disconnect() {
		await this.room?.disconnect();
		this.clear();
		this.room = null;
	}
}
