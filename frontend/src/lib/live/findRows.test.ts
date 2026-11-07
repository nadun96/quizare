import { describe, expect, it } from 'vitest';
import type { DashboardRow } from '../types';
import { findRows, hiddenSelected } from './findRows';

const row = (attempt_id: string, name: string, more: Partial<DashboardRow> = {}) =>
	({ attempt_id, name, user_id: 'u' + attempt_id, student_number: null, state: 'in_progress', index: 0, total: 4, answered: 1, warnings: 0, violations: 0, extension_sec: 0, quiz_deadline: null, remaining_ms: null, connected: true, ...more }) as DashboardRow;
const rows = [
	row('a', 'G W L A Gunarathna', { student_number: 'S12', violations: 7, state: 'submitted' }),
	row('b', 'Harshi Dilmika', { student_number: 'S7', state: 'waiting', connected: false }),
	row('c', 'Prageeth Gnanasri', { state: 'invalidated' }),
	row('d', 'Upani Hewawasam', { team_id: 't1' })
];
const names = (x: DashboardRow[]) => x.map((r) => r.attempt_id).join('');

describe('finding students on the dashboard (D-155)', () => {
	it('finds by name, in any case and word order', () => {
		expect(names(findRows(rows, 'harshi', ''))).toBe('b');
		expect(names(findRows(rows, 'dilmika HARSHI', ''))).toBe('b');
		expect(names(findRows(rows, 'g', ''))).toBe('ac');
		expect(names(findRows(rows, 'nobody', ''))).toBe('');
	});
	it('finds by student ID and team', () => {
		expect(names(findRows(rows, 's12', ''))).toBe('a');
		expect(names(findRows(rows, 'red', '', (r) => (r.team_id ? 'Red' : '')))).toBe('d');
	});
	it('keeps everyone with no search, in their order', () => {
		expect(names(findRows(rows, '  ', ''))).toBe('abcd');
	});
	it('narrows by what’s happening, with the search', () => {
		expect(names(findRows(rows, '', 'flagged'))).toBe('ac');
		expect(names(findRows(rows, '', 'submitted'))).toBe('ac'); // an invalidated attempt has finished too
		expect(names(findRows(rows, '', 'waiting'))).toBe('b');
		expect(names(findRows(rows, '', 'offline'))).toBe('b');
		expect(names(findRows(rows, 'prageeth', 'flagged'))).toBe('c');
		expect(names(findRows(rows, 'harshi', 'flagged'))).toBe('');
	});
	it('counts the selected the list is hiding', () => {
		expect(hiddenSelected(new Set(['a', 'b']), findRows(rows, 'harshi', ''))).toBe(1);
		expect(hiddenSelected(new Set(['b']), findRows(rows, 'harshi', ''))).toBe(0);
	});
});
