import { describe, expect, it } from 'vitest';
import { reportScopeOptions, scopedName, type ReportItem } from './classReport';

const item = (kind: ReportItem['kind'], id: string, title: string, topic: string, module: string): ReportItem =>
	({ kind, id, title, topic, module, topic_id: 't-' + topic, module_id: 'm-' + module } as ReportItem);

describe('reports of part of the classroom (D-160)', () => {
	it('offers modules, topics, items by kind and quiz runs', () => {
		const items = [item('offline', 'o1', 'Lab book', 'Lab', 'Practicals'), item('quiz', 'q1', 'Check-in', 'Plants', 'Term 1'), item('poll', 'p1', 'Vote', 'Plants', 'Term 1')];
		const sessions = [{ id: 's1', quiz_id: 'q1', title: 'Check-in', started_at: '2026-11-05T10:00:00Z', ended_at: null, students: 28 }];
		const got = reportScopeOptions(items, sessions, () => '5 Nov');
		expect(got.map((o) => [o.value, o.group])).toEqual([
			['all', ''],
			['module:m-Practicals', 'Modules'],
			['module:m-Term 1', 'Modules'],
			['topic:t-Lab', 'Topics'],
			['topic:t-Plants', 'Topics'],
			['item:quiz:q1', 'Quizzes'],
			['item:poll:p1', 'Polls'],
			['item:offline:o1', 'Offline assignments'],
			['session:s1', 'Quiz sessions (one run)']
		]);
		expect(got.at(-1)?.label).toBe('Check-in · 5 Nov · 28');
		expect(got[3].label).toBe('Lab (Practicals)');
	});

	it('names a scoped report after what it covers', () => {
		expect(scopedName('Science class report', { kind: 'topic', label: 'Plants' })).toBe('Science class report · Plants');
		expect(scopedName('Science class report', { kind: 'all', label: 'Science' })).toBe('Science class report');
		expect(scopedName('Science class report')).toBe('Science class report');
	});
});
