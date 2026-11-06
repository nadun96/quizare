// The class report (D-140): the setup a teacher edits, and the report built
// from it, from /api/teacher/classrooms/{id}/class-report.
import { toCSV } from '../tutoring/types';

export type Pick = 'high' | 'low';
export type Detail = '' | 'marks' | 'correctness' | 'answers';
export type Unit = 'minutes' | 'hours' | 'days';

export type ReportItem = { kind: 'quiz' | 'tutorial'; id: string; title: string; topic: string; module: string; included: boolean; weight: number; pick: Pick; detail: Detail; review_minutes: number; taken: number; avg: number | null; high: number | null; low: number | null; max_marks: number };
export type ReportMark = { score: number; max: number; pct: number; points?: number; submissions: number; pending?: boolean };
export type ReportStudent = { user_id: string; name: string; student_number?: string; marks: Record<string, ReportMark>; total: number; missing: number; final: number | null };
export type ResultException = { kind: string; id: string; user_id: string; detail: Detail; review_minutes: number | null };
export type ClassReport = { classroom: string; items: ReportItem[]; students: ReportStudent[]; weight_total: number; exceptions: ResultException[] };

export type DraftItem = { included: boolean; weight: number | null; pick: Pick; detail: Detail; reviewN: number | null; reviewUnit: Unit };
export type DraftException = { item: string; user_id: string; detail: Detail; reviewN: number | null; reviewUnit: Unit };
export type Draft = { items: Record<string, DraftItem>; exceptions: DraftException[] };

export const itemKey = (it: { kind: string; id: string }) => `${it.kind}:${it.id}`;

export const DETAIL: { v: Detail; label: string; hint: string }[] = [
	{ v: '', label: 'Its own settings', hint: 'What the quiz or tutorial’s settings say' },
	{ v: 'marks', label: 'Marks only', hint: 'The score, no questions' },
	{ v: 'correctness', label: 'Marks and correctness', hint: 'Each question right or wrong, without the correct answer' },
	{ v: 'answers', label: 'Marks and correct answers', hint: 'Each question with its correct answer' }
];

/** The largest unit that says a number of minutes exactly: 2880 is 2 days. */
export function reviewParts(minutes: number | null | undefined): { n: number | null; unit: Unit } {
	if (minutes == null) return { n: null, unit: 'hours' };
	if (minutes === 0) return { n: 0, unit: 'hours' };
	if (minutes % 1440 === 0) return { n: minutes / 1440, unit: 'days' };
	if (minutes % 60 === 0) return { n: minutes / 60, unit: 'hours' };
	return { n: minutes, unit: 'minutes' };
}

export const toMinutes = (n: number, unit: Unit) => Math.round(n * (unit === 'days' ? 1440 : unit === 'hours' ? 60 : 1));

export function draftFrom(r: ClassReport): Draft {
	const items: Record<string, DraftItem> = {};
	for (const it of r.items) {
		const p = reviewParts(it.review_minutes || null);
		items[itemKey(it)] = { included: it.included, weight: it.weight, pick: it.pick, detail: it.detail, reviewN: p.n, reviewUnit: p.unit };
	}
	const exceptions = r.exceptions.map((x): DraftException => {
		const p = reviewParts(x.review_minutes);
		return { item: itemKey(x), user_id: x.user_id, detail: x.detail, reviewN: p.n, reviewUnit: p.unit };
	});
	return { items, exceptions };
}

/** Sets every quiz to the highest or lowest mark; a tutorial has one submission. */
export function setAllPicks(d: Draft, pick: Pick) {
	for (const k of Object.keys(d.items)) if (k.startsWith('quiz:')) d.items[k].pick = pick;
}

/** What's counted in the total: the weights of the items in the report. */
export function draftWeightTotal(d: Draft): number {
	const sum = Object.values(d.items).reduce((a, it) => a + (it.included && it.weight && it.weight > 0 ? it.weight : 0), 0);
	return Math.round(sum * 100) / 100;
}

const splitKey = (k: string) => {
	const i = k.indexOf(':');
	return { kind: k.slice(0, i), id: k.slice(i + 1) };
};

