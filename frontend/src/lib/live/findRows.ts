// Finding students on a live session's dashboard (D-155).
import { matches } from '../grading/reportView';
import type { DashboardRow } from '../types';

export type RowShow = '' | 'waiting' | 'admitted' | 'in_progress' | 'paused' | 'submitted' | 'flagged' | 'offline';

export const ROW_SHOW: { v: RowShow; label: string }[] = [
	{ v: '', label: 'Everyone' },
	{ v: 'waiting', label: 'Waiting' },
	{ v: 'admitted', label: 'Admitted' },
	{ v: 'in_progress', label: 'In progress' },
	{ v: 'paused', label: 'Paused' },
	{ v: 'submitted', label: 'Finished' },
	{ v: 'flagged', label: 'Flagged' },
	{ v: 'offline', label: 'Offline' }
];

const isFlagged = (r: DashboardRow) => r.violations > 0 || r.state === 'invalidated';

function fits(r: DashboardRow, show: RowShow): boolean {
	switch (show) {
		case '':
			return true;
		case 'flagged':
			return isFlagged(r);
		case 'submitted':
			return r.state === 'submitted' || r.state === 'invalidated';
		case 'offline':
			return !r.connected && (r.state === 'in_progress' || r.state === 'waiting');
		default:
			return r.state === show;
	}
}

/** The rows that fit, by name, student ID or team, in the order they came. */
export function findRows(rows: DashboardRow[], q: string, show: RowShow, teamOf: (r: DashboardRow) => string = () => ''): DashboardRow[] {
	return rows.filter((r) => fits(r, show) && matches(`${r.name} ${r.student_number ?? ''} ${teamOf(r)}`, q));
}

/** How many of the selected the list isn't showing: commands still reach them. */
export function hiddenSelected(selected: Set<string>, shown: DashboardRow[]): number {
	const on = new Set(shown.map((r) => r.attempt_id));
	return [...selected].filter((id) => !on.has(id)).length;
}
