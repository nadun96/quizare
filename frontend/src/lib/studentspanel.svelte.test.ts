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

// A fake server that filters and searches enrolments as the Go server does (PL-FR-03).
const calls: { method: string; url: string; body?: unknown }[] = [];
function fakeFetch() {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		calls.push({ method: init.method ?? 'GET', url, body: init.body ? JSON.parse(init.body as string) : undefined });
		let body: unknown = { affected: 1 };
		if (url.endsWith('/categories')) body = { categories: cats };
		else if (url.includes('/enrolments')) {
			const q = new URL(url, 'http://x').searchParams;
			const rows = students.filter((s) => (!q.get('category') || s.categories?.includes(q.get('category')!)) && (!q.get('q') || s.student_name.toLowerCase().includes(q.get('q')!.toLowerCase())));
			body = { enrolments: rows, total: rows.length, page: 1, size: 25 };
		}
		return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });
	});
}
afterEach(() => {
	vi.unstubAllGlobals();
	calls.length = 0;
});

async function render() {
	fakeFetch();
	const target = document.createElement('div');
	document.body.append(target);
	const props = $state({ classroomId: 'k', count: 0, oneditnumber: async () => {}, onstatus: async () => {} });
	const c = mount(StudentsPanel, { target, props });
	await vi.waitFor(() => expect(target.querySelectorAll('tbody tr').length).toBe(3));
	await vi.waitFor(() => expect(target.querySelectorAll('.chip').length).toBe(3));
	flushSync();
	return { target, props, done: () => (unmount(c), target.remove()) };
}
const rows = (t: HTMLElement) => [...t.querySelectorAll('tbody tr')].map((r) => r.textContent ?? '');
const lastList = () => calls.filter((c) => c.url.includes('/enrolments')).at(-1)!.url;

describe('StudentsPanel', () => {
	test('the server filters by category and searches; the count comes from its total', async () => {
		const v = await render();
		expect(v.props.count).toBe(3);
		expect(v.target.querySelector('.chip')!.textContent).toContain('3');
		v.target.querySelectorAll<HTMLButtonElement>('.chip')[2].click(); // Needs support
		await vi.waitFor(() => expect(rows(v.target)).toHaveLength(1));
		expect(lastList()).toContain('category=c2');
		expect(rows(v.target)[0]).toContain('Kasun');
		v.target.querySelectorAll<HTMLButtonElement>('.chip')[0].click(); // All
		await vi.waitFor(() => expect(rows(v.target)).toHaveLength(3));
		const search = v.target.querySelector<HTMLInputElement>('input[type=search]')!;
		search.value = 'nim';
		search.dispatchEvent(new Event('input', { bubbles: true }));
		search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
		await vi.waitFor(() => expect(rows(v.target)).toHaveLength(1));
		expect(lastList()).toContain('q=nim');
		expect(rows(v.target)[0]).toContain('Nimal');
		v.done();
	});

	test('each row shows its categories by name, not colour alone', async () => {
		const v = await render();
		expect(rows(v.target)[0]).toContain('Group A');
		expect(rows(v.target)[1]).toContain('Needs support');
		v.done();
	});

	test('select students on the page and add them to a category, then reload', async () => {
		const v = await render();
		const boxes = v.target.querySelectorAll<HTMLInputElement>('tbody input[type=checkbox]');
		boxes[1].click();
		boxes[2].click();
		flushSync();
		expect(v.target.textContent).toContain('2 selected');
		const sel = v.target.querySelector<HTMLSelectElement>('select[aria-label=Category]')!;
		sel.value = 'c1';
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		flushSync();
		const before = calls.length;
		[...v.target.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Add to')!.click();
		await vi.waitFor(() => expect(calls.slice(before).some((c) => c.url.includes('/enrolments'))).toBe(true));
		const post = calls.find((c) => c.method === 'POST');
		expect(post?.url).toBe('/api/teacher/categories/c1/members');
		expect(post?.body).toEqual({ enrolment_ids: ['e2', 'e3'], assigned: true });
		await tick();
		v.done();
	});
});
