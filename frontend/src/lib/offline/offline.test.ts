import { describe, expect, it } from 'vitest';
import { changed, parseMark, progress, readable, sheetUrl } from './offline';

describe('offline assignments (D-159)', () => {
	it('reads typed marks', () => {
		expect(parseMark('', 20)).toEqual({ value: null, error: '' });
		expect(parseMark(' 12,5 ', 20)).toEqual({ value: 12.5, error: '' });
		expect(parseMark('21', 20).error).toBe('0 to 20');
		expect(parseMark('-1', 20).error).toBe('0 to 20');
		expect(parseMark('abc', 20).error).toBe('Not a number');
		expect(parseMark('1.234', 20).error).toBe('At most 2 decimal places');
	});

	it('finds the rows that changed', () => {
		const saved = [
			{ id: 'a', score: 10, comment: 'ok' },
			{ id: 'b', score: null, comment: '' },
			{ id: 'c', score: 5, comment: '' },
			{ id: 'd', score: 7, comment: '' }
		];
		const { changes, errors } = changed(saved, {
			a: { mark: '10', comment: ' ok ' }, // the same
			b: { mark: '8', comment: 'new' },
			c: { mark: '', comment: 'x' }, // cleared
			d: { mark: '99', comment: '' }
		}, 20);
		expect(changes).toEqual([{ id: 'b', score: 8, comment: 'new' }, { id: 'c', score: null, comment: '' }]);
		expect(errors).toEqual({ d: '0 to 20' });
	});

	it('says how far marking has got, and where files come from', () => {
		expect(progress({ marked: 3, students: 30 })).toBe('3 of 30 marked');
		expect(sheetUrl('x', 'csv', 'groups')).toBe('/api/teacher/offline-assignments/x/sheet?format=csv&sheet=groups');
		expect(sheetUrl('x', 'xlsx')).toBe('/api/teacher/offline-assignments/x/sheet?format=xlsx');
		expect(readable('Marks.XLSX')).toBe(true);
		expect(readable('marks.xls')).toBe(false);
	});
});
