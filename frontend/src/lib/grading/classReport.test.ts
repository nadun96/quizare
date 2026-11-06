import { describe, expect, it } from 'vitest';
import { classReportCSV, markTone, draftFrom, draftProblem, draftWeightTotal, finalRange, groupSheets, groupsCSV, groupsProblem, individualSheet, reviewParts, setAllPicks, setupPayload, splitIntoGroups, toMinutes, ungrouped, type ClassReport, type ReportItem } from './classReport';

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
		expect(t.rows[0]).toEqual(['Amaya', 'S1', '8 / 10 (80%)', '1 / 2 (50%)', '16 / 20', '80%']);
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
		expect(sheets[0].rows[0]).toEqual(['Group 1', 'Amaya', '8 / 10 (80%)', '—', '16 / 20', '80%']);
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
		expect(markTone({ pct: 80 }, 0.2)).toBe('high');
		expect(markTone(undefined, 0.2)).toBe('missing');
		expect(markTone(undefined, 0)).toBeNull();
	});

	it('gives the individual sheet a colour for each cell', () => {
		const t = individualSheet(report());
		expect(t.tones?.[0]).toEqual([null, null, 'high', 'middle', null, 'high']);
		expect(t.tones?.[0].length).toBe(t.head.length);
	});
});

