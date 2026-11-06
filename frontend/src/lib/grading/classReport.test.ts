import { describe, expect, it } from 'vitest';
import { classReportCSV, draftFrom, draftProblem, draftWeightTotal, finalRange, printTable, reviewParts, setAllPicks, setupPayload, toMinutes, type ClassReport, type ReportItem } from './classReport';

const item = (more: Partial<ReportItem>): ReportItem => ({ kind: 'quiz', id: 'q1', title: 'Cells', topic: 'T', module: 'M', included: true, weight: 20, pick: 'high', detail: '', review_minutes: 0, taken: 1, avg: 80, high: 80, low: 80, max_marks: 10, ...more });
const report = (): ClassReport => ({
	classroom: 'Science',
	weight_total: 20,
	items: [item({}), item({ kind: 'tutorial', id: 't1', title: 'Atoms', weight: 0, review_minutes: 2880, detail: 'marks' }), item({ id: 'q2', title: 'Left out', included: false })],
	students: [{ user_id: 'u1', name: 'Amaya', student_number: 'S1', marks: { 'quiz:q1': { score: 8, max: 10, pct: 80, points: 16, submissions: 2 }, 'tutorial:t1': { score: 1, max: 2, pct: 50, submissions: 1 } }, total: 16, missing: 0, final: 80 }],
	exceptions: [{ kind: 'quiz', id: 'q1', user_id: 'u1', detail: '', review_minutes: 90 }]
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
		expect(draftWeightTotal(draftFrom(report()))).toBe(20);
	});

	it('catches a setup the server would refuse', () => {
		const d = draftFrom(report());
		expect(draftProblem(d)).toBe('');
		d.items['quiz:q1'].weight = -1;
		expect(draftProblem(d)).toMatch(/between 0 and 1000/);
		d.items['quiz:q1'].weight = 5;
		d.exceptions.push({ ...d.exceptions[0] });
		expect(draftProblem(d)).toMatch(/two exceptions/);
		d.exceptions.pop();
		d.exceptions[0].reviewN = null;
		expect(draftProblem(d)).toMatch(/must change/);
	});

	it('reports only what is included, with a weight 0 item shown but not given points', () => {
		const csv = classReportCSV(report());
		expect(csv).toContain('Cells (points of 20)');
		expect(csv).toContain('Atoms (%)');
		expect(csv).not.toContain('Atoms (points');
		expect(csv).not.toContain('Left out');
		const t = printTable(report());
		expect(t.head).toContain('Atoms · Not in the total');
		expect(t.rows[0]).toEqual(['Amaya', 'S1', '8 / 10 (80%)', '1 / 2 (50%)', '16 / 20', '80%']);
	});

	it('gives the class’s range of final results', () => {
		const r = report();
		r.students.push({ ...r.students[0], user_id: 'u2', final: 40 }, { ...r.students[0], user_id: 'u3', final: null });
		expect(finalRange(r)).toEqual({ avg: 60, high: 80, low: 40 });
	});
});
