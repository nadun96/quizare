import { describe, expect, it } from 'vitest';
import { bandOptions, inBand, isFiltered, matches, newView, nextSort, numberCSV, numberSheet, shownNote, sortRows, type ViewCol } from './reportView';

type Row = { name: string; id: string; mark: number | null };
const rows: Row[] = [
	{ name: 'Ben', id: 'S10', mark: 40 },
	{ name: 'amaya', id: 'S2', mark: 90 },
	{ name: 'Chen', id: 'S1', mark: null },
	{ name: 'Dee', id: '', mark: 40 }
];
const cols: ViewCol<Row>[] = [
	{ key: 'student', label: 'Student', value: (r) => r.name },
	{ key: 'id', label: 'ID', value: (r) => r.id },
	{ key: 'mark', label: 'Mark', value: (r) => r.mark, numeric: true }
];
const names = (x: Row[]) => x.map((r) => r.name);

describe('sorting a report (D-154)', () => {
	it('sorts names without regard to case', () => {
		expect(names(sortRows(rows, cols, 'student', 'asc'))).toEqual(['amaya', 'Ben', 'Chen', 'Dee']);
		expect(names(sortRows(rows, cols, 'student', 'desc'))).toEqual(['Dee', 'Chen', 'Ben', 'amaya']);
	});
	it('sorts IDs as people read them, S2 before S10', () => {
		expect(names(sortRows(rows, cols, 'id', 'asc'))).toEqual(['Chen', 'amaya', 'Ben', 'Dee']);
	});
	it('sorts marks as numbers, and keeps blanks last both ways', () => {
		expect(names(sortRows(rows, cols, 'mark', 'desc'))).toEqual(['amaya', 'Ben', 'Dee', 'Chen']);
		expect(names(sortRows(rows, cols, 'mark', 'asc'))).toEqual(['Ben', 'Dee', 'amaya', 'Chen']);
	});
	it('keeps equal rows in the order they came, and doesn’t change the original', () => {
		const copy = [...rows];
		sortRows(rows, cols, 'mark', 'desc');
		expect(rows).toEqual(copy);
		expect(names(sortRows(rows, cols, 'nothing', 'asc'))).toEqual(names(rows));
	});
	it('a pressed column flips; a new one starts its natural way', () => {
		const v = newView();
		expect(nextSort(v, cols, 'student').dir).toBe('desc');
		expect(nextSort(v, cols, 'mark')).toMatchObject({ sort: 'mark', dir: 'desc' });
		expect(nextSort({ ...v, sort: 'mark', dir: 'desc' }, cols, 'id')).toMatchObject({ sort: 'id', dir: 'asc' });
	});
});

describe('filtering a report (D-154)', () => {
	it('finds every word typed, in any order or case', () => {
		expect(matches('Amaya Silva S12', 'silva amaya')).toBe(true);
		expect(matches('Amaya Silva S12', 's12')).toBe(true);
		expect(matches('Amaya Silva S12', 'amaya perera')).toBe(false);
		expect(matches('Amaya', '   ')).toBe(true);
	});
	const bands = [{ from: 75, color: '#111111' }, { from: 50, color: '#222222' }, { from: 0, color: '#333333' }];
	it('offers each colour range', () => {
		expect(bandOptions(bands, 'Final')).toEqual([
			{ v: 'band:75', label: 'Final: 75% and above' },
			{ v: 'band:50', label: 'Final: 50% to under 75%' },
			{ v: 'band:0', label: 'Final: Below 50%' }
		]);
	});
	it('puts a mark in exactly one range, and none without a mark', () => {
		expect([100, 75, 74.9, 50, 49.9, 0].map((p) => bands.filter((b) => inBand(p, `band:${b.from}`, bands)).map((b) => b.from))).toEqual([[75], [75], [50], [50], [0], [0]]);
		expect(inBand(null, 'band:0', bands)).toBe(false);
	});
	it('a range that’s since changed shows everyone rather than nobody', () => {
		expect(inBand(10, 'band:60', bands)).toBe(true);
	});
	it('knows when it’s filtered, and says so', () => {
		expect(isFiltered(newView())).toBe(false);
		expect(isFiltered({ ...newView(), sort: 'mark' })).toBe(false);
		expect(isFiltered({ ...newView(), q: 'a' })).toBe(true);
		expect(isFiltered({ ...newView(), show: 'missing' })).toBe(true);
		expect(isFiltered({ ...newView(), group: 'g' })).toBe(true);
		expect(shownNote(3, 12, 'students')).toBe('Showing 3 of 12 students');
		expect(shownNote(12, 12, 'students')).toBe('');
	});
});

describe('numbering a report (D-154)', () => {
	it('numbers a sheet’s rows and keeps colours in their columns', () => {
		const s = numberSheet({ head: ['Student', 'Mark'], rows: [['Ann', '80%'], ['Ben', '40%']], tones: [[null, '#111111'], [null, '#222222']], frozen: 2 });
		expect(s.head).toEqual(['No.', 'Student', 'Mark']);
		expect(s.rows).toEqual([[1, 'Ann', '80%'], [2, 'Ben', '40%']]);
		expect(s.tones?.[1]).toEqual([null, null, '#222222']);
		expect(s.frozen).toBe(3);
	});
	it('numbers only the students in a CSV, and lines the class figures up', () => {
		expect(numberCSV([['Student'], ['Ann'], ['Ben'], [], ['Class average', 5]], 2)).toEqual([['No.', 'Student'], [1, 'Ann'], [2, 'Ben'], [], ['', 'Class average', 5]]);
	});
});
