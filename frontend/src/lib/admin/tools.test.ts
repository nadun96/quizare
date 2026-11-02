import { describe, expect, it } from 'vitest';
import { consoleTabs } from '../staff';
import { GROUPS, grouped, toolFrom, TOOLS } from './tools';

const admin = { id: 'a', role: 'admin' } as const;
const manager = { id: 'm', role: 'teacher', manager: { features: ['view_users', 'audit'] } } as const;

describe('the admin console’s tools (D-118)', () => {
	it('gives every console tab a tile in a known group', () => {
		const keys = GROUPS.map((g) => g.key);
		for (const t of consoleTabs(admin as never)) {
			expect(TOOLS[t].label).toBeTruthy();
			expect(keys).toContain(TOOLS[t].group);
		}
		expect(grouped(consoleTabs(admin as never)).flatMap((g) => g.tools).length).toBe(consoleTabs(admin as never).length);
	});

	it('shows a manager only their tools, and leaves out empty groups', () => {
		const g = grouped(consoleTabs(manager as never));
		expect(g.map((x) => x.key)).toEqual(['people', 'system']);
		expect(g.flatMap((x) => x.tools.map((t) => t.key))).toEqual(['users', 'audit']);
	});

	it('finds tools by any word of their name or description', () => {
		const tabs = consoleTabs(admin as never);
		expect(grouped(tabs, 'backup').flatMap((g) => g.tools.map((t) => t.key))).toEqual(['backups']);
		expect(grouped(tabs, 'MAIL server').flatMap((g) => g.tools.map((t) => t.key))).toEqual(['email']);
		expect(grouped(tabs, 'nothing like this')).toEqual([]);
	});

	it('opens a tool from the address only if the person may', () => {
		expect(toolFrom('audit', ['users', 'audit'])).toBe('audit');
		expect(toolFrom('backups', ['users', 'audit'])).toBeNull();
		expect(toolFrom(null, ['users'])).toBeNull();
	});
});
