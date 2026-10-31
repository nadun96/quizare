// Each person's dashboard (D-99): the lists the server keeps, and how an
// item reads.
import type { IconName } from '../ui/Icon.svelte';

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

/** The lists, in the order they show, with what each is called for each role. */
export const LISTS: { id: ListName; title: string; teacherTitle: string; empty: string; teacherEmpty: string }[] = [
	{ id: 'live', title: 'Live now', teacherTitle: 'Live now', empty: 'Nothing is live in your classes right now.', teacherEmpty: 'None of your sessions, polls, meetings or presentations is live.' },
	{ id: 'overdue', title: 'Overdue and missed', teacherTitle: 'Past due, not everyone finished', empty: 'Nothing overdue. Well done.', teacherEmpty: 'No past-due work with students still to finish.' },
	{ id: 'upcoming', title: 'Coming up', teacherTitle: 'Coming up', empty: 'Nothing with a date coming up.', teacherEmpty: 'None of your tutorials or presentations opens or is due soon.' },
	{ id: 'pending', title: 'To do', teacherTitle: 'Waiting on you', empty: 'Nothing waiting. Tutorials and presentations without a due date show here.', teacherEmpty: 'Nothing to mark or approve.' }
];

export const KIND: Record<Item['kind'], { label: string; icon: IconName }> = {
	quiz: { label: 'Quiz', icon: 'check-circle' },
	tutorial: { label: 'Tutorial', icon: 'book' },
	presentation: { label: 'Presentation', icon: 'monitor' },
	meeting: { label: 'Meeting', icon: 'video' },
	poll: { label: 'Poll', icon: 'chart' },
	marking: { label: 'Marking', icon: 'pencil' },
	enrolment: { label: 'Joining', icon: 'users' }
};

/** The words for an item's status and date: "Due tomorrow, 3:00 PM", "Missed on 28 Oct". */
export function when(it: Pick<Item, 'status' | 'at'>, now = Date.now(), locale?: string): string {
	const at = it.at ? new Date(it.at) : null;
	const day = (d: Date) => {
		const days = Math.round((startOfDay(d) - startOfDay(new Date(now))) / 86_400_000);
		const time = d.toLocaleTimeString(locale, { hour: 'numeric', minute: '2-digit' });
		if (days === 0) return `today, ${time}`;
		if (days === 1) return `tomorrow, ${time}`;
		if (days === -1) return `yesterday, ${time}`;
		return d.toLocaleDateString(locale, { day: 'numeric', month: 'short' });
	};
	switch (it.status) {
		case 'live':
			return 'Live now';
		case 'opens':
			return at ? `Opens ${day(at)}` : 'Opens soon';
		case 'due':
		case 'due_soon':
			return at ? `Due ${day(at)}` : 'Due soon';
		case 'overdue':
			return at ? `Was due ${day(at)}` : 'Overdue';
		case 'missed':
			return at ? `Missed, ended ${day(at)}` : 'Missed';
		case 'in_progress':
			return 'Started';
		case 'not_started':
			return 'Not started';
		case 'to_mark':
			return 'To mark';
		case 'to_approve':
			return 'To approve';
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
