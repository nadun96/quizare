// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import GroupsPanel from './GroupsPanel.svelte';
import Leaderboard from './Leaderboard.svelte';
import { DEFAULT_SETTINGS, groupRanks } from './scoring';
import type { GroupsView } from './types';

const view: GroupsView = {
	groups: [
		{ id: 'g1', poll_id: 'p', name: 'Owls', color: 3, position: 0, members: [
			{ participant_id: 'a', name: 'Ada', captain: true, group_id: 'g1' },
			{ participant_id: 'b', name: 'Bo', captain: false, group_id: 'g1' }
		] },
		{ id: 'g2', poll_id: 'p', name: 'Foxes', color: 2, position: 1, members: [] }
	],
	ungrouped: [{ participant_id: 'c', name: 'Participant 3', captain: false }]
};
const calls: { method: string; url: string; body?: any }[] = [];
afterEach(() => {
	vi.unstubAllGlobals();
	calls.length = 0;
});

async function render() {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		calls.push({ method: init.method ?? 'GET', url, body: init.body ? JSON.parse(init.body as string) : undefined });
		return init.method && init.method !== 'GET' ? new Response(null, { status: 204 }) : new Response(JSON.stringify(view), { status: 200, headers: { 'Content-Type': 'application/json' } });
	});
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(GroupsPanel, { target, props: { pollId: 'p', settings: { ...DEFAULT_SETTINGS, groups: 'manual', group_acceptance: 'captain' } } });
	await vi.waitFor(() => expect(target.querySelectorAll('section').length).toBe(3));
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}

describe('GroupsPanel', () => {
	test('lists groups, captains and people without a group', async () => {
		const v = await render();
		const text = v.target.textContent ?? '';
		expect(text).toContain('Owls');
		expect(text).toContain('Nobody yet');
		expect(text).toContain('Not in a group');
		expect(text).toContain('only captains answer');
		expect(v.target.querySelector('[aria-label="Ada is captain"]')?.getAttribute('aria-pressed')).toBe('true');
		v.done();
	});
	test('moves a participant and hands over the captaincy', async () => {
		const v = await render();
		const sel = v.target.querySelector<HTMLSelectElement>('select[aria-label="Move Participant 3 to"]')!;
		sel.value = 'g2';
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		await vi.waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
		expect(calls.find((c) => c.method === 'POST')).toMatchObject({ url: '/api/teacher/polls/p/groups/members', body: { participant_ids: ['c'], group_id: 'g2' } });
		calls.length = 0;
		await vi.waitFor(() => expect(v.target.querySelector<HTMLButtonElement>('[aria-label="Make Bo captain"]')?.disabled).toBe(false));
		v.target.querySelector<HTMLButtonElement>('[aria-label="Make Bo captain"]')!.click();
		await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ participant_ids: ['b'], captain: true }));
		v.done();
	});
	test('random groups send the count', async () => {
		const v = await render();
		[...v.target.querySelectorAll('button')].find((b) => b.textContent?.includes('Make random groups'))!.click();
		await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST')).toMatchObject({ url: '/api/teacher/polls/p/groups/generate', body: { from: 'random', count: 4, reassign: false } }));
		v.done();
	});
});

describe('group leaderboard', () => {
	test('shows member counts and colour dots, and marks your group', () => {
		const target = document.createElement('div');
		document.body.append(target);
		const ranks = groupRanks([
			{ rank: 1, id: 'g1', name: 'Owls', color: 3, members: 2, score: 150, answered: 2 },
			{ rank: 2, id: 'g2', name: 'Foxes', color: 2, members: 1, score: 100, answered: 1 }
		]);
		const c = mount(Leaderboard, { target, props: { ranks, meKey: 'g2' } });
		flushSync();
		const rows = target.querySelectorAll('li');
		expect(rows[0].textContent).toContain('2 members');
		expect(rows[1].textContent).toContain('1 member');
		expect(rows[1].classList.contains('me')).toBe(true);
		expect(target.querySelectorAll('.gdot')).toHaveLength(2);
		unmount(c);
		target.remove();
	});
});
