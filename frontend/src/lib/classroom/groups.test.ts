import { describe, expect, it } from 'vitest';
import { byRole, joinBlock, policyState, shareOut, ungrouped, type ClassGroup, type GroupPolicy, type MyGroup, type MyGroupsView } from './groups';

const person = (user_id: string, name: string, role: 'member' | 'leader' | 'assistant' = 'member') => ({ user_id, name, role });
const group = (id: string, ...people: ReturnType<typeof person>[]): ClassGroup => ({ id, classroom_id: 'c', name: id, color: 1, position: 0, members: people.length, public_name: null, people });
const policy = (p: Partial<GroupPolicy> = {}): GroupPolicy => ({ self_join: true, opens_at: null, closes_at: null, max_per_student: 1, max_size: null, open: true, ...p });

describe('classroom groups (D-158)', () => {
	it('puts leaders first, then assistants, then members by name', () => {
		const got = byRole([person('1', 'Zoe'), person('2', 'Amal', 'assistant'), person('3', 'Bo', 'leader'), person('4', 'Ann')]);
		expect(got.map((p) => p.name)).toEqual(['Bo', 'Amal', 'Ann', 'Zoe']);
	});

	it('finds students in no group', () => {
		const students = [person('1', 'A'), person('2', 'B'), person('3', 'C')];
		expect(ungrouped(students, [group('g', person('2', 'B'))]).map((s) => s.user_id)).toEqual(['1', '3']);
	});

	it('shares students out to even the groups', () => {
		const got = shareOut([{ id: 'a', size: 3 }, { id: 'b', size: 0 }, { id: 'c', size: 1 }], ['1', '2', '3', '4', '5'], () => 0);
		const sizes = { a: 3 + got.get('a')!.length, b: got.get('b')!.length, c: 1 + got.get('c')!.length };
		expect(sizes).toEqual({ a: 3, b: 3, c: 3 });
		expect([...got.values()].flat().sort()).toEqual(['1', '2', '3', '4', '5']);
		expect(shareOut([], ['1']).size).toBe(0);
	});

	it('says whether choosing is open', () => {
		const now = new Date('2026-11-08T10:00:00Z');
		expect(policyState(policy({ self_join: false, open: false }), now)).toBe('off');
		expect(policyState(policy(), now)).toBe('open');
		expect(policyState(policy({ open: false, opens_at: '2026-11-09T00:00:00Z' }), now)).toBe('soon');
		expect(policyState(policy({ open: false, closes_at: '2026-11-07T00:00:00Z' }), now)).toBe('closed');
	});

	it('says why a student can’t choose a group', () => {
		const g = (x: Partial<MyGroup> = {}): MyGroup => ({ id: 'g', name: 'G', teacher_name: 'G', public_name: null, color: 1, size: 0, mine: false, full: false, ...x });
		const v = (p: Partial<GroupPolicy> = {}, chosen = 0): MyGroupsView => ({ groups: [], policy: policy(p), chosen });
		expect(joinBlock(g(), v())).toBe('');
		expect(joinBlock(g({ mine: true }), v())).toBe('in');
		expect(joinBlock(g(), v({ open: false }))).toBe('closed');
		expect(joinBlock(g({ full: true }), v())).toBe('full');
		expect(joinBlock(g(), v({}, 1))).toBe('limit');
		expect(joinBlock(g(), v({ max_per_student: 2 }, 1))).toBe('');
	});
});
