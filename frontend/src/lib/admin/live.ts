// The admin's live activity (D-90): what's live across the platform, and
// the pauses on new activities.

export type ActivityKind = 'quiz_session' | 'poll' | 'meeting' | 'presentation' | 'tutoring';

export type Activity = {
	kind: ActivityKind;
	id: string;
	title: string;
	status: string;
	teacher_id: string;
	teacher_name: string;
	teacher_email: string;
	classroom: string;
	started_at: string;
	people: number;
};

export type Pause = {
	id: string;
	teacher_id: string | null;
	teacher_name?: string;
	teacher_email?: string;
	reason: string;
	created_at: string;
};

export const KINDS: { id: ActivityKind; label: string; people: [string, string] }[] = [
	{ id: 'quiz_session', label: 'Quiz session', people: ['student', 'students'] },
	{ id: 'poll', label: 'Poll', people: ['participant', 'participants'] },
	{ id: 'meeting', label: 'Meeting', people: ['in it now', 'in it now'] },
	{ id: 'presentation', label: 'Live presentation', people: ['watching now', 'watching now'] },
	{ id: 'tutoring', label: 'Tutoring session', people: ['in it now', 'in it now'] }
];

/** A tutoring session, from the tutoring service (admins only), as an activity. */
export type TutoringLive = { id: string; title: string; status: string; teacher_id: string; teacher_name: string; classroom_name: string; started_at: string; people: number };
export const fromTutoring = (t: TutoringLive): Activity => ({
	kind: 'tutoring', id: t.id, title: t.title, status: t.status, teacher_id: t.teacher_id, teacher_name: t.teacher_name,
	teacher_email: '', classroom: t.classroom_name, started_at: t.started_at, people: t.people
});

export const kindLabel = (k: string) => KINDS.find((x) => x.id === k)?.label ?? k;

/** "1 student", "3 participants", "2 in it now". */
export function peopleText(k: string, n: number): string {
	const p = KINDS.find((x) => x.id === k)?.people ?? ['', ''];
	return `${n} ${n === 1 ? p[0] : p[1]}`.trim();
}

/** "4 min", "2 h 5 min", "3 days": how long something has been live. */
export function liveFor(startedAt: string, now = Date.now()): string {
	const min = Math.max(0, Math.floor((now - new Date(startedAt).getTime()) / 60000));
	if (min < 60) return `${min} min`;
	const h = Math.floor(min / 60);
	if (h < 48) return min % 60 ? `${h} h ${min % 60} min` : `${h} h`;
	return `${Math.floor(h / 24)} days`;
}

/** How many of each kind are live, and the people in them. */
export function summary(list: Activity[]): { kind: ActivityKind; label: string; count: number; people: number }[] {
	return KINDS.map((k) => {
		const of = list.filter((a) => a.kind === k.id);
		return { kind: k.id, label: k.label, count: of.length, people: of.reduce((n, a) => n + a.people, 0) };
	});
}

/** The pause for everyone, if there is one. */
export const everyonePause = (pauses: Pause[]) => pauses.find((p) => p.teacher_id === null) ?? null;