/** What's wrong with the setup, or empty. */
export function draftProblem(d: Draft): string {
	for (const it of Object.values(d.items)) {
		if (it.weight != null && (it.weight < 0 || it.weight > 1000)) return 'A weight must be between 0 and 1000.';
		if (it.reviewN != null && it.reviewN < 0) return 'A review time can’t be negative.';
	}
	const seen = new Set<string>();
	for (const x of d.exceptions) {
		if (!x.item || !x.user_id) return 'Choose the item and the student for each exception.';
		if (x.detail === '' && x.reviewN == null) return 'An exception must change what the student sees or their review time.';
		const k = x.item + '|' + x.user_id;
		if (seen.has(k)) return 'A student has two exceptions for the same item.';
		seen.add(k);
	}
	return '';
}

export function setupPayload(d: Draft) {
	return {
		items: Object.entries(d.items).map(([k, it]) => ({ ...splitKey(k), included: it.included, weight: it.weight ?? 0, pick: it.pick, detail: it.detail, review_minutes: it.reviewN ? toMinutes(it.reviewN, it.reviewUnit) : 0 })),
		exceptions: d.exceptions.map((x) => ({ ...splitKey(x.item), user_id: x.user_id, detail: x.detail, review_minutes: x.reviewN == null ? null : toMinutes(x.reviewN, x.reviewUnit) }))
	};
}

export const columns = (r: ClassReport) => r.items.filter((i) => i.included);

/** "8 / 10 (80%)", or nothing for no mark. */
export function markText(m: ReportMark | undefined): string {
	return m ? `${m.score} / ${m.max} (${m.pct}%)` : '';
}

/** What a column's weight means for the total. */
export const weightText = (it: { weight: number }) => (it.weight > 0 ? `Weight ${it.weight}` : 'Not in the total');

/** The final percentages' class average, highest and lowest. */
export function finalRange(r: ClassReport): { avg: number | null; high: number | null; low: number | null } {
	const f = r.students.flatMap((s) => (s.final == null ? [] : [s.final]));
	if (!f.length) return { avg: null, high: null, low: null };
	return { avg: Math.round((f.reduce((a, b) => a + b, 0) / f.length) * 10) / 10, high: Math.max(...f), low: Math.min(...f) };
}

export function classReportCSV(r: ClassReport): string {
	const cols = columns(r);
	const head = ['Student', 'Student ID'];
	for (const it of cols) {
		head.push(`${it.title} (marks)`, `${it.title} (out of)`, `${it.title} (%)`);
		if (it.weight > 0) head.push(`${it.title} (points of ${it.weight})`);
	}
	head.push('Total', 'Out of', 'Final (%)', 'Counted items missed');
	const rows: (string | number)[][] = [head];
	for (const s of r.students) {
		const row: (string | number)[] = [s.name, s.student_number ?? ''];
		for (const it of cols) {
			const m = s.marks[itemKey(it)];
			row.push(m?.score ?? '', m?.max ?? '', m?.pct ?? '');
			if (it.weight > 0) row.push(m?.points ?? 0);
		}
		row.push(s.total, r.weight_total, s.final ?? '', s.missing);
		rows.push(row);
	}
	const per = (f: (it: ReportItem) => string | number) => cols.flatMap((it) => ['', '', f(it), ...(it.weight > 0 ? [''] : [])]);
	const range = finalRange(r);
	rows.push([]);
	rows.push(['Class average (%)', '', ...per((it) => it.avg ?? ''), '', '', range.avg ?? '']);
	rows.push(['Highest (%)', '', ...per((it) => it.high ?? ''), '', '', range.high ?? '']);
	rows.push(['Lowest (%)', '', ...per((it) => it.low ?? ''), '', '', range.low ?? '']);
	rows.push(['Weight', '', ...per((it) => it.weight)]);
	rows.push(['Mark used', '', ...per((it) => (it.kind === 'tutorial' ? 'only one' : it.pick === 'low' ? 'lowest' : 'highest'))]);
	return toCSV(rows);
}

/** The report as one table to print. */
export function printTable(r: ClassReport): { head: string[]; rows: (string | number)[][] } {
	const cols = columns(r);
	const head = ['Student', 'ID', ...cols.map((it) => `${it.title} · ${weightText(it)}`), 'Total', 'Final'];
	const rows = r.students.map((s) => [s.name, s.student_number ?? '', ...cols.map((it) => markText(s.marks[itemKey(it)]) || '—'), `${s.total} / ${r.weight_total}`, s.final == null ? '' : `${s.final}%`]);
	return { head, rows };
}
