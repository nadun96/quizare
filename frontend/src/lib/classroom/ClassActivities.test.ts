// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import ClassActivities from './ClassActivities.svelte';
import TopicSelect from './TopicSelect.svelte';

const goto = vi.hoisted(() => vi.fn());
vi.mock('$app/navigation', () => ({ goto }));
vi.mock('../tutoring/client', () => ({
	tutoringConfig: async () => ({ enabled: true }),
	TutorClient: class {
		get = async () => ({ sessions: [{ id: 's1', title: 'Revision', status: 'open', join_code: 'TUT1', created_at: '2026-11-01T00:00:00Z' }] });
		post = async (url: string, body: unknown) => {
			calls.push({ method: 'POST', url, body });
			return { join_code: 'TUT2' };
		};
	}
}));

const calls: { method: string; url: string; body?: unknown }[] = [];
afterEach(() => {
	vi.unstubAllGlobals();
	goto.mockClear();
	calls.length = 0;
});
const settle = async () => {
	for (let i = 0; i < 6; i++) {
		await tick();
		await new Promise((r) => setTimeout(r));
	}
	flushSync();
};
function stub(replies: Record<string, unknown>) {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		const method = init.method ?? 'GET';
		calls.push({ method, url, body: typeof init.body === 'string' ? JSON.parse(init.body) : undefined });
		const key = Object.keys(replies).find((k) => url.startsWith(k.split(' ')[1]) && k.startsWith(method));
		return new Response(JSON.stringify(key ? replies[key] : {}), { status: 200, headers: { 'Content-Type': 'application/json' } });
	});
}
const lists = {
	'GET /api/teacher/polls': { polls: [{ id: 'p1', title: 'Warm up', status: 'open', participants: 3, created_at: '2026-11-01T00:00:00Z' }] },
	'GET /api/teacher/meetings': { meetings: [{ id: 'm1', title: 'Office hours', code: 'MEET1', status: 'open', created_at: '2026-11-01T00:00:00Z' }] }
};
function render(props: { classroomId: string; topicId?: string | null }) {
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(ClassActivities, { target, props });
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}
const submit = (t: HTMLElement, label: string, value: string) => {
	const input = t.querySelector(`input[aria-label="${label}"]`) as HTMLInputElement;
	input.value = value;
	input.dispatchEvent(new Event('input', { bubbles: true }));
	flushSync();
	input.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
};

describe('polls, meetings and tutoring in a classroom or topic (D-147)', () => {
	test('lists a topic’s own, with links', async () => {
		stub(lists);
		const { target, done } = render({ classroomId: 'c1', topicId: 't1' });
		await settle();
		expect(calls.filter((c) => c.method === 'GET').every((c) => c.url.includes('topic_id=t1'))).toBe(true);
		expect(target.querySelector('a[href="/t/polls/p1"]')?.textContent).toBe('Warm up');
		expect(target.querySelector('a[href="/m/MEET1"]')?.textContent).toBe('Office hours');
		expect(target.querySelector('a[href="/tutor/TUT1"]')?.textContent).toBe('Revision');
		expect([...target.querySelectorAll('a')].find((a) => a.textContent === 'Schedule')?.getAttribute('href')).toBe('/t/tutoring?classroom_id=c1&topic_id=t1');
		done();
	});

	test('a classroom lists by classroom', async () => {
		stub(lists);
		const { done } = render({ classroomId: 'c1' });
		await settle();
		expect(calls.filter((c) => c.method === 'GET').every((c) => c.url.includes('classroom_id=c1') && !c.url.includes('topic_id'))).toBe(true);
		done();
	});

	test('a new poll is for the classroom’s students, by name, under the topic', async () => {
		stub({ ...lists, 'POST /api/teacher/polls': { id: 'p9' } });
		const { target, done } = render({ classroomId: 'c1', topicId: 't1' });
		await settle();
		submit(target, 'Poll title', 'Quick check');
		await settle();
		expect(calls.find((c) => c.method === 'POST')?.body).toMatchObject({ classroom_id: 'c1', topic_id: 't1', title: 'Quick check', settings: { audience: 'classroom', identity: 'identified' } });
		expect(goto).toHaveBeenCalledWith('/t/polls/p9');
		done();
	});

	test('a new meeting and tutoring session go under the topic and open', async () => {
		stub({ ...lists, 'POST /api/teacher/meetings': { code: 'NEW1' } });
		const { target, done } = render({ classroomId: 'c1', topicId: 't1' });
		await settle();
		submit(target, 'Meeting title', 'Help');
		await settle();
		expect(calls.find((c) => c.url === '/api/teacher/meetings')?.body).toEqual({ classroom_id: 'c1', topic_id: 't1', title: 'Help' });
		expect(goto).toHaveBeenCalledWith('/m/NEW1');
		submit(target, 'Tutoring session title', 'Extra');
		await settle();
		expect(calls.find((c) => c.url === '/api/sessions')?.body).toMatchObject({ classroom_id: 'c1', topic_id: 't1', title: 'Extra' });
		expect(goto).toHaveBeenCalledWith('/tutor/TUT2');
		done();
	});
});

describe('choosing a topic (D-147)', () => {
	const topics = {
		'GET /api/teacher/classrooms/c1/modules': { modules: [{ id: 'm1', name: 'Term 1' }, { id: 'm2', name: 'Empty' }] },
		'GET /api/teacher/modules/m1/topics': { topics: [{ id: 't1', name: 'Cells' }] },
		'GET /api/teacher/modules/m2/topics': { topics: [] }
	};
	test('offers topics by module, without empty ones', async () => {
		stub(topics);
		const target = document.createElement('div');
		const c = mount(TopicSelect, { target, props: { classroomId: 'c1', value: 't1' } });
		await settle();
		expect([...target.querySelectorAll('optgroup')].map((g) => g.label)).toEqual(['Term 1']);
		expect(target.querySelector('select')?.value).toBe('t1');
		unmount(c);
	});
	test('shows nothing for a classroom without topics', async () => {
		stub({ 'GET /api/teacher/classrooms/c1/modules': { modules: [] } });
		const target = document.createElement('div');
		const c = mount(TopicSelect, { target, props: { classroomId: 'c1' } });
		await settle();
		expect(target.querySelector('select')).toBeNull();
		unmount(c);
	});
});
