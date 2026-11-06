import { describe, expect, it } from 'vitest';
import { A4, ACCENTS, contrastText, headerLines, layout, pageLabel, pdfName, legend, printReportHTML, tint, toneFor, wrap, type ReportHeader, type Sheet } from './reportPdf';

// Each character is 5 points wide.
const measure = (t: string) => t.length * 5;
const g = { ...A4, headerHeight: 60 };
const header: ReportHeader = { classroom: 'Science', teacher: 'Ms P', title: '', subtitle: 'Term 1', institution: 'RUSL', date_mode: 'custom', date_value: '2026-11-06', show_teacher: true, footer: '© RUSL', page_numbers: true, logo: '', accent: '#1e4fa3', color_marks: true, band_high: 75, band_middle: 50 };

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

describe('logo and colour on reports (D-143)', () => {
	const sheet: Sheet = { heading: 'Marks', head: ['Student', 'Quiz'], rows: [['Amaya', '8 / 10 (80%)'], ['<b>Ben</b>', '—']], tones: [[null, 'high'], [null, 'missing']] };

	it('bands marks', () => {
		expect([toneFor(100), toneFor(75), toneFor(74.9), toneFor(50), toneFor(49.9), toneFor(0), toneFor(null)]).toEqual(['high', 'high', 'middle', 'middle', 'low', 'low', null]);
	});

	it('picks text that reads on the colour', () => {
		expect(contrastText('#000000')).toBe('#ffffff');
		expect(contrastText('#ffffff')).toBe('#111111');
		expect(contrastText('#ffeb3b')).toBe('#111111');
		for (const a of ACCENTS) expect(contrastText(a.v), a.label).toBe('#ffffff');
	});

	it('mixes a colour with white', () => {
		expect(tint('#000000', 0.5)).toBe('#808080');
		expect(tint('#1e4fa3', 0)).toBe('#ffffff');
		expect(tint('#1e4fa3', 1)).toBe('#1e4fa3');
	});

	it('keeps each row’s colours through the page layout', () => {
		const pages = layout([sheet], measure, g);
		expect(pages[0].tones).toEqual([[null, 'high'], [null, 'missing']]);
		const many: Sheet = { head: ['S', 'M'], rows: Array.from({ length: 80 }, (_, i) => [`S${i}`, i]), tones: Array.from({ length: 80 }, (_, i) => [null, i === 79 ? 'low' : 'high']) };
		const split = layout([many], measure, g);
		expect(split.length).toBeGreaterThan(1);
		expect(split.at(-1)!.tones.at(-1)).toEqual([null, 'low']);
	});

	it('prints with the logo, colour and footer, and escapes what it’s given', () => {
		const html = printReportHTML([sheet], { ...header, logo: 'data:image/png;base64,AAAA', accent: '#0f766e' }, 'Report', new Date('2026-01-02T12:00:00'));
		expect(html).toContain('<img src="data:image/png;base64,AAAA"');
		expect(html).toContain('th{background:#0f766e;color:#ffffff');
		expect(html).toContain('style="background:#dcfce7');
		expect(html).toContain('75% and above');
		expect(html).toContain('© RUSL');
		expect(html).toContain('&lt;b&gt;Ben&lt;/b&gt;');
		expect(html).not.toContain('<b>Ben');
	});

	it('leaves marks plain when the teacher turns colours off, and no logo when there is none', () => {
		const html = printReportHTML([sheet], { ...header, color_marks: false }, 'Report');
		expect(html).not.toContain('style="background:#dcfce7');
		expect(html).not.toContain('75% and above');
		expect(html).not.toContain('<img');
	});
});

describe('a classroom’s own colour bands (D-144)', () => {
	const bands = { high: 80, middle: 40 };
	it('puts marks in the bands the teacher chose', () => {
		expect([toneFor(80, bands), toneFor(79.9, bands), toneFor(40, bands), toneFor(39.9, bands)]).toEqual(['high', 'middle', 'middle', 'low']);
	});
	it('says so in the key', () => {
		expect(legend(bands).map((x) => x.label)).toEqual(['80% and above', '40% to under 80%', 'Below 40%']);
		const html = printReportHTML([{ head: ['A'], rows: [['x']], tones: [['high']] }], { ...header, band_high: 80, band_middle: 40 }, 'Report');
		expect(html).toContain('80% and above');
		expect(html).not.toContain('75% and above');
	});
});

