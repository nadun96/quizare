import { describe, expect, it } from 'vitest';
import { attendanceScopes, scopedAttendance, scopedGradebook, scopeFrom, scopeLabel, scopeOptions, scopeValue, type Attendance, type Gradebook } from './grading';

// Two modules: Physics (topics Heat, Light) and Biology (Cells).
const g: Gradebook = {
	classroom: 'Science',
	weight_total: 40,
	items: [
		{ kind: 'quiz', id: 'q1', title: 'Heat check', topic: 'Heat', topic_id: 'heat', module_id: 'phy', module: 'Physics', weight: 10, taken: 2, avg: 77.5 },
		{ kind: 'quiz', id: 'q2', title: 'Light quiz', topic: 'Light', topic_id: 'light', module_id: 'phy', module: 'Physics', weight: 20, taken: 1, avg: 50 },
		{ kind: 'tutorial', id: 't1', title: 'Cells, part 1', topic: 'Cells', topic_id: 'cells', module_id: 'bio', module: 'Biology', weight: 10, taken: 1, avg: 80 }
	],
	students: [
		{ user_id: 'a', name: 'Ann', marks: { 'quiz:q1': { pct: 50, points: 5 }, 'quiz:q2': { pct: 100, points: 20 }, 'tutorial:t1': { pct: 80, points: 8 } }, average: 76.7, total: 33, missing: 0, final: 82.5 },
		{ user_id: 'b', name: 'Ben', marks: { 'quiz:q1': { pct: 100, points: 10 } }, average: 100, total: 10, missing: 2, final: 25 }
	],
	sessions: [
		{ id: 's1', quiz_id: 'q1', title: 'Heat check', status: 'ended', at: '2026-10-02T09:00:00Z', attempts: 2, results: 2, avg: 75 },
		{ id: 's2', quiz_id: 'q2', title: 'Light quiz', status: 'ended', at: '2026-10-09T09:00:00Z', attempts: 1, results: 1, avg: 100 }
	]
};

describe('reports of part of a classroom (D-130)', () => {
	it('offers the modules, topics and each quiz or tutorial', () => {
		const o = scopeOptions(g.items);
		expect(o.map((x) => x.value)).toEqual(['all', 'module:phy', 'module:bio', 'topic:heat', 'topic:light', 'topic:cells', 'item:quiz:q1', 'item:quiz:q2', 'item:tutorial:t1']);
		expect(o.find((x) => x.value === 'topic:heat')?.label).toBe('Heat (Physics)');
		expect(scopeFrom('item:quiz:q1')).toEqual({ kind: 'item', id: 'quiz:q1' });
		expect(scopeFrom('nonsense')).toEqual({ kind: 'all' });
		expect(scopeValue(scopeFrom('module:phy'))).toBe('module:phy');
		expect(scopeLabel(g.items, { kind: 'item', id: 'tutorial:t1' })).toBe('Cells, part 1');
	});

	it('works the results out again for a module', () => {
		const p = scopedGradebook(g, { kind: 'module', id: 'phy' });
		expect(p.items.map((i) => i.id)).toEqual(['q1', 'q2']);
		expect(p.weight_total).toBe(30);
		const [ann, ben] = p.students;
		expect(ann).toMatchObject({ total: 25, final: 83.3, average: 75, missing: 0 });
		expect(Object.keys(ann.marks)).toEqual(['quiz:q1', 'quiz:q2']);
		expect(ben).toMatchObject({ total: 10, final: 33.3, average: 100, missing: 1 });
		expect(p.sessions?.map((x) => x.id)).toEqual(['s1', 's2']);
	});

	it('narrows to one topic or one tutorial', () => {
		const heat = scopedGradebook(g, { kind: 'topic', id: 'heat' });
		expect(heat.students[1]).toMatchObject({ total: 10, final: 100 });
		expect(heat.sessions?.map((x) => x.id)).toEqual(['s1']);
		const cells = scopedGradebook(g, { kind: 'item', id: 'tutorial:t1' });
		expect(cells.items).toHaveLength(1);
		expect(cells.students[1]).toMatchObject({ average: null, final: 0, missing: 1 });
		expect(cells.sessions).toEqual([]);
		expect(scopedGradebook(g, { kind: 'all' })).toBe(g);
	});

	it('keeps attendance to the scope’s quiz sessions', () => {
		const a: Attendance = {
			classroom: 'Science', from: '2026-10-01', to: '2026-10-31',
			events: [
				{ kind: 'quiz', id: 's1', title: 'Heat check', at: '2026-10-02T09:00:00Z', quiz_id: 'q1', topic_id: 'heat', topic: 'Heat', module_id: 'phy', module: 'Physics' },
				{ kind: 'quiz', id: 's3', title: 'Cell check', at: '2026-10-03T09:00:00Z', quiz_id: 'q3', topic_id: 'cells', topic: 'Cells', module_id: 'bio', module: 'Biology' },
				{ kind: 'meeting', id: 'm1', title: 'Revision', at: '2026-10-05T09:00:00Z' }
			],
			students: [{ user_id: 'a', name: 'Ann', attended: ['s1', 's3', 'm1'], minutes: { m1: 40 } }]
		};
		const phy = scopedAttendance(a, { kind: 'module', id: 'phy' });
		expect(phy.events.map((e) => e.id)).toEqual(['s1']);
		expect(phy.students[0].attended).toEqual(['s1']);
		expect(scopedAttendance(a, { kind: 'all' }).events).toHaveLength(3);
		expect(attendanceScopes(a).map((o) => o.value)).toEqual(['all', 'module:phy', 'module:bio', 'topic:heat', 'topic:cells', 'item:quiz:q1', 'item:quiz:q3']);
	});
});
