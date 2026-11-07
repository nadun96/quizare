import { describe, expect, it } from 'vitest';
import { classReportCSV, markTone, draftFrom, draftProblem, draftWeightTotal, finalRange, groupSheets, groupsCSV, groupsProblem, individualSheet, reviewParts, setAllPicks, setupPayload, splitIntoGroups, toMinutes, ungrouped, weightText, type ClassReport, type ReportItem } from './classReport';

const item = (more: Partial<ReportItem>): ReportItem => ({ group_rule: 'average', kind: 'quiz', id: 'q1', title: 'Cells', topic: 'T', module: 'M', included: true, weight: 0.2, pick: 'high', detail: '', review_minutes: 0, taken: 1, avg: 80, high: 80, low: 80, max_marks: 10, ...more });
const report = (): ClassReport => ({
	classroom: 'Science',
	weight_total: 0.2,
	out_of: 20,
	items: [item({}), item({ kind: 'tutorial', id: 't1', title: 'Atoms', weight: 0, review_minutes: 2880, detail: 'marks' }), item({ id: 'q2', title: 'Left out', included: false })],
	students: [{ user_id: 'u1', name: 'Amaya', student_number: 'S1', marks: { 'quiz:q1': { score: 8, max: 10, pct: 80, points: 16, submissions: 2 }, 'tutorial:t1': { score: 1, max: 2, pct: 50, submissions: 1 } }, total: 16, missing: 0, final: 80 }],
	exceptions: [{ kind: 'quiz', id: 'q1', user_id: 'u1', detail: '', review_minutes: 90 }],
	class_groups: [],
	item_groups: []
});

describe('the class report (D-140)', () => {
	it('says a review time in the unit that fits', () => {
		expect(reviewParts(2880)).toEqual({ n: 2, unit: 'days' });
		expect(reviewParts(120)).toEqual({ n: 2, unit: 'hours' });
		expect(reviewParts(90)).toEqual({ n: 90, unit: 'minutes' });
		expect(reviewParts(null).n).toBeNull();
		expect(toMinutes(2, 'days')).toBe(2880);
		expect(toMinutes(1.5, 'hours')).toBe(90);
	});

	it('turns the setup into what the server takes, and back', () => {
		const d = draftFrom(report());
		expect(d.items['tutorial:t1'].reviewN).toBe(2);
		expect(d.items['tutorial:t1'].reviewUnit).toBe('days');
		const p = setupPayload(d);
		expect(p.items.find((x) => x.id === 't1')).toMatchObject({ kind: 'tutorial', weight: 0, detail: 'marks', review_minutes: 2880 });
		expect(p.items.find((x) => x.id === 'q2')?.included).toBe(false);
		expect(p.exceptions[0]).toEqual({ kind: 'quiz', id: 'q1', user_id: 'u1', detail: '', review_minutes: 90 });
	});

	it('sets highest or lowest on every quiz at once, and leaves tutorials', () => {
		const d = draftFrom(report());
		setAllPicks(d, 'low');
		expect(d.items['quiz:q1'].pick).toBe('low');
		expect(d.items['quiz:q2'].pick).toBe('low');
		expect(d.items['tutorial:t1'].pick).toBe('high');
	});

	it('totals only the weights of items in the report', () => {
		expect(draftWeightTotal(draftFrom(report()))).toBe(0.2);
	});

	it('catches a setup the server would refuse', () => {
		const d = draftFrom(report());
		expect(draftProblem(d)).toBe('');
		d.items['quiz:q1'].weight = -1;
		expect(draftProblem(d)).toMatch(/between 0 and 1,/);
		d.items['quiz:q1'].weight = 1.5;
		expect(draftProblem(d)).toMatch(/between 0 and 1,/);
		d.items['quiz:q1'].weight = 0.5;
		d.exceptions.push({ ...d.exceptions[0] });
		expect(draftProblem(d)).toMatch(/two exceptions/);
		d.exceptions.pop();
		d.exceptions[0].reviewN = null;
		expect(draftProblem(d)).toMatch(/must change/);
	});

	it('reports only what is included, with a weight 0 item shown but not given points', () => {
		const csv = classReportCSV(report());
		expect(csv).toContain('Cells (final mark, % × 0.2)');
		expect(csv).toContain('Atoms (%)');
		expect(csv).not.toContain('Atoms (final mark');
		expect(csv).not.toContain('Left out');
		const t = individualSheet(report());
		expect(t.head).toContain('Atoms (Not in the total)');
		expect(t.rows[0]).toEqual(['Amaya', 'S1', '8 / 10 (80%), final mark 16', '1 / 2 (50%)', '16 / 20', '80%']);
	});

	it('follows the product owner’s example on paper (D-145)', () => {
		const a = (id: string, weight: number) => item({ id, title: id, weight });
		const r: ClassReport = {
			...report(),
			weight_total: 0.4,
			out_of: 40,
			items: [a('A1', 0.1), a('A2', 0.2), a('A3', 0.1)],
			students: [{ user_id: 'u1', name: 'Student 1', marks: { 'quiz:A1': { score: 30, max: 40, pct: 75, points: 7.5, submissions: 1 }, 'quiz:A2': { score: 20, max: 20, pct: 100, points: 20, submissions: 1 }, 'quiz:A3': { score: 75, max: 100, pct: 75, points: 7.5, submissions: 1 } }, total: 35, missing: 0, final: 87.5 }]
		};
		const t = individualSheet(r);
		expect(t.head).toEqual(['Student', 'ID', 'A1 (Weight 0.1, 10%)', 'A2 (Weight 0.2, 20%)', 'A3 (Weight 0.1, 10%)', 'Total', 'Final']);
		expect(t.rows[0]).toEqual(['Student 1', '', '30 / 40 (75%), final mark 7.5', '20 / 20 (100%), final mark 20', '75 / 100 (75%), final mark 7.5', '35 / 40', '87.5%']);
	});

	it('says a weight as a percent without floating point noise', () => {
		expect(weightText({ weight: 0.07 })).toBe('Weight 0.07, 7%');
		expect(weightText({ weight: 0.125 })).toBe('Weight 0.125, 12.5%');
		expect(weightText({ weight: 0 })).toBe('Not in the total');
	});

	it('gives the class’s range of final results', () => {
		const r = report();
		r.students.push({ ...r.students[0], user_id: 'u2', final: 40 }, { ...r.students[0], user_id: 'u3', final: null });
		expect(finalRange(r)).toEqual({ avg: 60, high: 80, low: 40 });
	});
});

