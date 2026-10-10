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
	scheduled_at?: string;
	created_at: string;
	started_at?: string;
	ended_at?: string;
};

export type TutoringParticipant = {
	user_id: string;
	name: string;
	role: 'teacher' | 'student';
	state: 'waiting' | 'admitted' | 'refused' | 'removed';
	allow_mic: boolean;
	allow_camera: boolean;
	allow_screen: boolean;
	chat_muted: boolean;
	hand_at?: string;
	first_at: string;
	online: boolean;
};

export type ChatMessage = {
	id: number;
	user_id: string;
	name: string;
	from_teacher: boolean;
	audience: 'everyone' | 'teachers' | 'one';
	to_user?: string;
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
};

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
	if (m.audience === 'teachers') return m.user_id === meId ? 'to the teacher' : 'question to teachers';
	if (m.audience === 'one') return m.to_user === meId ? 'private, to you' : 'private';
	return '';
}
