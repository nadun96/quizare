// A classroom's groups (D-158): its categories with leaders, assistant
// leaders, a public name the leader chooses, and students choosing their own
// while the teacher allows it.

export type Role = 'member' | 'leader' | 'assistant';

export type GroupPerson = { enrolment_id?: string; user_id: string; name: string; student_number?: string | null; role: Role; self_joined?: boolean; gender?: string };

export type ClassGroup = { id: string; classroom_id: string; name: string; color: number; position: number; members: number; public_name: string | null; people: GroupPerson[] };

export type GroupPolicy = { self_join: boolean; opens_at: string | null; closes_at: string | null; max_per_student: number; max_size: number | null; open: boolean };

export type GroupsView = { groups: ClassGroup[]; policy: GroupPolicy; students: GroupPerson[] };

export type MyGroup = {
	id: string;
	name: string;
	teacher_name: string;
	public_name: string | null;
	color: number;
	size: number;
	mine: boolean;
	role?: Role;
	self_joined?: boolean;
	full: boolean;
	members?: GroupPerson[];
};

export type MyGroupsView = { groups: MyGroup[]; policy: GroupPolicy; chosen: number };

export const ROLES: { v: Role; label: string }[] = [
	{ v: 'member', label: 'Member' },
	{ v: 'leader', label: 'Leader' },
	{ v: 'assistant', label: 'Assistant leader' }
];

/** Leaders first, then assistants, then members, each by name. */
export function byRole(people: GroupPerson[]): GroupPerson[] {
	const rank = (r: Role) => (r === 'leader' ? 0 : r === 'assistant' ? 1 : 2);
	return [...people].sort((a, b) => rank(a.role) - rank(b.role) || a.name.localeCompare(b.name));
}

/** The students in no group. */
export function ungrouped(students: GroupPerson[], groups: ClassGroup[]): GroupPerson[] {
	const placed = new Set(groups.flatMap((g) => g.people.map((p) => p.user_id)));
	return students.filter((s) => !placed.has(s.user_id));
}

/**
 * Shares students out among groups, each to the smallest group at the time
 * (ties to the first), in a random order, so the groups end up even. Returns
 * group id → the students to add.
 */
export function shareOut(groups: { id: string; size: number }[], students: string[], random: () => number = Math.random): Map<string, string[]> {
	const out = new Map<string, string[]>(groups.map((g) => [g.id, []]));
	if (!groups.length) return out;
	const sizes = groups.map((g) => g.size);
	const order = [...students];
	for (let i = order.length - 1; i > 0; i--) {
		const j = Math.floor(random() * (i + 1));
		[order[i], order[j]] = [order[j], order[i]];
	}
	for (const s of order) {
		let at = 0;
		for (let i = 1; i < sizes.length; i++) if (sizes[i] < sizes[at]) at = i;
		sizes[at]++;
		out.get(groups[at].id)!.push(s);
	}
	return out;
}

/** What students are told about choosing: whether they may now, and until when. */
export function policyState(p: GroupPolicy, now = new Date()): 'off' | 'soon' | 'open' | 'closed' {
	if (!p.self_join) return 'off';
	if (p.open) return 'open';
	if (p.opens_at && new Date(p.opens_at) > now) return 'soon';
	return 'closed';
}

/** Whether a student may choose this group now, and if not, why. */
export function joinBlock(g: MyGroup, v: MyGroupsView): '' | 'closed' | 'full' | 'limit' | 'in' {
	if (g.mine) return 'in';
	if (!v.policy.open) return 'closed';
	if (g.full) return 'full';
	if (v.chosen >= v.policy.max_per_student) return 'limit';
	return '';
}
