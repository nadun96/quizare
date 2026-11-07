import { describe, expect, it } from 'vitest';
import { attendanceDetailCSV, attendanceSummaryCSV, gradebookCSV, mergeTutoring, points, printHTML, summarise, weightTotal, type Attendance, type Gradebook } from './grading';

const g: Gradebook = {
	classroom: 'Physics',
	weight_total: 15,
	items: [
		{ kind: 'quiz', id: 'q1', title: 'Heat check', topic: 'Heat', weight: 10, taken: 2, avg: 77.5, high: 100, low: 55, max_marks: 10 },
		{ kind: 'quiz', id: 'q2', title: 'Light', topic: 'Light', weight: null, taken: 1, avg: 50 },
		{ kind: 'tutorial', id: 't1', title: 'Cells, part 1', topic: 'Cells', weight: 5, taken: 1, avg: 50 }
	],
	students: [
		{ user_id: 'a', name: 'Ann', student_number: 'S1', marks: { 'quiz:q1': { pct: 55, points: 5.5 }, 'quiz:q2': { pct: 50 } }, average: 52.5, total: 5.5, missing: 1, final: 36.7, marks_total: 11, marks_max: 20 },
		{ user_id: 'b', name: 'Ben', marks: {}, average: null, total: 0, missing: 2, final: 0, marks_total: 0, marks_max: 20 }
	]
};

const att: Attendance = {
	classroom: 'Physics',
	from: '2026-10-01',
	to: '2026-10-31',
	events: [
		{ kind: 'quiz', id: 's1', title: 'Heat check', at: '2026-10-02T09:00:00Z' },
		{ kind: 'meeting', id: 'm1', title: 'Revision', at: '2026-10-05T09:00:00Z' }
	],
	students: [
		{ user_id: 'a', name: 'Ann', student_number: 'S1', attended: ['s1', 'm1'], minutes: { m1: 40 } },
		{ user_id: 'b', name: 'Ben', attended: [], minutes: {} }
	]
};

// CSV lines end in CRLF, as spreadsheets expect.
const rows = (csv: string) => csv.split(/\r?\n/);

describe('reports as filtered and sorted (D-154)', () => {
	const att: Attendance = { classroom: 'S', from: '2026-10-01', to: '2026-10-31', events: [{ kind: 'quiz', id: 'e1', title: 'Quiz', at: '2026-10-02T10:00:00Z' }], students: [
		{ user_id: 'a', name: 'Ann', attended: ['e1'], minutes: {} },
		{ user_id: 'b', name: 'Ben', attended: [], minutes: {} }
	] };
	it('writes attendance in the order shown, leaving out who’s filtered away', () => {
		const sum = rows(attendanceSummaryCSV(att, summarise(att).reverse().slice(0, 1)));
		expect(sum[1].startsWith('1,Ben,')).toBe(true);
		expect(sum.join()).not.toContain('Ann');
		const d = rows(attendanceDetailCSV(att, ['b', 'a']));
		expect(d[1].startsWith('1,Ben,')).toBe(true);
		expect(d[2].startsWith('2,Ann,')).toBe(true);
		// The count of who came stays the whole class's.
		expect(rows(attendanceDetailCSV(att, ['b'])).find((l) => l.startsWith(',Present'))).toBe(',Present,,1');
	});
});

describe('gradebook and reports (D-122)', () => {
	it('turns a mark into points: 55 % on an item worth 10 % is 5.5', () => {
		expect(points(55, 10)).toBe(5.5);
		expect(points(100, 12.5)).toBe(12.5);
		expect(points(33.3, 15)).toBeCloseTo(5, 1);
	});

	it('lets the weights add up to less than 100, not more', () => {
		expect(weightTotal({ a: 10, b: 25, c: null })).toEqual({ total: 35, ok: true });
		expect(weightTotal({ a: 60, b: 50 })).toEqual({ total: 110, ok: false });
		expect(weightTotal({ a: -5 }).ok).toBe(false);
	});

	it('writes the results report with points, totals and the class rows', () => {
		const lines = rows(gradebookCSV(g).trim());
		expect(lines[0]).toBe('No.,Student,Student ID,Heat check (%),Heat check (points of 10),Light (%),"Cells, part 1 (%)","Cells, part 1 (points of 5)",Average (%),Total marks,Marks out of,Weighted total,Out of,Final (%),Weighted items missed');
		expect(lines[1]).toBe('1,Ann,S1,55,5.5,50,,0,52.5,11,20,5.5,15,36.7,1');
		expect(lines[2]).toBe('2,Ben,,,0,,,0,,0,20,0,15,0,2');
		// D-133: the class's totals, and each item's highest and lowest.
		expect(lines.find((l) => l.startsWith(',Class average'))).toBe(',Class average,,77.5,,50,50,,52.5,5.5');
		expect(lines.find((l) => l.startsWith(',Highest'))).toBe(',Highest,,100,,,,,52.5,11');
		expect(lines.find((l) => l.startsWith(',Lowest'))).toBe(',Lowest,,55,,,,,52.5,0');
		expect(lines.find((l) => l.startsWith(',Out of (marks)'))).toMatch(/^,Out of \(marks\),,10,/);
		expect(lines.at(-1)).toContain('Weight (% of the final result),,10,,not counted,5,');
	});

	it('adds attendance to the results when asked', () => {
		const lines = rows(gradebookCSV(g, new Map(summarise(att).map((s) => [s.user_id, s]))));
		expect(lines[0].endsWith('Sessions attended,Sessions,Attendance (%)')).toBe(true);
		expect(lines[1].endsWith(',2,2,100')).toBe(true);
	});

	it('merges tutoring sessions and summarises by kind', () => {
		const m = mergeTutoring(att, { sessions: [{ id: 't9', title: 'Live lesson', started_at: '2026-10-03T09:00:00Z', seconds: 3600 }], students: [{ user_id: 'b', name: 'Ben', attended: ['t9'], seconds: 1800 }], counts_in_results: false });
		expect(m.events.map((e) => e.id)).toEqual(['s1', 't9', 'm1']);
		const [ann, ben] = summarise(m);
		expect(ann).toMatchObject({ came: 2, of: 3, rate: 67, minutes: 40 });
		expect(ben).toMatchObject({ came: 1, of: 3, rate: 33, minutes: 30 });
		expect(ben.byKind.tutoring).toEqual({ came: 1, of: 1 });
	});

	it('writes the attendance summary and the session-by-session sheet', () => {
		expect(rows(attendanceSummaryCSV(att))[1]).toBe('1,Ann,S1,1/1,1/1,,2,2,100,40');
		const d = rows(attendanceDetailCSV(att));
		expect(d[0]).toBe('No.,Student,Student ID,Quiz: Heat check (2026-10-02),Meeting: Revision (2026-10-05)');
		expect(d[1]).toBe('1,Ann,S1,present,present (40 min)');
		expect(d[2]).toBe('2,Ben,,absent,absent');
		expect(d[3]).toBe(',Present,,1,1');
	});

	it('prints safely', () => {
		const h = printHTML('Results <Physics>', 'Oct', ['A'], [['<b>x</b>', 5]]);
		expect(h).toContain('Results &lt;Physics&gt;');
		expect(h).toContain('<td>&lt;b&gt;x&lt;/b&gt;</td><td class="n">5</td>');
	});
});
