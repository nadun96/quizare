// A student's one classroom (D-138): what's on, due and coming, and what
// they've completed, from /api/my/classrooms/{id}/work, with tutoring
// sessions, which the tutoring service knows about, added here.
import type { IconName } from '../ui/Icon.svelte';
import { canJoin, type CalendarEntry, type MyClassroomAttendance } from '../tutoring/types';

export type WorkItem = { kind: string; id: string; title: string; classroom: string; at?: string; status: string; detail?: string; href: string };
export type DoneItem = { kind: string; id: string; title: string; at?: string; status: string; released: boolean; score?: number; max?: number; pct?: number; passed?: boolean; pending?: boolean; tries?: number; href?: string };
export type ClassroomWork = {
	classroom_id: string;
	name: string;
	description: string;
	teacher: string;
	status: string;
	student_number: string | null;
	live: WorkItem[];
	due: WorkItem[];
	upcoming: WorkItem[];
	completed: DoneItem[];
};

export const KIND: Record<string, { label: string; icon: IconName }> = {
	quiz: { label: 'Quiz', icon: 'list-ordered' },
	tutorial: { label: 'Tutorial', icon: 'book' },
	presentation: { label: 'Presentation', icon: 'monitor' },
	meeting: { label: 'Meeting', icon: 'video' },
	poll: { label: 'Poll', icon: 'chart' },
	tutoring: { label: 'Tutoring', icon: 'users' }
};
export const kindOf = (k: string) => KIND[k] ?? { label: k, icon: 'list' as IconName };

export const STATUS: Record<string, { label: string; tone: string }> = {
	live: { label: 'On now', tone: 'badge-success' },
	overdue: { label: 'Overdue', tone: 'badge-error' },
	missed: { label: 'Missed', tone: 'badge-warning' },
	due_soon: { label: 'Due soon', tone: 'badge-warning' },
	due: { label: 'Due', tone: 'badge-info' },
	in_progress: { label: 'In progress', tone: 'badge-info' },
	not_started: { label: 'Not started', tone: 'badge-ghost' },
	opens: { label: 'Not open yet', tone: 'badge-ghost' },
	submitted: { label: 'Submitted', tone: 'badge-success' },
	finished: { label: 'Finished', tone: 'badge-success' },
	attended: { label: 'Attended', tone: 'badge-success' },
	invalidated: { label: 'Ended early', tone: 'badge-warning' }
};
export const statusOf = (s: string) => STATUS[s] ?? { label: s, tone: 'badge-ghost' };

const when = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });

/** "Due 5 Nov, 3:00 PM": what the date beside an item means. */
export function dateLine(it: { status: string; at?: string }): string {
	if (!it.at) return '';
	const d = when(it.at);
	switch (it.status) {
		case 'overdue': return `Was due ${d}`;
		case 'missed': return `Ended ${d}`;
		case 'due':
		case 'due_soon': return `Due ${d}`;
		case 'opens': return `Opens ${d}`;
		case 'live': return `Since ${d}`;
		default: return d;
	}
}

/** The button on an item: what following its link does. */
export function action(it: WorkItem): string {
	if (it.kind === 'quiz') return it.status === 'missed' ? '' : it.href.startsWith('/attempt/') ? 'Continue' : 'Join';
	if (it.status === 'live') return 'Join';
	return it.status === 'in_progress' ? 'Continue' : 'Open';
}

/** "4 / 4 (100%)", or why there's no mark to show. */
export function markLine(d: DoneItem): string {
	if (d.score != null && d.max != null) return `${d.score} / ${d.max}${d.pct != null ? ` (${d.pct}%)` : ''}`;
	if (d.pending) return 'Still being marked';
	const waiting = d.kind === 'quiz' ? d.status !== 'invalidated' : d.kind === 'tutorial' && !d.released;
	return waiting ? 'Marks not released yet' : '';
}

/** Tutoring sessions added: live or scheduled from the calendar, those attended from the attendance report. */
export function withTutoring(w: ClassroomWork, calendar: CalendarEntry[], attendance: MyClassroomAttendance[], now: number): ClassroomWork {
	const mine = calendar.filter((e) => e.classroom_id === w.classroom_id && e.status !== 'ended' && e.status !== 'cancelled');
	const item = (e: CalendarEntry, status: string, at?: string): WorkItem => ({ kind: 'tutoring', id: e.id, title: e.title, classroom: e.classroom_name, at, status, href: canJoin(e, now) ? '/tutor/' + e.join_code : '' });
	const live = mine.filter((e) => e.status === 'live' || canJoin(e, now)).map((e) => item(e, 'live', e.started_at ?? e.scheduled_at));
	const liveIds = new Set(live.map((x) => x.id));
	const coming = mine.filter((e) => !liveIds.has(e.id)).map((e) => item(e, 'opens', e.scheduled_at));
	const attended = (attendance.find((a) => a.classroom_id === w.classroom_id)?.recent ?? [])
		.filter((r) => r.attended)
		.map((r, i): DoneItem => ({ kind: 'tutoring', id: `tutoring-${i}`, title: r.title, at: r.started_at, status: 'attended', released: false }));
	const byDate = (a: { at?: string }, b: { at?: string }) => (a.at ?? '').localeCompare(b.at ?? '');
	return {
		...w,
		live: [...w.live, ...live],
		upcoming: [...w.upcoming, ...coming].sort(byDate),
		completed: [...w.completed, ...attended].sort((a, b) => byDate(b, a))
	};
}
