// Peer-to-peer meetings (D-69): the parts of the call that are plain data,
// kept apart from the browser's WebRTC objects so they can be tested.

export type Meeting = {
	id: string;
	host_id: string;
	host_name?: string;
	title: string;
	code: string;
	classroom_id: string | null;
	max_people: number;
	status: 'open' | 'ended';
	created_at: string;
	ended_at?: string;
	online: number;
};

export type MeetingAccess = { meeting: Meeting; host: boolean; ice_servers: RTCIceServer[] };

export type Attendance = { user_id: string; name: string; first_joined: string; last_seen: string; joins: number; online: boolean };

/** Someone in the call, as the signalling server describes them. */
export type Person = { peer_id: string; user_id: string; name: string; host: boolean; mic: boolean; cam: boolean; screen: boolean; hand: boolean };

export type Signal = { type: string; [k: string]: unknown };

/** Why the call is over, if it is. */
export type Ending = 'ended' | 'removed' | 'replaced' | 'full' | 'left';

export const ENDINGS: Record<Ending, { title: string; body: string }> = {
	ended: { title: 'The meeting has ended', body: 'The host ended it for everyone.' },
	removed: { title: 'You were removed', body: 'The host removed you from the meeting.' },
	replaced: { title: 'Joined somewhere else', body: 'You joined this meeting from another tab or device, so this one has left.' },
	full: { title: 'The meeting is full', body: 'Try again when someone leaves.' },
	left: { title: 'You left the meeting', body: 'Rejoin with the same link while the meeting is open.' }
};

/** The close code the server uses for a full meeting. */
export const CLOSE_FULL = 4009;

/**
 * Perfect negotiation needs one side of each pair to give way when both send
 * an offer at once. Both sides work out the same answer from the two ids.
 */
export function polite(me: string, them: string): boolean {
	return me > them;
}

/**
 * The people in the call after a message from the server. `welcome` starts
 * over (a reconnect gets a new peer id, and everyone else may have changed).
 */
export function applyRoster(people: Person[], m: Signal): Person[] {
	switch (m.type) {
		case 'welcome':
			return [...((m.peers as Person[]) ?? [])];
		case 'joined': {
			const p = m.peer as Person;
			return [...people.filter((o) => o.peer_id !== p.peer_id && o.user_id !== p.user_id), p];
		}
		case 'left':
			return people.filter((o) => o.peer_id !== m.peer_id);
		case 'state':
			return people.map((o) =>
				o.peer_id === m.peer_id ? { ...o, mic: !!m.mic, cam: !!m.cam, screen: !!m.screen, hand: !!m.hand } : o
			);
	}
	return people;
}

/** Columns for a grid of n tiles: one, then two, then three. */
export function gridColumns(n: number): number {
	return n <= 1 ? 1 : n <= 4 ? 2 : 3;
}

/**
 * Which of a sender's streams is which. Each description carries the ids of
 * the sender's camera and screen streams, since the track itself doesn't say.
 */
export type StreamIds = { cam?: string; screen?: string };

export function streamKind(ids: StreamIds | undefined, streamId: string): 'cam' | 'screen' {
	return ids?.screen && ids.screen === streamId ? 'screen' : 'cam';
}

/** Initials for a tile with its camera off. */
export function initials(name: string): string {
	const parts = name.trim().split(/\s+/).filter(Boolean);
	if (!parts.length) return '?';
	return ((parts[0][0] ?? '') + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase();
}

/**
 * How to lay n 16:9 tiles out in a w×h box so they're as large as possible
 * with nothing scrolling (D-72): the column count, and each tile's size.
 */
export function fitGrid(n: number, w: number, h: number, gap = 8, aspect = 16 / 9): { cols: number; w: number; h: number } {
	if (n < 1 || w <= 0 || h <= 0) return { cols: 1, w: 0, h: 0 };
	let best = { cols: 1, w: 0, h: 0 };
	for (let cols = 1; cols <= n; cols++) {
		const rows = Math.ceil(n / cols);
		const tw = Math.min((w - gap * (cols - 1)) / cols, ((h - gap * (rows - 1)) / rows) * aspect);
		if (tw > best.w) best = { cols, w: Math.floor(tw), h: Math.floor(tw / aspect) };
	}
	return best;
}

export type MeetingLimits = { max_people: number; max_open: number };

/**
 * The sizes a teacher can choose under the admin's limit (D-72; 0 is
 * unlimited), and the one chosen at first.
 */
export function sizeOptions(max: number): { sizes: number[]; initial: number } {
	const presets = [4, 6, 8, 12, 20, 30, 50];
	let sizes = max > 0 ? presets.filter((n) => n <= max) : [...presets, 0];
	if (max > 0 && !sizes.includes(max)) sizes = [...sizes, max];
	const initial = max > 0 && max < 6 ? max : 6;
	return { sizes, initial };
}

/** One line on what a size means for the people in the call. */
export function sizeHint(n: number): string {
	if (n > 0 && n <= 4) return 'Best on slow connections.';
	if (n > 0 && n <= 8) return 'Everyone needs a good connection.';
	return 'Everyone sends video to everyone: past 8 people, most should keep their camera off.';
}