describe('groups on mark sheets (D-141)', () => {
	const withGroups = (): ClassReport => {
		const r = report();
		r.students.push({ ...r.students[0], user_id: 'u2', name: 'Ben', marks: {}, total: 0, final: 0 });
		r.class_groups = [{ id: 'g1', name: 'Group 1', members: ['u1'], marks: { 'quiz:q1': { score: 8, max: 10, pct: 80, points: 16, marked: 1 } }, total: 16, final: 80 }];
		r.item_groups = [{ kind: 'tutorial', id: 't1', groups: [{ id: 'g2', name: 'Red', members: ['u1', 'u2'], marks: { 'tutorial:t1': { score: 0.5, max: 2, pct: 25, marked: 1 } }, total: 0, final: null }] }];
		return r;
	};

	it('shares students out evenly, whatever the number', () => {
		const ids = Array.from({ length: 10 }, (_, i) => `u${i}`);
		const groups = splitIntoGroups(ids, 3, () => 0.5);
		expect(groups.map((g) => g.name)).toEqual(['Group 1', 'Group 2', 'Group 3']);
		expect(groups.map((g) => g.members.length)).toEqual([4, 3, 3]);
		expect(groups.flatMap((g) => g.members).sort()).toEqual([...ids].sort());
		expect(splitIntoGroups(['a'], 0).length).toBe(1);
	});

	it('builds a sheet for the class and one for an item’s own groups', () => {
		const sheets = groupSheets(withGroups());
		expect(sheets.map((s) => s.heading)).toEqual(['Marks by class group', 'Marks by Atoms’s own groups']);
		expect(sheets[0].rows[0]).toEqual(['Group 1', 'Amaya', '8 / 10 (80%), final mark 16', '—', '16 / 20', '80%']);
		expect(sheets[1].rows[0]).toEqual(['Red', 'Amaya, Ben', '0.5 / 2 (25%)']);
		expect(groupsCSV(withGroups())).toContain('Marks by class group');
	});

	it('leaves out an item that isn’t in the report, and lists who has no group', () => {
		const r = withGroups();
		r.items[1].included = false;
		expect(groupSheets(r).map((s) => s.heading)).toEqual(['Marks by class group']);
		expect(ungrouped(r)).toEqual(['Ben']);
	});

	it('catches a blank or repeated name', () => {
		expect(groupsProblem([{ name: 'A' }, { name: 'B' }])).toBe('');
		expect(groupsProblem([{ name: ' ' }])).toMatch(/needs a name/);
		expect(groupsProblem([{ name: 'A' }, { name: ' a ' }])).toMatch(/Two groups/);
	});
});

describe('colours on mark sheets (D-143)', () => {
	it('colours a mark by its height, and a missing one only if it counts', () => {
		expect(markTone({ pct: 80 }, 0.2)).toBe('#dcfce7');
		expect(markTone(undefined, 0.2)).toBe('missing');
		expect(markTone(undefined, 0)).toBeNull();
	});

	it('uses the classroom’s own bands', () => {
		const t = individualSheet(report(), [{ from: 90, color: '#111111' }, { from: 60, color: '#222222' }, { from: 0, color: '#333333' }]);
		expect(t.tones?.[0]).toEqual([null, null, '#222222', '#333333', null, '#222222']);
	});

	it('gives the individual sheet a colour for each cell', () => {
		const t = individualSheet(report());
		expect(t.tones?.[0]).toEqual([null, null, '#dcfce7', '#fef3c7', null, '#dcfce7']);
		expect(t.tones?.[0].length).toBe(t.head.length);
	});
});

