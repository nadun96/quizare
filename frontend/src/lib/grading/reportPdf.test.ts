import { describe, expect, it } from 'vitest';
import { A4, headerLines, layout, pageLabel, pdfName, wrap, type ReportHeader, type Sheet } from './reportPdf';

// Each character is 5 points wide.
const measure = (t: string) => t.length * 5;
const g = { ...A4, headerHeight: 60 };
const header: ReportHeader = { classroom: 'Science', teacher: 'Ms P', title: '', subtitle: 'Term 1', institution: 'RUSL', date_mode: 'custom', date_value: '2026-11-06', show_teacher: true, footer: '© RUSL', page_numbers: true };

describe('locked report PDFs (D-141)', () => {
	it('wraps words, and breaks one too long to fit', () => {
		expect(wrap('one two three', 40, measure)).toEqual(['one two', 'three']);
		expect(wrap('abcdefghij', 25, measure)).toEqual(['abcde', 'fghij']);
		expect(wrap('', 40, measure)).toEqual(['']);
	});

	it('keeps rows that fit on one page', () => {
		const pages = layout([{ head: ['Student', 'Mark'], rows: [['A', 1], ['B', 2]] }], measure, g);
		expect(pages).toHaveLength(1);
		expect(pages[0].rows.map((r) => r[0][0])).toEqual(['A', 'B']);
	});

	it('runs on to more pages when rows run out of room, repeating the head', () => {
		const rows = Array.from({ length: 80 }, (_, i) => [`S${i}`, i]);
		const pages = layout([{ head: ['Student', 'Mark'], rows }], measure, g);
		expect(pages.length).toBeGreaterThan(1);
		expect(pages.every((p) => p.headLines[0][0] === 'Student')).toBe(true);
		expect(pages.flatMap((p) => p.rows.map((r) => r[0][0]))).toEqual(rows.map((r) => r[0]));
	});

	it('splits a wide report across pages, repeating the student column', () => {
		const head = ['Student', ...Array.from({ length: 12 }, (_, i) => `A long assignment ${i}`)];
		const sheet: Sheet = { heading: 'Marks', head, rows: [head.map((_, i) => (i ? '10 / 10 (100%)' : 'Amaya'))] };
		const pages = layout([sheet], measure, g);
		expect(pages.length).toBeGreaterThan(1);
		expect(pages.every((p) => p.columns[0] === 0)).toBe(true);
		expect(pages[0].heading).toMatch(/^Marks · part 1 of /);
		// Every column is somewhere, once.
		expect(pages.flatMap((p) => p.columns.slice(1))).toEqual(Array.from({ length: 12 }, (_, i) => i + 1));
		for (const p of pages) expect(p.widths.reduce((a, b) => a + b, 0)).toBeLessThanOrEqual(g.width - g.margin * 2 + 0.01);
	});

	it('squeezes a column too wide to fit, rather than running off the page', () => {
		const pages = layout([{ head: ['A', 'B'], rows: [['x'.repeat(500), 'y'.repeat(500)]] }], measure, g);
		for (const p of pages) expect(p.widths.reduce((a, b) => a + b, 0)).toBeLessThanOrEqual(g.width - g.margin * 2 + 0.01);
	});

	it('builds the header from what the teacher chose', () => {
		const today = new Date('2026-01-02T12:00:00');
		const l = headerLines(header, 'Science: class report', today);
		expect(l.title).toBe('Science: class report');
		expect(l.institution).toBe('RUSL');
		expect(l.meta).toContain('Teacher: Ms P');
		expect(l.meta).toContain('2026');
		expect(headerLines({ ...header, title: 'Term report' }, 'x', today).title).toBe('Term report');
		expect(headerLines({ ...header, date_mode: 'none', show_teacher: false }, 'x', today).meta).toBe('');
		expect(headerLines({ ...header, date_mode: 'today' }, 'x', today).meta).toContain('January');
	});

	it('numbers pages and tidies the file name', () => {
		expect(pageLabel(0, 3)).toBe('Page 1 of 3');
		expect(pdfName('Science: a/b')).toBe('Science a b.pdf');
		expect(pdfName('  ')).toBe('report.pdf');
	});
});
