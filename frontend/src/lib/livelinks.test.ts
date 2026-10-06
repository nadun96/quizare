// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import LiveLinks from './LiveLinks.svelte';
import Leaderboard from './poll/Leaderboard.svelte';

const calls: { method: string; url: string; body?: any }[] = [];
let links: any[] = [];
afterEach(() => {
	vi.unstubAllGlobals();
	calls.length = 0;
	links = [];
});

async function render(props: Record<string, unknown>) {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		const body = init.body ? JSON.parse(init.body as string) : undefined;
		calls.push({ method: init.method ?? 'GET', url, body });
		const json = (x: unknown, status = 200) => new Response(JSON.stringify(x), { status, headers: { 'Content-Type': 'application/json' } });
		if (init.method === 'POST' && url === '/api/teacher/share-links') {
			const l = { id: 'l1', scope: body.scope, views: body.views, identify: body.identify, label: body.label, expires_at: null, revoked_at: null, created_at: new Date().toISOString(), token: 'tok123' };
			links = [l, { id: 'old', scope: 'session', views: ['pass_rate'], identify: 'anonymous', label: '', expires_at: null, revoked_at: null, created_at: '' }];
			return json(l, 201);
		}
		if (init.method === 'DELETE') return new Response(null, { status: 204 });
		return json({ links });
	});
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(LiveLinks, { target, props: props as any });
	await vi.waitFor(() => expect(calls.length).toBeGreaterThan(0));
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}

describe('LiveLinks', () => {
	test('a poll link defaults to nicknames and shows the URL once', async () => {
		const v = await render({ scope: 'live_poll', targetId: 'p1', teams: true });
		const text = v.target.textContent ?? '';
		expect(text).toContain('Their nicknames');
		expect(text).not.toContain('student IDs');
		v.target.querySelector<HTMLInputElement>('#ll-label')!.value = 'Room 4';
		v.target.querySelector<HTMLInputElement>('#ll-label')!.dispatchEvent(new Event('input', { bubbles: true }));
		v.target.querySelector('form')!.requestSubmit();
		await vi.waitFor(() => expect(v.target.querySelector<HTMLInputElement>('input[aria-label="Live link"]')?.value).toBe(location.origin + '/live/tok123'));
		expect(calls.find((c) => c.method === 'POST')!.body).toMatchObject({ scope: 'live_poll', target_id: 'p1', views: ['leaderboard', 'teams'], identify: 'nickname', label: 'Room 4' });
		// Only this scope's links are listed.
		await vi.waitFor(() => expect(v.target.querySelectorAll('li.link')).toHaveLength(1));
		v.done();
	});
	test('a session link offers student IDs and leaves teams out when there are none', async () => {
		const v = await render({ scope: 'live_session', targetId: 's1', teams: false });
		expect(v.target.textContent).toContain('Classroom student IDs');
		expect(v.target.textContent).not.toContain('Teams');
		v.target.querySelector<HTMLInputElement>('input[value="student_id"]')!.click();
		v.target.querySelector('form')!.requestSubmit();
		await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST')!.body).toMatchObject({ views: ['leaderboard'], identify: 'student_id' }));
		v.done();
	});
});

describe('Leaderboard in percent', () => {
	test('shows percentages and a custom detail line', () => {
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(Leaderboard, { target, props: { ranks: [{ rank: 1, key: 'S1', name: 'S1', score: 87.5, correct: 0, answered: 0, detail: 'Red · 7/8' }], unit: 'percent' } });
		flushSync();
		expect(target.textContent).toContain('87.5%');
		expect(target.textContent).toContain('Red · 7/8');
		unmount(c);
		target.remove();
	});
});
