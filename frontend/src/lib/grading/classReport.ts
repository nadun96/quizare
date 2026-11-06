// The class report (D-140): the setup a teacher edits, and the report built
// from it, from /api/teacher/classrooms/{id}/class-report.
import { toCSV } from '../tutoring/types';
import type { Sheet } from './reportPdf';

export type Pick = 'high' | 'low';
export type Detail = '' | 'marks' | 'correctness' | 'answers';
export type Unit = 'minutes' | 'hours' | 'days';
export type GroupRule = 'average' | 'total' | 'highest' | 'lowest';

export type ReportItem = { kind: 'quiz' | 'tutorial' | 'poll'; group_rule: GroupRule; id: string; title: string; topic: string; module: string; included: boolean; weight: number; pick: Pick; detail: Detail; review_minutes: number; taken: number; avg: number | null; high: number | null; low: number | null; max_marks: number };
export type ReportMark = { score: number; max: number; pct: number; points?: number; submissions: number; pending?: boolean };
export type ReportStudent = { user_id: string; name: string; student_number?: string; marks: Record<string, ReportMark>; total: number; missing: number; final: number | null };
export type ResultException = { kind: string; id: string; user_id: string; detail: Detail; review_minutes: number | null };
export type GroupMark = { score: number; max: number; pct: number; points?: number; marked: number };
export type GroupRow = { id: string; name: string; members: string[]; marks: Record<string, GroupMark>; total: number; final: number | null };
export type ItemGroups = { kind: string; id: string; groups: GroupRow[] };
export type ClassReport = { classroom: string; items: ReportItem[]; students: ReportStudent[]; weight_total: number; out_of: number; exceptions: ResultException[]; class_groups: GroupRow[]; item_groups: ItemGroups[] };

export type DraftItem = { groupRule: GroupRule; included: boolean; weight: number | null; pick: Pick; detail: Detail; reviewN: number | null; reviewUnit: Unit };
export type DraftException = { item: string; user_id: string; detail: Detail; reviewN: number | null; reviewUnit: Unit };
export type Draft = { items: Record<string, DraftItem>; exceptions: DraftException[] };

export const itemKey = (it: { kind: string; id: string }) => `${it.kind}:${it.id}`;

export const DETAIL: { v: Detail; label: string; hint: string }[] = [
	{ v: '', label: 'Its own settings', hint: 'What the quiz or tutorial’s settings say' },
	{ v: 'marks', label: 'Marks only', hint: 'The score, no questions' },
	{ v: 'correctness', label: 'Marks and correctness', hint: 'Each question right or wrong, without the correct answer' },
	{ v: 'answers', label: 'Marks and correct answers', hint: 'Each question with its correct answer' }
];

export const GROUP_RULES: { v: GroupRule; label: string }[] = [
	{ v: 'average', label: 'Average' },
	{ v: 'total', label: 'Total' },
	{ v: 'highest', label: 'Highest' },
	{ v: 'lowest', label: 'Lowest' }
];

export const KIND_LABEL: Record<string, string> = { quiz: 'Quiz', tutorial: 'Tutorial', poll: 'Poll' };

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
		items[itemKey(it)] = { groupRule: it.group_rule, included: it.included, weight: it.weight, pick: it.pick, detail: it.detail, reviewN: p.n, reviewUnit: p.unit };
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
	return Math.round(sum * 10000) / 10000;
}

const splitKey = (k: string) => {
	const i = k.indexOf(':');
	return { kind: k.slice(0, i), id: k.slice(i + 1) };
};

