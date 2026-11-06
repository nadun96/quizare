// Locked PDFs of a classroom's reports (D-141). Each page is a picture the
// browser draws, so Sinhala and Tamil names show correctly and there is no text
// in the file to change; the PDF's permissions allow printing only, under a
// random password nobody keeps. The layout is worked out apart from the drawing.

export type Cell = string | number;
export type Sheet = { heading?: string; head: string[]; rows: Cell[][]; frozen?: number };
export type ReportHeader = { classroom: string; teacher: string; title: string; subtitle: string; institution: string; date_mode: 'today' | 'custom' | 'none'; date_value: string; show_teacher: boolean; footer: string; page_numbers: boolean };

export type Measure = (text: string, bold: boolean) => number;
export type Geometry = { width: number; height: number; margin: number; headerHeight: number; footerHeight: number; lineHeight: number; padding: number; headingHeight: number; maxColumn: number };
export type PageTable = { heading: string; columns: number[]; widths: number[]; headLines: string[][]; rows: string[][][]; rowHeights: number[]; headHeight: number };

// A4 landscape, in points.
export const A4: Geometry = { width: 842, height: 595, margin: 36, headerHeight: 0, footerHeight: 26, lineHeight: 12, padding: 4, headingHeight: 20, maxColumn: 190 };

/** Words that fit the width, a word too long for it broken where it must be. */
export function wrap(text: string, width: number, measure: Measure, bold = false): string[] {
	const lines: string[] = [];
	for (const para of text.split('\n')) {
		let line = '';
		for (const word of para.split(/\s+/).filter(Boolean)) {
			const next = line ? line + ' ' + word : word;
			if (measure(next, bold) <= width) {
				line = next;
				continue;
			}
			if (line) lines.push(line);
			line = '';
			if (measure(word, bold) <= width) {
				line = word;
				continue;
			}
			for (const ch of graphemes(word)) {
				if (line && measure(line + ch, bold) > width) {
					lines.push(line);
					line = '';
				}
				line += ch;
			}
		}
		lines.push(line);
	}
	return lines.length ? lines : [''];
}

function graphemes(s: string): string[] {
	if (typeof Intl !== 'undefined' && 'Segmenter' in Intl) return Array.from(new Intl.Segmenter().segment(s), (x) => x.segment);
	return Array.from(s);
}

/** Lays sheets out on pages: too many columns go on to more pages, repeating the frozen ones; too many rows likewise. */
export function layout(sheets: Sheet[], measure: Measure, g: Geometry): PageTable[] {
	const usable = g.width - g.margin * 2;
	const pages: PageTable[] = [];
	for (const sheet of sheets) {
		const frozen = Math.min(sheet.frozen ?? 1, sheet.head.length);
		const natural = sheet.head.map((h, c) => {
			let w = Math.max(...wrapWidest(h, measure, true));
			for (const r of sheet.rows) w = Math.max(w, ...wrapWidest(String(r[c] ?? ''), measure, false));
			return Math.min(g.maxColumn, w + g.padding * 2 + 1);
		});
		const frozenCols = natural.map((_, c) => c).slice(0, frozen);
		const frozenWidth = frozenCols.reduce((a, c) => a + natural[c], 0);
		// The other columns, as many on a page as fit.
		const groups: number[][] = [];
		let cur: number[] = [];
		let used = frozenWidth;
		for (let c = frozen; c < natural.length; c++) {
			if (cur.length && used + natural[c] > usable) {
				groups.push(cur);
				cur = [];
				used = frozenWidth;
			}
			cur.push(c);
			used += natural[c];
		}
		if (cur.length || !groups.length) groups.push(cur);

		groups.forEach((group, part) => {
			const columns = [...frozenCols, ...group];
			const sum = columns.reduce((a, c) => a + natural[c], 0);
			// Too wide even alone: squeezed. Otherwise left at their width, the spare shared out.
			const ratio = usable / Math.max(1, sum);
			const widths = columns.map((c) => natural[c] * (sum > usable ? ratio : Math.min(ratio, 1.6)));
			const cell = (text: Cell, i: number, bold: boolean) => wrap(String(text ?? ''), Math.max(8, widths[i] - g.padding * 2), measure, bold);
			const headLines = columns.map((c, i) => cell(sheet.head[c], i, true));
			const height = (lines: string[][]) => Math.max(...lines.map((l) => l.length)) * g.lineHeight + g.padding * 2;
			const headHeight = height(headLines);
			const heading = (sheet.heading ?? '') + (groups.length > 1 ? `${sheet.heading ? ' · ' : ''}part ${part + 1} of ${groups.length}` : '');
			const room = g.height - g.margin * 2 - g.headerHeight - g.footerHeight - (heading ? g.headingHeight : 0) - headHeight;
			let page: PageTable = { heading, columns, widths, headLines, rows: [], rowHeights: [], headHeight };
			let left = room;
			for (const r of sheet.rows) {
				const lines = columns.map((c, i) => cell(r[c], i, false));
				const h = height(lines);
				if (page.rows.length && h > left) {
					pages.push(page);
					page = { ...page, rows: [], rowHeights: [] };
					left = room;
				}
				page.rows.push(lines);
				page.rowHeights.push(h);
				left -= h;
			}
			pages.push(page);
		});
	}
	return pages;
}

function wrapWidest(text: string, measure: Measure, bold: boolean): number[] {
	// A cell's natural width: its longest line as written, or its longest word when that's long.
	const lines = text.split('\n');
	return lines.map((l) => measure(l, bold));
}

