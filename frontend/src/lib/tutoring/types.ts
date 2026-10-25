// What the tutoring service sends (tutoring/internal/tutor).

export type TutoringSession = {
	id: string;
	teacher_id: string;
	teacher_name: string;
	classroom_id: string;
	classroom_name: string;
	title: string;
	join_code: string;
	status: 'open' | 'live' | 'ended';
	admit_mode: 'auto' | 'manual';
	locked: boolean;
	chat_mode: 'off' | 'to_teacher' | 'everyone' | 'announcements';
	slow_seconds: number;
	pinned_id?: number;
	/** Who broadcasts instead of the lead teacher (TS-FR-17); unset = the lead teacher. */
	broadcaster_id?: string;
	/** Shown only to the person asked to broadcast. */
	broadcast_offer?: string;
	layout: 'spotlight' | 'side' | 'grid';
	/** Breakout groups (D-79). */
	groups_open: boolean;
	groups_close_at?: string;
	groups_choose: boolean;
	group_chat: boolean;
	group_mic: boolean;
	group_camera: boolean;
	group_screen: boolean;
	scheduled_at?: string;
	created_at: string;
	started_at?: string;
	ended_at?: string;
	/** What the session will use (TS-FR-08, D-81). */
	plan: Plan;
	/** What the service switched off to stay within the bandwidth budget (TS-FR-38). */
	auto_off: AutoOffStep[];
};

export type TutoringParticipant = {
	user_id: string;
	name: string;
	role: 'teacher' | 'coteacher' | 'student';
	state: 'waiting' | 'admitted' | 'refused' | 'removed';
	allow_mic: boolean;
	allow_camera: boolean;
	allow_screen: boolean;
	chat_muted: boolean;
	hand_at?: string;
	first_at: string;
	online: boolean;
	/** A student's breakout group, or the group a teacher is visiting (D-79). */
	group?: number;
	visiting?: number;
};

export type Group = { id: number; name: string; members: string[]; size: number; help_at?: string };

export type ChatMessage = {
	id: number;
	user_id: string;
	name: string;
	from_teacher: boolean;
	audience: 'everyone' | 'teachers' | 'one' | 'group';
	to_user?: string;
	group_id?: number;
	text: string;
	created_at: string;
};

export type TutoringView = {
	session: TutoringSession;
	me: TutoringParticipant;
	participants?: TutoringParticipant[];
	online: number;
	waiting: number;
	pinned?: ChatMessage;
	media_url: string;
	/** The lead teacher and co-teachers (TS-FR-73). */
	teachers: { id: string; name: string; role: 'teacher' | 'coteacher' }[];
	/** The recording under way, shown to everyone (TS-FR-76, D-80). */
	recording?: { id: string; status: 'recording' | 'paused'; started_by: string; started_by_name: string; started_at: string };
	/** All groups for teachers; for students names and sizes, and who is in their own group. */
	groups: Group[];
	group_mates?: { id: string; name: string }[];
};

/** The breakout group whose room this person is in now: a student's own, or the one a teacher visits. */
export function groupNow(v: TutoringView): Group | undefined {
	if (!v.session.groups_open || v.session.status !== 'live') return undefined;
	const id = isStaffRole(v.me.role) ? v.me.visiting : v.me.group;
	return id ? v.groups.find((g) => g.id === id) : undefined;
}

/** Seconds until the groups close, when a countdown is running. */
export function secondsLeft(closeAt: string | undefined, now: number): number | null {
	if (!closeAt) return null;
	return Math.max(0, Math.ceil((Date.parse(closeAt) - now) / 1000));
}

export const isStaffRole = (r?: string) => r === 'teacher' || r === 'coteacher';

/** Whether this person is the one broadcasting now. */
export function isBroadcaster(v: TutoringView): boolean {
	return v.session.broadcaster_id ? v.session.broadcaster_id === v.me.user_id : v.me.role === 'teacher';
}

/**
 * Where this person's devices go (D-60): the broadcast, or backstage for the
 * teachers. The lead teacher's microphone stays in the broadcast while
 * someone else broadcasts.
 */
export function publishTarget(v: TutoringView): 'main' | 'stage' | 'group' {
	if (groupNow(v)) return 'group'; // in a breakout group, devices go to the group (D-79)
	return isBroadcaster(v) || v.me.role === 'teacher' ? 'main' : 'stage';
}

