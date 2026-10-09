// @vitest-environment jsdom
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';

// A fake server: 53 rows, paged as the Go page package does.
const calls: string[] = [];
vi.mock('./api', () => ({
	api: {
		get: async (path: string) => {
			calls.push(path);
			const u = new URL(path, 'http://x');
			const page = Number(u.searchParams.get('page'));
			const size = Number(u.searchParams.get('size'));
			const q = u.searchParams.get('q') ?? '';
			const all = Array.from({ length: 53 }, (_, i) => ({ id: i + 1, name: `Row ${i + 1}` })).filter((r) => r.name.includes(q));
			if (u.searchParams.get('dir') === 'desc') all.reverse();
			return { rows: all.slice((page - 1) * size, page * size), total: all.length, page, size };
		}
	}
}));

const { Paged } = await import('./paged.svelte');
const Pager = (await import('./ui/Pager.svelte')).default;

function fakeUrl(start = '') {
	let qs = new URLSearchParams(start);
	return { read: () => new URLSearchParams(qs), write: (p: URLSearchParams) => (qs = p), get: () => qs.toString() };
}

beforeEach(() => {
	calls.length = 0;
	localStorage.clear();
});

describe('Paged (PL-FR-01, TS-FR-92 to TS-FR-94)', () => {
	test('asks the server for one page and reports the range', async () => {
		const l = new Paged<{ id: number }>(() => '/api/x', 'rows');
		await l.load();
		expect(calls[0]).toBe('/api/x?page=1&size=25&dir=asc');
		expect(l.rows.length).toBe(25);
		expect(l.total).toBe(53);
		expect(l.pages).toBe(3);
		expect(l.range).toBe('1–25 of 53');
		await l.goTo(3);
		expect(l.rows.map((r) => r.id)).toEqual([51, 52, 53]);
		expect(l.range).toBe('51–53 of 53');
		await l.goTo(99); // clamped
		expect(l.page).toBe(3);
	});
	test('search, sort and filters start again at page 1 and are sent to the server', async () => {
		let status = 'open';
		const l = new Paged<{ id: number }>(() => '/api/x?scope=a', 'rows', { sort: 'created', desc: true, filters: () => ({ status, empty: '' }) });
		await l.goTo(1);
		await l.load();
		await l.goTo(2);
		await l.search('Row 1');
		expect(l.page).toBe(1);
		expect(calls.at(-1)).toBe('/api/x?scope=a&page=1&size=25&q=Row+1&sort=created&dir=desc&status=open');
		await l.sortBy('name');
		expect([l.sort, l.desc]).toEqual(['name', false]);
		await l.sortBy('name');
		expect(l.desc).toBe(true);
		status = 'closed';
		await l.refilter();
		expect(calls.at(-1)).toContain('status=closed');
	});
	test('the page size is remembered on the device and keeps the first row in view', async () => {
		const l = new Paged<{ id: number }>(() => '/api/x', 'rows', { url: null });
		await l.load();
		await l.goTo(3); // rows 51–53 at 25 per page
		await l.setSize(50);
		expect(l.page).toBe(2);
		expect(l.rows[0]).toEqual({ id: 51, name: 'Row 51' });
		expect(new Paged(() => '/api/x', 'rows').size).toBe(50);
	});
	test('state lives in the address, with a prefix per list, and comes back from it', async () => {
		const url = fakeUrl('tab=log');
		const l = new Paged<{ id: number }>(() => '/api/x', 'rows', { url, prefix: 'log', sort: 'time' });
		await l.load();
		expect(url.get()).toBe('tab=log'); // defaults stay out of the address
		await l.goTo(2);
		await l.search('Row');
		await l.goTo(2);
		expect(Object.fromEntries(new URLSearchParams(url.get()))).toEqual({ tab: 'log', logpage: '2', logq: 'Row' });
		const again = new Paged<{ id: number }>(() => '/api/x', 'rows', { url, prefix: 'log', sort: 'time' });
		expect([again.page, again.q]).toEqual([2, 'Row']);
		// Filters are in the address too, while set.
		const f = new Paged<{ id: number }>(() => '/api/x', 'rows', { url, filters: () => ({ status: 'open', none: undefined }) });
		await f.load();
		expect(Paged.fromUrl(url, 'status')).toBe('open');
		expect(url.get()).not.toContain('none');
	});
	test('a link with another sort and no direction sorts ascending, as the server does', () => {
		const l = new Paged(() => '/api/x', 'rows', { url: fakeUrl('sort=name'), sort: 'created', desc: true });
		expect([l.sort, l.desc]).toEqual(['name', false]);
		const d = new Paged(() => '/api/x', 'rows', { url: fakeUrl('sort=created'), sort: 'created', desc: true });
		expect(d.desc).toBe(true);
	});
	test('a stale page past the end falls back to the last page', async () => {
		const l = new Paged<{ id: number }>(() => '/api/x', 'rows', { url: fakeUrl('page=9') });
		await l.load();
		expect(l.page).toBe(3);
		expect(l.rows.length).toBe(3);
	});
});

describe('Pager', () => {
	test('shows the range and moves between pages with labelled buttons', async () => {
		const l = new Paged<{ id: number }>(() => '/api/x', 'rows');
		await l.load();
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(Pager, { target, props: { list: l, label: 'Rows' } });
		flushSync();
		expect(target.textContent).toContain('1–25 of 53');
		expect(target.textContent).toContain('Page 1 of 3');
		expect(target.querySelector<HTMLButtonElement>('[aria-label="Previous page"]')!.disabled).toBe(true);
		target.querySelector<HTMLButtonElement>('[aria-label="Last page"]')!.click();
		await vi.waitFor(() => expect(l.page).toBe(3));
		flushSync();
		expect(target.textContent).toContain('51–53 of 53');
		expect(target.querySelector<HTMLButtonElement>('[aria-label="Next page"]')!.disabled).toBe(true);
		expect(target.querySelector('nav')!.getAttribute('aria-label')).toBe('Rows pages');
		unmount(c);
		target.remove();
	});
});
