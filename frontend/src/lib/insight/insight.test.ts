import { describe, expect, it } from 'vitest';
import { dateLabel, DEFAULT, fmt, fromParams, pivot, range, toCSV, toParams, type Result } from './insight';

const today = new Date(Date.UTC(2026, 10, 2));

const res: Result = {
	columns: [
		{ key: 'classroom', label: 'Classroom', kind: 'dimension' },
		{ key: 'date', label: 'Date', kind: 'dimension' },
		{ key: 'avg_score', label: 'Average score', kind: 'measure', unit: '%' }
	],
	rows: [
		{ dims: { classroom: { id: 'a', label: 'Physics, 10A' }, date: { id: '2026-09-01', label: '2026-09-01' } }, values: { avg_score: 70 }, level: 0 },
		{ dims: { classroom: { id: 'a', label: 'Physics, 10A' }, date: { id: '2026-10-01', label: '2026-10-01' } }, values: { avg_score: 80 }, level: 0 },
		{ dims: { classroom: { id: 'b', label: 'Chemistry' }, date: { id: '2026-10-01', label: '2026-10-01' } }, values: { avg_score: null }, level: 0, hidden: true },
		{ dims: { classroom: { id: 'a', label: 'Physics, 10A' } }, values: { avg_score: 75 }, level: 1 },
		{ dims: null, values: { avg_score: 75 }, level: 2 }
	],
	truncated: false,
	from: '2026-01-01',
	to: '2026-11-02',
	grain: 'month',
	loaded_at: null
};

describe('Explore (D-120)', () => {
	it('keeps the query in the address', () => {
		const q = { ...DEFAULT, dimensions: ['classroom', 'date'], filters: { classroom: ['a', 'b'], kind: [] }, totals: false, from: '2026-01-01', to: '2026-03-31' };
		const back = fromParams(toParams(q), today);
		expect(back).toEqual({ ...q, filters: { classroom: ['a', 'b'] } });
		expect(fromParams(new URLSearchParams(), today)).toMatchObject({ measures: DEFAULT.measures, to: '2026-11-02', from: '2025-11-03' });
	});

	it('works out date ranges ending today', () => {
		expect(range('30d', today)).toEqual({ from: '2026-10-04', to: '2026-11-02' });
		expect(range('nonsense', today).from).toBe('2025-11-03');
	});

	it('names dates at the grain', () => {
		expect(dateLabel('2026-04-01', 'quarter')).toBe('Q2 2026');
		expect(dateLabel('2026-04-01', 'year')).toBe('2026');
		expect(dateLabel('2026-04-06', 'week')).toMatch(/^Week of /);
	});

	it('pivots two dimensions, leaving hidden and missing cells empty', () => {
		const p = pivot(res, 'avg_score');
		expect(p.rows.map((r) => r.id)).toEqual(['a', 'b']);
		expect(p.cols.map((c) => c.id)).toEqual(['2026-09-01', '2026-10-01']);
		expect(p.at('a', '2026-10-01')).toBe(80);
		expect(p.at('b', '2026-10-01')).toBeNull();
		expect(p.at('b', '2026-09-01')).toBeUndefined();
	});

	it('writes CSV with quoted text and "All" for totals', () => {
		const csv = toCSV(res).split('\n');
		expect(csv[0]).toBe('Classroom,Date,Average score (%)');
		expect(csv[1]).toMatch(/^"Physics, 10A",.*,70$/);
		expect(csv[3]).toMatch(/^Chemistry,.*,$/);
		expect(csv[5]).toBe('All,All,75');
	});

	it('formats values with their unit', () => {
		expect(fmt(62.5, '%')).toBe('62.5%');
		expect(fmt(3, 'min')).toBe('3 min');
		expect(fmt(null)).toBe('—');
	});
});
