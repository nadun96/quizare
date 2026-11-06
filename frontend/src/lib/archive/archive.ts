// Finished activities and the archive (D-149). Quiz sessions, polls, meetings
// and live presentations come from the platform, tutoring sessions from their
// own service; both follow the same rules.
import type { IconName } from '../ui/Icon.svelte';

export type Kind = 'quiz' | 'poll' | 'meeting' | 'presentation' | 'tutoring';
export type View = 'past' | 'archived';

export type ArchiveItem = {
	kind: Exclude<Kind, 'tutoring'>;
	id: string;
	title: string;
	detail: string;
	classroom_id: string | null;
	classroom: string;
	started_at: string;
	ended_at: string;
	archived_at: string | null;
	people: number | null;
	href: string;
};

export const KINDS: { v: Kind; label: string; icon: IconName }[] = [
	{ v: 'quiz', label: 'Quiz session', icon: 'list-ordered' },
	{ v: 'poll', label: 'Poll', icon: 'chart' },
	{ v: 'meeting', label: 'Meeting', icon: 'video' },
	{ v: 'presentation', label: 'Live presentation', icon: 'monitor' },
	{ v: 'tutoring', label: 'Tutoring session', icon: 'users' }
];
export const kindInfo = (k: string) => KINDS.find((x) => x.v === k) ?? KINDS[0];

/** What goes with it for good: said before anything is deleted. */
const LOST: Record<Kind, string> = {
	quiz: 'Students’ attempts and marks for this session go too, so they leave Results, the Class report and Attendance.',
	poll: 'Its answers and any marks go too, so they leave the Class report.',
	meeting: 'Who attended goes too, so it leaves Attendance.',
	presentation: 'The record of this live session goes. Time on slides stays with the presentation.',
	tutoring: 'Its chat, attendance and recordings go too.'
};

export function deleteDialog(kind: Kind, title: string) {
	return { title: `Delete “${title}” for good?`, body: `${LOST[kind]} This can’t be undone.`, confirm: 'Delete for good', danger: true };
}

/** A tutoring session's list row comes with its own fields. */
export type TutoringRow = { id: string; title: string; classroom_id: string; classroom_name: string; status: string; started_at?: string | null; scheduled_at?: string | null; created_at: string; ended_at?: string | null; cancelled_at?: string | null };

/** The tutoring session as an item of the list. */
export function fromTutoring(s: TutoringRow): { kind: 'tutoring'; id: string; title: string; detail: string; classroom: string; started_at: string; ended_at: string; people: null; href: string } {
	const started = s.started_at ?? s.scheduled_at ?? s.created_at;
	return { kind: 'tutoring', id: s.id, title: s.title, detail: s.status === 'cancelled' ? 'Cancelled' : '', classroom: s.classroom_name, started_at: started, ended_at: s.ended_at ?? s.cancelled_at ?? started, people: null, href: '' };
}
