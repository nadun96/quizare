// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import SessionTeams from './SessionTeams.svelte';
import type { TeamStanding, TeamsView } from './types';

const view: TeamsView = {
	mode: 'random', acceptance: 'all', calc: 'average',
	teams: [
		{ id: 't1', name: 'Owls', color: 3, position: 0, members: 2, member_list: [
			{ attempt_id: 'a1', name: 'Amaya', student_number: 'S1', state: 'submitted', captain: true, team_id: 't1' },
			{ attempt_id: 'a2', name: 'Kasun', student_number: null, state: 'in_progress', captain: false, team_id: 't1' }
		] },
		{ id: 't2', name: 'Foxes', color: 2, position: 1, members: 0, member_list: [] }
	],
	unassigned: [{ attempt_id: 'a3', name: 'Nimal', student_number: null, state: 'waiting', captain: false }]
};
const standings: TeamStanding[] = [
	{ rank: 1, id: 't1', name: 'Owls', color: 3, members: 2, finished: 1, score: 1.5, max_score: 3, pct: 50, complete: false },
	{ rank: 2, id: 't2', name: 'Foxes', color: 2, members: 0, finished: 0, score: 0, max_score: 3, pct: 0, complete: true }
];
const calls: { method: string; url: string; body?: any }[] = [];
afterEach(() => {
	vi.unstubAllGlobals();
	calls.length = 0;
});

async function render(ended = false) {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		calls.push({ method: init.method ?? 'GET', url, body: init.body ? JSON.parse(init.body as string) : undefined });
		if (init.method && init.method !== 'GET') return new Response(null, { status: 204 });
		const body = url.endsWith('/standings') ? { teams: standings } : view;
		return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
	});
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(SessionTeams, { target, props: { sessionId: 's', ended } });
	await vi.waitFor(() => expect(target.querySelectorAll('.stand').length).toBe(2));
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}

describe('SessionTeams', () => {
	test('shows standings with progress and the rule in words', async () => {
		const v = await render();
		const text = v.target.textContent ?? '';
		expect(text).toContain("every member's mark counts, averaged over all members");
		const first = v.target.querySelector('.stand')!.textContent ?? '';
		expect(first).toContain('Owls');
		expect(first).toContain('50%');
		expect(first).toContain('1/2 done');
		expect(first).toContain('marking');
		expect(text).toContain('No team');
		v.done();
	});
	test('moves a student and makes a captain', async () => {
		const v = await render();
		const sel = v.target.querySelector<HTMLSelectElement>('select[aria-label="Move Nimal to"]')!;
		sel.value = 't2';
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST')).toMatchObject({ url: '/api/teacher/sessions/s/teams/members', body: { attempt_ids: ['a3'], team_id: 't2' } }));
		calls.length = 0;
		await vi.waitFor(() => expect(v.target.querySelector<HTMLButtonElement>('[aria-label="Make Kasun captain"]')?.disabled).toBe(false));
		v.target.querySelector<HTMLButtonElement>('[aria-label="Make Kasun captain"]')!.click();
		await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ attempt_ids: ['a2'], captain: true }));
		v.done();
	});
	test('an ended session is read-only', async () => {
		const v = await render(true);
		expect([...v.target.querySelectorAll('button')].some((b) => b.textContent?.includes('Random teams'))).toBe(false);
		expect(v.target.querySelector<HTMLSelectElement>('select[aria-label="Move Nimal to"]')?.disabled).toBe(true);
		v.done();
	});
});
