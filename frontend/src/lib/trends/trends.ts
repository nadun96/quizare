// Long-term trends (D-119): month-by-month figures for a classroom, a
// student in it, or a student's own, read from the server's roll-ups.

export type TrendMonth = {
	month: string; // 2026-03
	enrolled?: number;
	active: number;
	taking_part?: number | null;
	sessions?: number;
	quizzes: number;
	avg: number | null;
	pass_rate: number | null;
	tutorials: number;
	class_avg?: number | null;
};
export type TrendStudent = {
	user_id: string;
	name: string;
	student_number?: string;
	quizzes: number;
	avg: number | null;
	tutorials: number;
	active_months: number;
	recent_avg: number | null;
	earlier_avg: number | null;
	last_active?: string;
};
export type ClassroomTrends = { classroom: string; months: TrendMonth[]; students: TrendStudent[]; updated_at: string | null };
export type StudentTrends = { name: string; classroom?: string; months: TrendMonth[]; updated_at: string | null };

export const SPANS = [
	{ months: 12, label: '12 months' },
	{ months: 24, label: '2 years' },
	{ months: 60, label: '5 years' }
] as const;

/** "Mar 2026", or "Mar" when short. */
export function monthLabel(m: string, short = false): string {
	const [y, mo] = m.split('-').map(Number);
	const d = new Date(Date.UTC(y, mo - 1, 1));
	return d.toLocaleDateString(undefined, short ? { month: 'short', timeZone: 'UTC' } : { month: 'short', year: 'numeric', timeZone: 'UTC' });
}

const round1 = (n: number) => Math.round(n * 10) / 10;

/** Totals over the months shown: averages are weighted by results, taking part by members. */
export function summary(ms: TrendMonth[]) {
	let results = 0,
		sum = 0,
		passed = 0,
		tutorials = 0,
		members = 0,
		active = 0,
		sessions = 0;
	for (const m of ms) {
		results += m.quizzes;
		tutorials += m.tutorials;
		sessions += m.sessions ?? 0;
		if (m.avg !== null) sum += m.avg * m.quizzes;
		if (m.pass_rate !== null) passed += (m.pass_rate / 100) * m.quizzes;
		if (m.enrolled) {
			members += m.enrolled;
			active += m.active;
		}
	}
	return {
		results,
		tutorials,
		sessions,
		avg: results ? round1(sum / results) : null,
		passRate: results ? Math.round((passed / results) * 100) : null,
		takingPart: members ? Math.min(100, Math.round((active / members) * 100)) : null,
		activeMonths: ms.filter((m) => m.quizzes + m.tutorials > 0).length
	};
}

/** Up, down or steady: the last three months' average against the three before, by at least 5 points. */
export function direction(recent: number | null, earlier: number | null): 'up' | 'down' | 'steady' | null {
	if (recent === null || earlier === null) return null;
	const d = recent - earlier;
	return d >= 5 ? 'up' : d <= -5 ? 'down' : 'steady';
}

/** The months with results, as points for a line: the value, and a comparison beside it. */
export function points(ms: TrendMonth[], value: (m: TrendMonth) => number | null | undefined, context: (m: TrendMonth) => number | null | undefined, note: (m: TrendMonth) => string) {
	return ms
		.filter((m) => value(m) !== null && value(m) !== undefined)
		.map((m) => ({ label: monthLabel(m.month), sub: monthLabel(m.month, true), value: value(m) as number, context: context(m) ?? null, note: note(m) }));
}

/** Since the first month with anything in it: a new classroom doesn't show years of empty months. */
export function fromFirst(ms: TrendMonth[], keep = 6): TrendMonth[] {
	const first = ms.findIndex((m) => m.quizzes + m.tutorials > 0 || (m.enrolled ?? 0) > 0);
	if (first < 0) return ms.slice(-keep);
	return ms.slice(Math.min(first, Math.max(0, ms.length - keep)));
}
