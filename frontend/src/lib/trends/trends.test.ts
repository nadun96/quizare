import { describe, expect, it } from 'vitest';
import { direction, fromFirst, monthLabel, points, summary, type TrendMonth } from './trends';

const m = (month: string, quizzes: number, avg: number | null, extra: Partial<TrendMonth> = {}): TrendMonth => ({ month, active: 0, quizzes, avg, pass_rate: avg === null ? null : 50, tutorials: 0, ...extra });

describe('long-term trends (D-119)', () => {
	it('weights averages by results and taking part by members', () => {
		const s = summary([m('2026-01', 1, 100, { enrolled: 10, active: 2 }), m('2026-02', 3, 40, { enrolled: 10, active: 8, tutorials: 2 }), m('2026-03', 0, null, { enrolled: 10, active: 0 })]);
		expect(s).toMatchObject({ results: 4, avg: 55, passRate: 50, tutorials: 2, takingPart: 33, activeMonths: 2 });
		expect(summary([])).toMatchObject({ results: 0, avg: null, passRate: null, takingPart: null });
	});

	it('calls a change of five points or more a direction', () => {
		expect(direction(70, 60)).toBe('up');
		expect(direction(60, 65)).toBe('down');
		expect(direction(62, 60)).toBe('steady');
		expect(direction(null, 60)).toBeNull();
	});

	it('makes points only for months with a value', () => {
		const p = points([m('2026-01', 1, 80, { class_avg: 70 }), m('2026-02', 0, null)], (x) => x.avg, (x) => x.class_avg, (x) => `${x.quizzes} results`);
		expect(p).toHaveLength(1);
		expect(p[0]).toMatchObject({ value: 80, context: 70, note: '1 results' });
		expect(p[0].label).toContain('2026');
	});

	it('starts from the first month with anything in it, keeping a few', () => {
		const ms = ['01', '02', '03', '04', '05', '06', '07', '08', '09', '10', '11', '12'].map((x) => m('2026-' + x, 0, null));
		ms[9] = m('2026-10', 2, 50);
		expect(fromFirst(ms).map((x) => x.month)[0]).toBe('2026-07'); // still six months shown
		ms[1] = m('2026-02', 1, 50);
		expect(fromFirst(ms)[0].month).toBe('2026-02');
		expect(fromFirst(ms.map((x) => ({ ...x, quizzes: 0 })), 3)).toHaveLength(3);
	});

	it('names months', () => {
		expect(monthLabel('2026-03')).toMatch(/2026/);
		expect(monthLabel('2026-03', true)).not.toMatch(/2026/);
	});
});
