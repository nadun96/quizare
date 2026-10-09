// Admins and managers (PL-FR-10 to PL-FR-17, D-56). The server checks every
// feature on every request; these helpers only decide what to show.

export type Feature = 'view_users' | 'manage_users' | 'approval_policy' | 'settings' | 'usage' | 'audit' | 'storage' | 'cleanup' | 'backups';

/** The features an admin can give a manager, in the order they're offered. */
export const FEATURES: { id: Feature; label: string; hint: string; warn?: string }[] = [
	{ id: 'view_users', label: 'View users', hint: 'List and search users and see their status.' },
	{ id: 'manage_users', label: 'Manage users', hint: 'Approve teachers, and activate, suspend or delete teachers and students. Never admins or managers. Includes viewing users.' },
	{ id: 'approval_policy', label: 'Teacher approval policy', hint: 'Choose whether new teachers need approval.' },
	{ id: 'settings', label: 'Platform settings', hint: "Change the platform's default settings." },
	{ id: 'usage', label: 'Usage', hint: "See the platform's usage counts." },
	{ id: 'audit', label: 'Audit log', hint: 'Read the audit log.' },
	{ id: 'storage', label: 'Storage', hint: 'See the space the platform uses and set the recording and backup limits.' },
	{ id: 'cleanup', label: 'Clean-up', hint: 'Delete old uploaded files and old backups to free space.' },
	{
		id: 'backups',
		label: 'Backups',
		hint: 'Export and download full backups, and see the backup history.',
		// PO-28: never given by default, and the admin is warned.
		warn: "A backup holds all the platform's data: every account, answer, result and uploaded file. Give this only to someone you'd trust with all of it."
	}
];

export const featureLabel = (f: string) => FEATURES.find((x) => x.id === f)?.label ?? f;

type Person = { id?: string; role: string; manager?: { features: string[] } | null };

export const isAdmin = (u: Person | null | undefined) => u?.role === 'admin';
export const isStaff = (u: Person | null | undefined) => !!u && (u.role === 'admin' || !!u.manager);
export const isManager = (u: Person | null | undefined) => !!u?.manager;

/** Whether u may use a feature: admins always, managers only when given it. */
export function can(u: Person | null | undefined, f: Feature): boolean {
	if (!u) return false;
	return u.role === 'admin' || !!u.manager?.features.includes(f);
}

/**
 * Whether viewer may approve, suspend or delete target: never themself; an
 * admin may act on anyone else, a manager with "Manage users" only on
 * teachers and students who aren't managers (PL-FR-13, PL-FR-14).
 */
export function canActOn(viewer: Person | null | undefined, target: Person): boolean {
	if (!viewer || viewer.id === target.id || !can(viewer, 'manage_users')) return false;
	if (viewer.role === 'admin') return true;
	return (target.role === 'teacher' || target.role === 'student') && !target.manager;
}

/** Ticking "Manage users" brings "View users"; the server does the same. */
export function withImplied(features: Feature[]): Feature[] {
	const set = new Set(features);
	if (set.has('manage_users')) set.add('view_users');
	return FEATURES.map((f) => f.id).filter((f) => set.has(f));
}

/** "Teacher · manager", "Manager", "Admin"… for user lists. */
export function roleLabel(u: Person): string {
	const r = u.role.charAt(0).toUpperCase() + u.role.slice(1);
	return u.manager && u.role !== 'manager' ? r + ' · manager' : r;
}

export type ConsoleTab = 'users' | 'usage' | 'settings' | 'audit' | 'storage' | 'backups' | 'managers';

/** The admin console's tabs this person can open, in order. */
export function consoleTabs(u: Person | null | undefined): ConsoleTab[] {
	const tabs: ConsoleTab[] = [];
	if (can(u, 'view_users')) tabs.push('users');
	if (can(u, 'usage')) tabs.push('usage');
	if (can(u, 'approval_policy') || can(u, 'settings')) tabs.push('settings');
	if (can(u, 'audit')) tabs.push('audit');
	if (can(u, 'storage') || can(u, 'cleanup')) tabs.push('storage');
	if (can(u, 'backups')) tabs.push('backups');
	if (isAdmin(u)) tabs.push('managers');
	return tabs;
}

/** "1.5 GB": sizes as people read them (binary units, as the server). */
export function fmtBytes(b: number): string {
	if (!Number.isFinite(b) || b < 1024) return `${Math.max(0, Math.round(b || 0))} B`;
	const units = ['KB', 'MB', 'GB', 'TB'];
	let v = b / 1024;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}