/** What's wrong with the setup, or empty. */
export function draftProblem(d: Draft): string {
	for (const it of Object.values(d.items)) {
		if (it.weight != null && (it.weight < 0 || it.weight > 1)) return 'A weight must be between 0 and 1, such as 0.4.';
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
		items: Object.entries(d.items).map(([k, it]) => ({ ...splitKey(k), group_rule: it.groupRule, included: it.included, weight: it.weight ?? 0, pick: it.pick, detail: it.detail, review_minutes: it.reviewN ? toMinutes(it.reviewN, it.reviewUnit) : 0 })),
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
		if (it.weight > 0) head.push(`${it.title} (final mark, % × ${it.weight})`);
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
		row.push(s.total, r.out_of, s.final ?? '', s.missing);
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

/** The individual mark sheet, for printing and PDF. */
export function individualSheet(r: ClassReport): Sheet {
	const cols = columns(r);
	const head = ['Student', 'ID', ...cols.map((it) => `${it.title} (${weightText(it)})`), 'Total', 'Final'];
	const rows = r.students.map((s) => [s.name, s.student_number ?? '', ...cols.map((it) => markText(s.marks[itemKey(it)]) || '—'), `${s.total} / ${r.out_of}`, s.final == null ? '' : `${s.final}%`]);
	return { heading: 'Individual marks', head, rows, frozen: 2 };
}

/** Group names and who's in them, in the classroom's order. */
export const memberNames = (r: ClassReport, g: GroupRow) => {
	const names = new Map(r.students.map((s) => [s.user_id, s.name]));
	return g.members.map((u) => names.get(u) ?? '').filter(Boolean).join(', ');
};

/** "8 / 10 (80%)", for a group. */
export const groupMarkText = (m: GroupMark | undefined) => (m ? `${m.score} / ${m.max} (${m.pct}%)` : '');

const ruleText = (it: ReportItem) => GROUP_RULES.find((x) => x.v === it.group_rule)?.label ?? 'Average';

/** The students in no group of the class. */
export function ungrouped(r: ClassReport): string[] {
	const placed = new Set(r.class_groups.flatMap((g) => g.members));
	return r.students.filter((s) => !placed.has(s.user_id)).map((s) => s.name);
}

/** The group mark sheets: the class's, then each item's own groups. */
export function groupSheets(r: ClassReport): Sheet[] {
	const cols = columns(r);
	const sheets: Sheet[] = [];
	if (r.class_groups.length) {
		const head = ['Group', 'Members', ...cols.map((it) => `${it.title} (${weightText(it)}, ${ruleText(it).toLowerCase()})`), 'Total', 'Final'];
		const rows = r.class_groups.map((g) => [g.name, memberNames(r, g), ...cols.map((it) => groupMarkText(g.marks[itemKey(it)]) || '—'), `${g.total} / ${r.out_of}`, g.final == null ? '' : `${g.final}%`]);
		sheets.push({ heading: 'Marks by class group', head, rows, frozen: 1 });
	}
	for (const own of r.item_groups) {
		const it = r.items.find((x) => x.kind === own.kind && x.id === own.id);
		if (!it?.included) continue;
		const head = ['Group', 'Members', `${it.title} (${ruleText(it).toLowerCase()})`, ...(it.weight > 0 ? [`Final mark (% × ${it.weight})`] : [])];
		const rows = own.groups.map((g) => {
			const m = g.marks[itemKey(it)];
			return [g.name, memberNames(r, g), groupMarkText(m) || '—', ...(it.weight > 0 ? [m?.points ?? 0] : [])];
		});
		sheets.push({ heading: `Marks by ${it.title}’s own groups`, head, rows, frozen: 1 });
	}
	return sheets;
}

/** Group sheets as CSV, one after another. */
export function groupsCSV(r: ClassReport): string {
	const rows: (string | number)[][] = [];
	for (const sh of groupSheets(r)) {
		if (rows.length) rows.push([]);
		rows.push([sh.heading ?? '']);
		rows.push(sh.head);
		rows.push(...sh.rows);
	}
	return toCSV(rows);
}

/** Shares students out at random and evenly: sizes differ by at most one. */
export function splitIntoGroups(userIds: string[], count: number, random: () => number = Math.random): { name: string; members: string[] }[] {
	const n = Math.max(1, Math.floor(count));
	const shuffled = [...userIds];
	for (let i = shuffled.length - 1; i > 0; i--) {
		const j = Math.floor(random() * (i + 1));
		[shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
	}
	const groups = Array.from({ length: n }, (_, i) => ({ name: `Group ${i + 1}`, members: [] as string[] }));
	shuffled.forEach((u, i) => groups[i % n].members.push(u));
	return groups;
}

/** What's wrong with groups being edited, or empty. */
export function groupsProblem(groups: { name: string }[]): string {
	const seen = new Set<string>();
	for (const g of groups) {
		const name = g.name.trim().toLowerCase();
		if (!name) return 'Each group needs a name.';
		if (seen.has(name)) return `Two groups are called ${g.name.trim()}.`;
		seen.add(name);
	}
	return '';
}
