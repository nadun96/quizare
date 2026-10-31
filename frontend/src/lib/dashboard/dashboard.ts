// Each person's dashboard (D-99): the lists the server keeps, and how an
// item reads.
import type { IconName } from '../ui/Icon.svelte';
import { i18n, t, type Key } from '../i18n/i18n.svelte';

export type ListName = 'live' | 'upcoming' | 'overdue' | 'pending';
export type Item = {
	kind: 'quiz' | 'tutorial' | 'presentation' | 'meeting' | 'poll' | 'marking' | 'enrolment';
	id: string;
	title: string;
	classroom: string;
	at?: string;
	status: string;
	detail?: string;
	href: string;
};
export type Summary = { role: string; counts: Record<ListName, number> };

/** The lists, in the order they show, with the words for each role (D-109: keys, read with t()). */
export const LISTS: { id: ListName; title: Key; teacherTitle: Key; empty: Key; teacherEmpty: Key }[] = [
	{ id: 'live', title: 'list.live', teacherTitle: 'list.live', empty: 'list.liveEmpty', teacherEmpty: 'list.liveEmptyT' },
	{ id: 'overdue', title: 'list.overdue', teacherTitle: 'list.overdueT', empty: 'list.overdueEmpty', teacherEmpty: 'list.overdueEmptyT' },
	{ id: 'upcoming', title: 'list.upcoming', teacherTitle: 'list.upcoming', empty: 'list.upcomingEmpty', teacherEmpty: 'list.upcomingEmptyT' },
	{ id: 'pending', title: 'list.pending', teacherTitle: 'list.pendingT', empty: 'list.pendingEmpty', teacherEmpty: 'list.pendingEmptyT' }
];

export const KIND: Record<Item['kind'], { label: Key; icon: IconName }> = {
	quiz: { label: 'kind.quiz', icon: 'check-circle' },
	tutorial: { label: 'kind.tutorial', icon: 'book' },
	presentation: { label: 'kind.presentation', icon: 'monitor' },
	meeting: { label: 'kind.meeting', icon: 'video' },
	poll: { label: 'kind.poll', icon: 'chart' },
	marking: { label: 'kind.marking', icon: 'pencil' },
	enrolment: { label: 'kind.enrolment', icon: 'users' }
};

/** The words for an item's status and date: "Due tomorrow, 3:00 PM", "Missed on 28 Oct". */
export function when(it: Pick<Item, 'status' | 'at'>, now = Date.now(), locale: string = i18n.tag): string {
	const at = it.at ? new Date(it.at) : null;
	const day = (d: Date) => {
		const days = Math.round((startOfDay(d) - startOfDay(new Date(now))) / 86_400_000);
		const time = d.toLocaleTimeString(locale, { hour: 'numeric', minute: '2-digit' });
		if (days === 0) return t('when.today', { time });
		if (days === 1) return t('when.tomorrow', { time });
		if (days === -1) return t('when.yesterday', { time });
		return d.toLocaleDateString(locale, { day: 'numeric', month: 'short' });
	};
	switch (it.status) {
		case 'live':
			return t('when.live');
		case 'opens':
			return at ? t('when.opens', { day: day(at) }) : t('when.opensSoon');
		case 'due':
		case 'due_soon':
			return at ? t('when.due', { day: day(at) }) : t('when.dueSoon');
		case 'overdue':
			return at ? t('when.wasDue', { day: day(at) }) : t('when.overdue');
		case 'missed':
			return at ? t('when.missedEnded', { day: day(at) }) : t('when.missed');
		case 'in_progress':
			return t('when.started');
		case 'not_started':
			return t('when.notStarted');
		case 'to_mark':
			return t('when.toMark');
		case 'to_approve':
			return t('when.toApprove');
		default:
			return '';
	}
}

const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

/** Which colour an item's status wears. */
export function tone(status: string): 'live' | 'warn' | 'bad' | 'info' | 'plain' {
	if (status === 'live') return 'live';
	if (status === 'overdue' || status === 'missed') return 'bad';
	if (status === 'due_soon' || status === 'to_mark' || status === 'to_approve') return 'warn';
	if (status === 'opens' || status === 'due') return 'info';
	return 'plain';
}
