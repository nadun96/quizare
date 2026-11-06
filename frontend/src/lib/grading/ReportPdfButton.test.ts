// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import ReportPdfButton from './ReportPdfButton.svelte';
import { exportPdf } from './reportPdf';

vi.mock('./reportPdf', async (real) => ({ ...(await real<typeof import('./reportPdf')>()), exportPdf: vi.fn(async () => {}) }));

const saved = { classroom: 'Science', teacher: 'Ms P', title: 'Term report', subtitle: '', institution: '', date_mode: 'today', date_value: '', show_teacher: true, footer: '© RUSL', page_numbers: true, logo: '', accent: '#1e4fa3', color_marks: true, band_high: 80, band_middle: 40 };
const calls: { method: string; body?: unknown }[] = [];
afterEach(() => {
	vi.unstubAllGlobals();
	vi.mocked(exportPdf).mockClear();
	calls.length = 0;
});
function render(disabled = false) {
	vi.stubGlobal('fetch', async (_url: string, init: RequestInit = {}) => {
		const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined;
		calls.push({ method: init.method ?? 'GET', body });
		return new Response(JSON.stringify(body ?? saved), { status: 200, headers: { 'Content-Type': 'application/json' } });
	});
	const target = document.createElement('div');
	document.body.append(target);
	const sheets = () => [{ head: ['Student'], rows: [['Amaya']] }];
	const c = mount(ReportPdfButton, { target, props: { classroomId: 'c1', title: 'Science: class report', fileName: 'Science', sheets, disabled } });
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}
const button = (t: HTMLElement, text: string) => [...t.querySelectorAll('button')].find((b) => b.textContent?.includes(text))!;
const settle = async () => {
	for (let i = 0; i < 5; i++) {
		await tick();
		await new Promise((r) => setTimeout(r));
	}
	flushSync();
};

describe('downloading a report (D-141)', () => {
	test('Download PDF downloads at once, with the saved header', async () => {
		const { target, done } = render();
		button(target, 'Download PDF').click();
		await settle();
		expect(calls.every((c) => c.method === 'GET')).toBe(true); // nothing saved, no form
		expect(exportPdf).toHaveBeenCalledOnce();
		const [sheets, header, title, file] = vi.mocked(exportPdf).mock.calls[0];
		expect(sheets[0].rows).toEqual([['Amaya']]);
		expect(header.footer).toBe('© RUSL');
		expect(title).toBe('Science: class report');
		expect(file).toBe('Science');
		done();
	});

	test('tells the page where its colours change', async () => {
		const seen: unknown[] = [];
		vi.stubGlobal('fetch', async () => new Response(JSON.stringify(saved), { status: 200, headers: { 'Content-Type': 'application/json' } }));
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(ReportPdfButton, { target, props: { classroomId: 'c1', title: 'T', fileName: 'F', sheets: () => [], onbands: (b: unknown) => seen.push(b) } });
		flushSync();
		await settle();
		expect(seen[0]).toEqual({ high: 80, middle: 40 });
		unmount(c);
		target.remove();
	});

	test('Print opens at once, and writes the report with the saved header', async () => {
		const doc = { write: vi.fn(), close: vi.fn() };
		const printed = vi.fn();
		const open = vi.fn(() => ({ document: { ...doc, querySelector: () => null }, close: vi.fn(), focus: vi.fn(), print: printed }));
		vi.stubGlobal('open', open);
		const { target, done } = render();
		button(target, 'Print').click();
		expect(open).toHaveBeenCalledOnce(); // in the click, or the browser blocks it
		await settle();
		expect(doc.write.mock.calls[0][0]).toContain('© RUSL');
		expect(doc.write.mock.calls[0][0]).toContain('Amaya');
		expect(printed).toHaveBeenCalledOnce();
		done();
	});

	test('with nothing to download, it stays off', async () => {
		const { target, done } = render(true);
		expect(button(target, 'Download PDF').disabled).toBe(true);
		expect(button(target, 'Print').disabled).toBe(true);
		expect(button(target, 'Header and footer').disabled).toBe(false); // can still be set up
		done();
	});
});
