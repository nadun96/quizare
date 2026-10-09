import { describe, expect, it } from 'vitest';
import { can, canActOn, consoleTabs, FEATURES, fmtBytes, isStaff, roleLabel, withImplied } from './staff';

// Managers in the admin console (D-56): what each person is shown. The
// server enforces the same rules on every request.
const admin = { id: 'a', role: 'admin' };
const teacher = { id: 't', role: 'teacher' };
const student = { id: 's', role: 'student' };
const mgr = (features: string[], role = 'teacher', id = 'm') => ({ id, role, manager: { features } });

describe('managers', () => {
	it('admins can do everything; managers only what they were given', () => {
		for (const f of FEATURES) expect(can(admin, f.id)).toBe(true);
		const m = mgr(['usage']);
		expect(can(m, 'usage')).toBe(true);
		expect(can(m, 'view_users')).toBe(false);
		expect(can(teacher, 'usage')).toBe(false);
		expect(can(null, 'usage')).toBe(false);
	});

	it('a manager with no features is still staff, a plain teacher is not', () => {
		expect(isStaff(mgr([]))).toBe(true);
		expect(isStaff(mgr([], 'manager'))).toBe(true);
		expect(isStaff(teacher)).toBe(false);
		expect(isStaff(admin)).toBe(true);
	});

	it('shows each person only their tabs; managers never get the managers page', () => {
		expect(consoleTabs(admin)).toEqual(['users', 'usage', 'settings', 'audit', 'storage', 'backups', 'managers']);
		expect(consoleTabs(mgr(['cleanup']))).toEqual(['storage']);
		expect(consoleTabs(mgr(['backups']))).toEqual(['backups']);
		expect(consoleTabs(mgr([]))).toEqual([]);
		expect(consoleTabs(mgr(['audit', 'view_users']))).toEqual(['users', 'audit']);
		expect(consoleTabs(mgr(['approval_policy']))).toEqual(['settings']);
		expect(consoleTabs(teacher)).toEqual([]);
	});

	it('managers act only on teachers and students who are not managers (PL-FR-13)', () => {
		const m = mgr(['view_users', 'manage_users']);
		expect(canActOn(m, teacher)).toBe(true);
		expect(canActOn(m, student)).toBe(true);
		expect(canActOn(m, admin)).toBe(false);
		expect(canActOn(m, mgr([], 'teacher', 'other'))).toBe(false);
		expect(canActOn(m, mgr([], 'manager', 'other'))).toBe(false);
		expect(canActOn(m, m)).toBe(false);
		expect(canActOn(mgr(['view_users']), teacher)).toBe(false);
		expect(canActOn(admin, { id: 'b', role: 'admin' })).toBe(true);
		expect(canActOn(admin, admin)).toBe(false);
	});

	it('"Manage users" brings "View users", in the offered order', () => {
		expect(withImplied(['audit', 'manage_users'])).toEqual(['view_users', 'manage_users', 'audit']);
		expect(withImplied([])).toEqual([]);
	});

	it('labels teacher-managers in user lists', () => {
		expect(roleLabel(mgr([]))).toBe('Teacher · manager');
		expect(roleLabel(mgr([], 'manager'))).toBe('Manager');
		expect(roleLabel(admin)).toBe('Admin');
	});

	it('warns before giving backups (PO-28)', () => {
		expect(FEATURES.find((f) => f.id === 'backups')?.warn).toMatch(/all the platform's data/);
		expect(FEATURES.filter((f) => f.warn).map((f) => f.id)).toEqual(['backups']);
	});

	it('formats sizes', () => {
		expect(fmtBytes(512)).toBe('512 B');
		expect(fmtBytes(1536)).toBe('1.5 KB');
		expect(fmtBytes(5 * 1024 ** 3)).toBe('5.0 GB');
		expect(fmtBytes(250 * 1024 ** 2)).toBe('250 MB');
	});
});