/** The broadcaster's name, for "Ann is broadcasting". */
export function broadcasterName(v: TutoringView): string {
	const id = v.session.broadcaster_id;
	if (!id) return v.session.teacher_name;
	return v.participants?.find((p) => p.user_id === id)?.name ?? v.teachers.find((t) => t.id === id)?.name ?? 'A student';
}

export const LAYOUTS: { id: TutoringSession['layout']; label: string }[] = [
	{ id: 'spotlight', label: 'Spotlight' },
	{ id: 'side', label: 'Side by side' },
	{ id: 'grid', label: 'Grid' }
];

export type Attendee = { user_id: string; name: string; role: string; first_at: string; last_left_at?: string; visits: number; seconds: number; online: boolean };

export const CHAT_MODES: { id: TutoringSession['chat_mode']; label: string; student: string }[] = [
	{ id: 'to_teacher', label: 'Students to teacher only', student: 'Your messages go to the teacher only.' },
	{ id: 'everyone', label: 'Everyone', student: 'Everyone sees your messages.' },
	{ id: 'announcements', label: 'Teacher announcements only', student: 'Only the teacher can write now.' },
	{ id: 'off', label: 'Off', student: 'Chat is off.' }
];

/** Splits text into plain parts and links, so chat shows links without ever rendering HTML (TS-FR-43). */
export function linkParts(text: string): { text: string; href?: string }[] {
	const out: { text: string; href?: string }[] = [];
	const re = /https?:\/\/[^\s<>"']+[^\s<>"'.,;:!?)\]]/g;
	let last = 0;
	for (const m of text.matchAll(re)) {
		if (m.index! > last) out.push({ text: text.slice(last, m.index) });
		out.push({ text: m[0], href: m[0] });
		last = m.index! + m[0].length;
	}
	if (last < text.length) out.push({ text: text.slice(last) });
	return out;
}

/** The hand queue: raised hands, earliest first. */
export function handQueue(ps: TutoringParticipant[]): TutoringParticipant[] {
	return ps.filter((p) => p.hand_at && p.state === 'admitted').sort((a, b) => Date.parse(a.hand_at!) - Date.parse(b.hand_at!));
}

/** Whether a message is the student's question to the teacher, or a private reply. */
export function audienceLabel(m: ChatMessage, meId: string): string {
	if (m.audience === 'group') return 'group';
	if (m.audience === 'teachers') return m.user_id === meId ? 'to the teacher' : 'question to teachers';
	if (m.audience === 'one') return m.to_user === meId ? 'private, to you' : 'private';
	return '';
}

/** A recording of a session (D-80): the presenter's screen and voice, kept 2 days. */
export type Recording = {
	id: string;
	session_id: string;
	session_title?: string;
	started_by: string;
	started_by_name: string;
	status: 'recording' | 'paused' | 'stopped';
	mime: string;
	bytes: number;
	chunks: number;
	viewers: 'teachers' | 'students';
	started_at: string;
	ended_at?: string;
	expires_at?: string;
	/** This person leads the session (or is the admin): download, who watches, delete. */
	lead: boolean;
};

/** "12 min", "1 h 05 min": how long a recording runs. */
export function recordingLength(r: Pick<Recording, 'started_at' | 'ended_at'>, now = Date.now()): string {
	const s = Math.max(0, Math.round(((r.ended_at ? Date.parse(r.ended_at) : now) - Date.parse(r.started_at)) / 1000));
	if (s < 60) return `${s} s`;
	const m = Math.round(s / 60);
	return m < 60 ? `${m} min` : `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, '0')} min`;
}

// ---------- the bandwidth budget (D-81) ----------

/** What a session will use (TS-FR-08). */
export type Plan = {
	students: number;
	screens: number;
	cameras: number;
	quality: 'readable' | 'smooth';
	student_mics: number;
	student_cameras: number;
	student_screens: number;
	group_video: boolean;
};

export type Estimate = { mbps: number; items: { name: string; mbps: number }[] };

export type Budget = {
	total_mbps: number;
	hard_cap_mbps: number;
	session_mbps: number;
	max_teacher_tracks: number;
	max_student_cameras: number;
	max_student_screens: number;
};

/** A plan, its forecast, and what's free for it now. */
export type PlanView = { plan: Plan; forecast: Estimate; free_mbps: number; hard_free_mbps: number; limit_mbps: number; budget: Budget; fits: boolean; advice?: string[] };

export type Preset = { id: string; name: string; plan: Plan };

export type AutoOffStep = 'group_camera' | 'group_screen' | 'student_shares' | 'extra_tracks' | 'camera' | 'fps';

/** What a session sends now, estimated, pushed to its teachers every 2 s (TS-FR-33). */
export type Resources = {
	load: Estimate & { viewers: number; tracks: number; screens: number; cameras: number; students_sharing: number };
	total_mbps: number;
	limit_mbps: number;
	budget_mbps: number;
	hard_cap_mbps: number;
	plan: Estimate;
	auto_off: AutoOffStep[];
};

/** A live session's use now: estimated, with its share of the measured CPU (D-82). */
export type LiveUse = {
	id: string;
	title: string;
	teacher_name: string;
	mbps: number;
	down_mbps: number;
	viewers: number;
	tracks: number;
	participants: number;
	reconnects: number;
	cpu?: number;
	auto_off: AutoOffStep[];
};

export type BudgetOverview = { budget: Budget; total_mbps: number; sessions: LiveUse[] };

/** The admin's monitoring (D-82, TS-NFR-41): now, and the last day or week. */
export type MonitorView = {
	total: {
		up_mbps: number;
		down_mbps: number;
		cpu?: number;
		tracks: number;
		participants: number;
		reconnects: number;
		measured?: { up_mbps: number; down_mbps: number; cpu: number; at: string };
		estimated_up_mbps: number;
	};
	sessions: LiveUse[];
	budget_mbps: number;
	range: '24h' | '7d';
	step_seconds: number;
	measuring: boolean;
	points: { at: string; up_mbps: number; down_mbps: number; cpu?: number; participants: number; reconnects: number; measured: boolean }[];
	history: {
		id: string;
		title: string;
		teacher_name: string;
		from: string;
		to: string;
		peak_up_mbps: number;
		avg_up_mbps: number;
		peak_down_mbps: number;
		peak_participants: number;
		peak_tracks: number;
		reconnects: number;
	}[];
};

/** Fills the gaps in a history with zeros, one point per step: no line is drawn across time nothing ran. */
export function fillGaps<T extends { at: string }>(points: T[], stepSeconds: number, zero: (at: string) => T): T[] {
	const out: T[] = [];
	const step = stepSeconds * 1000;
	for (const p of points) {
		const prev = out[out.length - 1];
		if (prev) {
			const gap = Math.round((Date.parse(p.at) - Date.parse(prev.at)) / step);
			// One zero just after the last point and one just before the next is enough.
			if (gap > 1) out.push(zero(new Date(Date.parse(prev.at) + step).toISOString()));
			if (gap > 2) out.push(zero(new Date(Date.parse(p.at) - step).toISOString()));
		}
		out.push(p);
	}
	return out;
}

/** What each switched-off step was, for "Turn back on". */
export const STEP_LABELS: Record<AutoOffStep, string> = {
	group_camera: 'Cameras in groups',
	group_screen: 'Sharing screens in groups',
	student_shares: 'More than two students sharing',
	extra_tracks: 'Extra screens and cameras',
	camera: 'The broadcaster’s camera',
	fps: 'Full frame rate for the screen'
};

export const DEFAULT_PLAN: Plan = { students: 30, screens: 1, cameras: 1, quality: 'readable', student_mics: 2, student_cameras: 2, student_screens: 2, group_video: false };

/** "12.4 Mbit/s"; small figures keep two decimals. */
export function mbps(n: number): string {
	return `${n < 10 ? n.toFixed(n < 1 ? 2 : 1) : Math.round(n)} Mbit/s`;
}

/** How full a meter is, and whether to warn (80 %) or alarm (over). */
export function meter(used: number, limit: number): { pct: number; level: 'ok' | 'warn' | 'over' } {
	if (limit <= 0) return { pct: used > 0 ? 100 : 0, level: used > 0 ? 'over' : 'ok' };
	const pct = Math.round((used / limit) * 100);
	return { pct: Math.min(100, pct), level: used > limit ? 'over' : pct >= 80 ? 'warn' : 'ok' };
}

/** Whether a refusal over the budget can be overridden ("…, or send it anyway"). */
export function insistable(message: string): boolean {
	return /\banyway$/.test(message);
}

/** When a recording is deleted, in words: "in 2 days", "in 5 hours". */
export function deletedIn(expiresAt: string | undefined, now = Date.now()): string {
	if (!expiresAt) return '';
	const h = Math.max(0, (Date.parse(expiresAt) - now) / 3_600_000);
	if (h >= 36) return `in ${Math.round(h / 24)} days`;
	if (h >= 1) return `in ${Math.round(h)} hours`;
	return 'within the hour';
}