/** The lines above every page's table. */
export function headerLines(h: ReportHeader, fallbackTitle: string, today: Date): { institution: string; title: string; subtitle: string; meta: string } {
	const date = h.date_mode === 'none' ? '' : h.date_mode === 'custom' ? shortDate(new Date(h.date_value + 'T00:00:00')) : shortDate(today);
	const meta = [h.show_teacher && h.teacher ? `Teacher: ${h.teacher}` : '', date].filter(Boolean).join(' · ');
	return { institution: h.institution, title: h.title || fallbackTitle, subtitle: h.subtitle, meta };
}

const shortDate = (d: Date) => d.toLocaleDateString(undefined, { day: 'numeric', month: 'long', year: 'numeric' });

export const pageLabel = (i: number, n: number) => `Page ${i + 1} of ${n}`;

/** Safe for a file name. */
export const pdfName = (name: string) => (name.replace(/[\\/:*?"<>|]+/g, ' ').replace(/\s+/g, ' ').trim() || 'report') + '.pdf';

const SCALE = 2; // pixels per point: about 144 dpi, sharp in print

/** Draws the pages and saves the locked PDF. Runs in the browser only. */
export async function exportPdf(sheets: Sheet[], header: ReportHeader, fallbackTitle: string, fileName: string): Promise<void> {
	const { jsPDF } = await import('jspdf');
	const family = getComputedStyle(document.body).fontFamily || 'system-ui, sans-serif';
	const font = (size: number, bold = false) => `${bold ? '600 ' : ''}${size}px ${family}`;
	const probe = document.createElement('canvas').getContext('2d')!;
	const measure: Measure = (t, bold) => {
		probe.font = font(9, bold);
		return probe.measureText(t).width;
	};
	const lines = headerLines(header, fallbackTitle, new Date());
	const headerHeight = 8 + (lines.institution ? 14 : 0) + 20 + (lines.subtitle ? 14 : 0) + (lines.meta ? 13 : 0);
	const g: Geometry = { ...A4, headerHeight };
	const pages = layout(sheets, measure, g);

	const ownerPassword = Array.from(crypto.getRandomValues(new Uint8Array(24)), (b) => b.toString(16).padStart(2, '0')).join('');
	const doc = new jsPDF({ orientation: 'landscape', unit: 'pt', format: 'a4', compress: true, encryption: { userPermissions: ['print'], ownerPassword, userPassword: '' } });
	doc.setProperties({ title: lines.title, creator: 'Classroom Quiz', subject: header.classroom });

	pages.forEach((p, n) => {
		const canvas = document.createElement('canvas');
		canvas.width = g.width * SCALE;
		canvas.height = g.height * SCALE;
		const c = canvas.getContext('2d')!;
		c.scale(SCALE, SCALE);
		c.fillStyle = '#fff';
		c.fillRect(0, 0, g.width, g.height);
		c.fillStyle = '#111';
		c.textBaseline = 'alphabetic';
		let y = g.margin;
		const text = (t: string, size: number, x: number, atY: number, bold = false, color = '#111', align: CanvasTextAlign = 'left') => {
			c.font = font(size, bold);
			c.fillStyle = color;
			c.textAlign = align;
			c.fillText(t, x, atY);
		};
		if (lines.institution) {
			y += 11;
			text(lines.institution, 10, g.margin, y, true, '#444');
			y += 3;
		}
		y += 16;
		text(lines.title, 15, g.margin, y, true);
		y += 4;
		if (lines.subtitle) {
			y += 11;
			text(lines.subtitle, 10, g.margin, y, false, '#444');
			y += 3;
		}
		if (lines.meta) {
			y += 11;
			text(lines.meta, 9, g.margin, y, false, '#555');
			y += 2;
		}
		y = g.margin + headerHeight;
		if (p.heading) {
			text(p.heading, 11, g.margin, y + 13, true);
			y += g.headingHeight;
		}
		// The table.
		const totalWidth = p.widths.reduce((a, b) => a + b, 0);
		c.fillStyle = '#f0f0f0';
		c.fillRect(g.margin, y, totalWidth, p.headHeight);
		const drawRow = (cells: string[][], height: number, bold: boolean) => {
			let x = g.margin;
			cells.forEach((cellLines, i) => {
				cellLines.forEach((l, k) => text(l, 9, x + g.padding, y + g.padding + g.lineHeight * (k + 1) - 3, bold));
				x += p.widths[i];
			});
			c.strokeStyle = '#bbb';
			c.lineWidth = 0.5;
			c.strokeRect(g.margin, y, totalWidth, height);
			let vx = g.margin;
			for (const w of p.widths.slice(0, -1)) {
				vx += w;
				c.beginPath();
				c.moveTo(vx, y);
				c.lineTo(vx, y + height);
				c.stroke();
			}
			y += height;
		};
		drawRow(p.headLines, p.headHeight, true);
		p.rows.forEach((r, i) => drawRow(r, p.rowHeights[i], false));
		// The foot.
		const foot = g.height - g.margin + 12;
		c.strokeStyle = '#ccc';
		c.lineWidth = 0.5;
		c.beginPath();
		c.moveTo(g.margin, foot - 12);
		c.lineTo(g.width - g.margin, foot - 12);
		c.stroke();
		if (header.footer) text(header.footer, 8, g.margin, foot, false, '#555');
		if (header.page_numbers) text(pageLabel(n, pages.length), 8, g.width - g.margin, foot, false, '#555', 'right');

		if (n > 0) doc.addPage('a4', 'landscape');
		doc.addImage(canvas, 'PNG', 0, 0, doc.internal.pageSize.getWidth(), doc.internal.pageSize.getHeight(), undefined, 'FAST');
	});
	doc.save(pdfName(fileName));
}