const EOL = new RegExp(String.fromCharCode(13) + '?' + String.fromCharCode(10));

describe('what else the class report shows (D-156)', () => {
	const withExtras = (options: Partial<ClassReport['options'] & object>): ClassReport => {
		const r = report();
		r.items[0].date = '2026-03-04T10:00:00Z';
		r.options = { violations: false, attendance: false, dates: false, ...options };
		r.students[0].violations = { 'quiz:q1': 2 };
		r.students[0].violations_total = 2;
		return r;
	};
	const att = new Map([['u1', { user_id: 'u1', name: 'Amaya', number: 'S1', byKind: {} as never, came: 5, of: 6, rate: 83, minutes: 0 }]]);

	it('shows nothing extra unless asked', () => {
		const t = individualSheet(withExtras({}), undefined, att);
		expect(t.head).toEqual(['Student', 'ID', 'Cells (Weight 0.2, 20%)', 'Atoms (Not in the total)', 'Total', 'Final']);
		expect(t.rows[0].join()).not.toContain('violation');
		expect(classReportCSV(withExtras({}), undefined, att)).not.toContain('iolations');
	});
	it('adds violations to the marks and a column', () => {
		const t = individualSheet(withExtras({ violations: true }));
		expect(t.head.at(-1)).toBe('Violations');
		expect(t.rows[0][2]).toBe('8 / 10 (80%), final mark 16, 2 violations');
		expect(t.rows[0].at(-1)).toBe(2);
		expect(t.tones?.[0].length).toBe(t.head.length);
		const csv = classReportCSV(withExtras({ violations: true }));
		expect(csv).toContain('Cells (violations)');
		expect(csv).not.toContain('Atoms (violations)'); // only quizzes have them
	});
	it('adds attendance, in its colour', () => {
		const t = individualSheet(withExtras({ attendance: true }), undefined, att);
		expect(t.head.at(-1)).toBe('Attendance (last 12 months)');
		expect(t.rows[0].at(-1)).toBe('5 / 6 (83%)');
		expect(t.tones?.[0].at(-1)).toBe('#dcfce7');
		expect(classReportCSV(withExtras({ attendance: true }), undefined, att)).toContain('Attendance (%)');
	});
	it('adds each item’s date', () => {
		const t = individualSheet(withExtras({ dates: true }));
		expect(t.head[2]).toMatch(/^Cells \(Weight 0\.2, 20%, .*2026\)$/);
		expect(classReportCSV(withExtras({ dates: true }))).toContain('2026-03-04');
	});
	it('keeps the class rows lined up under their columns', () => {
		const lines = classReportCSV(withExtras({ violations: true, dates: true }), undefined, att).trim().split(EOL);
		const head = lines[0].split(',');
		const avg = lines.find((l) => l.startsWith(',Class average'))!.split(',');
		expect(avg[head.indexOf('Cells (%)')]).toBe('80');
	});
	it('sends the options when saved, and starts from what’s saved', () => {
		const d = draftFrom(withExtras({ dates: true }));
		expect(d.options).toEqual({ violations: false, attendance: false, dates: true });
		expect(setupPayload(d).options).toEqual(d.options);
		expect(draftFrom(report()).options).toEqual({ violations: false, attendance: false, dates: false });
	});
});

describe('the class report as filtered (D-154)', () => {
	it('writes only who’s shown, numbered, with the class figures whole', () => {
		const r = report();
		const other = { ...r.students[0], user_id: 'u2', name: 'Ben', final: 40 };
		r.students = [...r.students, other];
		const lines = classReportCSV(r, [other]).trim().split(EOL);
		expect(lines[0].startsWith('No.,Student,Student ID')).toBe(true);
		expect(lines[1].startsWith('1,Ben,')).toBe(true);
		expect(lines.join()).not.toContain('Amaya');
		expect(lines.find((l) => l.startsWith(',Highest'))?.endsWith(',80')).toBe(true); // Amaya's is still the class's highest
	});

	it('numbers each group sheet in a CSV', () => {
		const r = report();
		r.class_groups = [{ id: 'g1', name: 'Red', members: ['u1'], marks: {}, total: 0, final: null }];
		const lines = groupsCSV(r).trim().split(EOL);
		expect(lines[1].startsWith('No.,Group')).toBe(true);
		expect(lines[2].startsWith('1,Red')).toBe(true);
	});
});
