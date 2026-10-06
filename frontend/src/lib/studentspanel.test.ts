// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import StudentsPanel from './StudentsPanel.svelte';

const cats = [
	{ id: 'c1', classroom_id: 'k', name: 'Group A', color: 1, position: 0, members: 1 },
	{ id: 'c2', classroom_id: 'k', name: 'Needs support', color: 6, position: 1, members: 1 }
];
const students = [
	{ id: 'e1', student_name: 'Amaya Silva', student_email: 'amaya@x', student_number: 'S1', status: 'active', created_at: '', categories: ['c1'] },
	{ id: 'e2', student_name: 'Kasun Perera', student_email: 'kasun@x', student_number: 'S2', status: 'active', created_at: '', categories: ['c2'] },
	{ id: 'e3', student_name: 'Nimal Fernando', student_email: 'nimal@x', student_number: null, status: 'pending', created_at: '' }
];

const calls: { method: string; url: string; body?: unknown }[] = [];
function fakeFetch() {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		calls.push({ method: init.method ?? 'GET', url, body: init.body ? JSON.parse(init.body as string) : undefined });
		const body = url.endsWith('/categories') ? { categories: cats } : { affected: 1 };
		return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
	});
}
afterEach(() => {
	vi.unstubAllGlobals();
	calls.length = 0;
});

async function render(onchange = () => {}) {
	fakeFetch();
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(StudentsPanel, { target, props: { classroomId: 'k', enrolments: students, onchange, oneditnumber: () => {}, onstatus: () => {} } });
	await vi.waitFor(() => expect(target.querySelectorAll('.chip').length).toBe(3));
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}
const rows = (t: HTMLElement) => [...t.querySelectorAll('tbody tr')].map((r) => r.textContent ?? '');

describe('StudentsPanel', () => {
	test('category chips filter the list; search narrows it', async () => {
		const v = await render();
		expect(rows(v.target)).toHaveLength(3);
		(v.target.querySelectorAll<HTMLButtonElement>('.chip')[2]).click(); // Needs support
		flushSync();
		expect(rows(v.target)).toHaveLength(1);
		expect(rows(v.target)[0]).toContain('Kasun');
		(v.target.querySelectorAll<HTMLButtonElement>('.chip')[0]).click(); // All
		flushSync();
		const search = v.target.querySelector<HTMLInputElement>('input[type=search]')!;
		search.value = 'nim';
		search.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();
		expect(rows(v.target)).toHaveLength(1);
		expect(rows(v.target)[0]).toContain('Nimal');
		v.done();
	});

	test('each row shows its categories by name, not colour alone', async () => {
		const v = await render();
		expect(rows(v.target)[0]).toContain('Group A');
		expect(rows(v.target)[1]).toContain('Needs support');
		v.done();
	});

	test('select students and add them to a category', async () => {
		let changed = 0;
		const v = await render(() => void changed++);
		const boxes = v.target.querySelectorAll<HTMLInputElement>('tbody input[type=checkbox]');
		boxes[1].click();
		boxes[2].click();
		flushSync();
		expect(v.target.textContent).toContain('2 selected');
		const sel = v.target.querySelector<HTMLSelectElement>('select[aria-label=Category]')!;
		sel.value = 'c1';
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		flushSync();
		[...v.target.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Add to')!.click();
		await vi.waitFor(() => expect(changed).toBe(1));
		const post = calls.find((c) => c.method === 'POST');
		expect(post?.url).toBe('/api/teacher/categories/c1/members');
		expect(post?.body).toEqual({ enrolment_ids: ['e2', 'e3'], assigned: true });
		await tick();
		v.done();
	});
});
