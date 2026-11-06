// A classroom's reports for paper: locked PDFs (D-141) and the print page, both
// with the classroom's header, footer, logo and colour (D-143). A PDF's pages
// are pictures the browser draws, so Sinhala and Tamil names show correctly and
// there is no text in the file to change; its permissions allow printing only,
// under a random password nobody keeps. The layout is worked out apart from the
// drawing.

export type Cell = string | number;
export type Tone = 'high' | 'middle' | 'low' | 'missing';
export type Sheet = { heading?: string; head: string[]; rows: Cell[][]; tones?: (Tone | null)[][]; frozen?: number };
export type ReportHeader = { classroom: string; teacher: string; title: string; subtitle: string; institution: string; date_mode: 'today' | 'custom' | 'none'; date_value: string; show_teacher: boolean; footer: string; page_numbers: boolean; logo: string; accent: string; color_marks: boolean };

export type Measure = (text: string, bold: boolean) => number;
export type Geometry = { width: number; height: number; margin: number; headerHeight: number; footerHeight: number; lineHeight: number; padding: number; headingHeight: number; maxColumn: number };
export type PageTable = { heading: string; columns: number[]; widths: number[]; headLines: string[][]; rows: string[][][]; tones: (Tone | null)[][]; rowHeights: number[]; headHeight: number };

// A4 landscape, in points.
export const A4: Geometry = { width: 842, height: 595, margin: 36, headerHeight: 0, footerHeight: 26, lineHeight: 12, padding: 4, headingHeight: 20, maxColumn: 190 };

/** Colours to choose from: each is dark enough for white text on it. */
export const ACCENTS: { v: string; label: string }[] = [
	{ v: '#1e4fa3', label: 'Blue' },
	{ v: '#0f766e', label: 'Teal' },
	{ v: '#166534', label: 'Green' },
	{ v: '#6b21a8', label: 'Purple' },
	{ v: '#9f1239', label: 'Crimson' },
	{ v: '#b45309', label: 'Amber' },
	{ v: '#334155', label: 'Slate' }
];

/** How high a mark is: the bands behind the colours. */
export function toneFor(pct: number | null | undefined): Tone | null {
	if (pct == null) return null;
	return pct >= 75 ? 'high' : pct >= 50 ? 'middle' : 'low';
}

/** Dark text on a light tint, so each reads when printed in grey too. */
export const TONE_COLOURS: Record<Tone, { bg: string; fg: string }> = {
	high: { bg: '#dcfce7', fg: '#14532d' },
	middle: { bg: '#fef3c7', fg: '#713f12' },
	low: { bg: '#fee2e2', fg: '#7f1d1d' },
	missing: { bg: '#f1f5f9', fg: '#475569' }
};

export const LEGEND: { tone: Tone; label: string }[] = [
	{ tone: 'high', label: '75% and above' },
	{ tone: 'middle', label: '50% to 74%' },
	{ tone: 'low', label: 'Below 50%' }
];

/** Tints for the screen: a background only, so the text keeps its own contrast in both themes. */
export const toneClass = (t: Tone | null | undefined) => (t === 'high' ? 'bg-success/15' : t === 'middle' ? 'bg-warning/20' : t === 'low' ? 'bg-error/15' : t === 'missing' ? 'bg-base-200' : '');

const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));

/** White or near-black, whichever reads better on the colour (WCAG contrast). */
export function contrastText(hex: string): '#ffffff' | '#111111' {
	const [r, g, b] = rgb(hex).map((c) => {
		const x = c / 255;
		return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
	});
	const lum = 0.2126 * r + 0.7152 * g + 0.0722 * b;
	return 1.05 / (lum + 0.05) >= (lum + 0.05) / 0.0611 ? '#ffffff' : '#111111';
}

/** The colour mixed with white: share 0.08 keeps 8% of it. */
export function tint(hex: string, share: number): string {
	const hexOf = (c: number) => Math.round(c * share + 255 * (1 - share)).toString(16).padStart(2, '0');
	return '#' + rgb(hex).map(hexOf).join('');
}

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

const widest = (text: string, measure: Measure, bold: boolean) => Math.max(...text.split('\n').map((l) => measure(l, bold)));

/** Lays sheets out on pages: too many columns go on to more pages, repeating the frozen ones; too many rows likewise. */
export function layout(sheets: Sheet[], measure: Measure, g: Geometry): PageTable[] {
	const usable = g.width - g.margin * 2;
	const pages: PageTable[] = [];
	for (const sheet of sheets) {
		const frozen = Math.min(sheet.frozen ?? 1, sheet.head.length);
		const natural = sheet.head.map((h, c) => {
			let w = widest(h, measure, true);
			for (const r of sheet.rows) w = Math.max(w, widest(String(r[c] ?? ''), measure, false));
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
			let page: PageTable = { heading, columns, widths, headLines, rows: [], tones: [], rowHeights: [], headHeight };
			let left = room;
			sheet.rows.forEach((r, ri) => {
				const lines = columns.map((c, i) => cell(r[c], i, false));
				const h = height(lines);
				if (page.rows.length && h > left) {
					pages.push(page);
					page = { ...page, rows: [], tones: [], rowHeights: [] };
					left = room;
				}
				page.rows.push(lines);
				page.tones.push(columns.map((c) => sheet.tones?.[ri]?.[c] ?? null));
				page.rowHeights.push(h);
				left -= h;
			});
			pages.push(page);
		});
	}
	return pages;
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

const esc = (s: string | number) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!);

/** The print page: every sheet under the header, with the footer below. */
export function printReportHTML(sheets: Sheet[], h: ReportHeader, fallbackTitle: string, today = new Date()): string {
	const l = headerLines(h, fallbackTitle, today);
	const on = contrastText(h.accent);
	const anyTone = h.color_marks && sheets.some((s) => s.tones?.some((r) => r.some((t) => t && t !== 'missing')));
	const cellStyle = (t: Tone | null | undefined) => (h.color_marks && t ? ` style="background:${TONE_COLOURS[t].bg};color:${TONE_COLOURS[t].fg}"` : '');
	const table = (s: Sheet) => `${s.heading ? `<h2>${esc(s.heading)}</h2>` : ''}<table><thead><tr>${s.head.map((x) => `<th>${esc(x)}</th>`).join('')}</tr></thead><tbody>
${s.rows.map((r, ri) => `<tr>${r.map((c, ci) => `<td${cellStyle(s.tones?.[ri]?.[ci])}>${esc(c)}</td>`).join('')}</tr>`).join('\n')}
</tbody></table>`;
	return `<!doctype html><html><head><meta charset="utf-8"><title>${esc(l.title)}</title><style>
*{-webkit-print-color-adjust:exact;print-color-adjust:exact}
body{font:12px/1.4 system-ui,sans-serif;margin:24px;color:#111}
header{display:flex;gap:14px;align-items:center;border-bottom:2px solid ${h.accent};padding-bottom:8px;margin-bottom:12px}
header img{max-height:56px;max-width:160px}
.inst{font-weight:600;color:#444}h1{font-size:18px;margin:0;color:${h.accent}}.sub{color:#444}.meta{color:#555;font-size:11px}
h2{font-size:13px;margin:14px 0 6px}
table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:4px 6px;text-align:left}
th{background:${h.accent};color:${on};border-color:${h.accent}}tbody tr:nth-child(even){background:${tint(h.accent, 0.05)}}
.legend{margin-top:8px;font-size:10px;color:#555}.legend i{display:inline-block;width:10px;height:10px;margin:0 4px 0 10px;vertical-align:-1px;border:1px solid #bbb}
footer{margin-top:14px;border-top:1px solid ${h.accent};padding-top:4px;color:#555;font-size:10px}
@page{size:landscape;margin:12mm}</style></head><body>
<header>${h.logo ? `<img src="${esc(h.logo)}" alt="">` : ''}<div>${l.institution ? `<div class="inst">${esc(l.institution)}</div>` : ''}<h1>${esc(l.title)}</h1>${l.subtitle ? `<div class="sub">${esc(l.subtitle)}</div>` : ''}${l.meta ? `<div class="meta">${esc(l.meta)}</div>` : ''}</div></header>
${sheets.map(table).join('\n')}
${anyTone ? `<p class="legend">${LEGEND.map((x) => `<i style="background:${TONE_COLOURS[x.tone].bg}"></i>${esc(x.label)}`).join('')}</p>` : ''}
${h.footer ? `<footer>${esc(h.footer)}</footer>` : ''}
</body></html>`;
}

const LOGO_BOX = { width: 600, height: 200 };
const LOGO_BYTES = 300 * 1024;

/** A picture the teacher chose, made small enough to keep: PNG, or JPEG when that's too big. */
export async function logoDataUrl(file: File): Promise<string> {
	const img = await loadImage(URL.createObjectURL(file));
	URL.revokeObjectURL(img.src);
	const scale = Math.min(1, LOGO_BOX.width / img.naturalWidth, LOGO_BOX.height / img.naturalHeight);
	const canvas = document.createElement('canvas');
	canvas.width = Math.max(1, Math.round(img.naturalWidth * scale));
	canvas.height = Math.max(1, Math.round(img.naturalHeight * scale));
	const c = canvas.getContext('2d')!;
	c.drawImage(img, 0, 0, canvas.width, canvas.height);
	const png = canvas.toDataURL('image/png');
	if ((png.length * 3) / 4 <= LOGO_BYTES) return png;
	c.globalCompositeOperation = 'destination-over';
	c.fillStyle = '#fff';
	c.fillRect(0, 0, canvas.width, canvas.height);
	return canvas.toDataURL('image/jpeg', 0.85);
}

function loadImage(src: string): Promise<HTMLImageElement> {
	return new Promise((resolve, reject) => {
		const img = new Image();
		img.onload = () => resolve(img);
		img.onerror = () => reject(new Error('not a picture'));
		img.src = src;
	});
}

const SCALE = 2; // pixels per point: about 144 dpi, sharp in print
const LOGO_HEIGHT = 46;
const LOGO_WIDTH = 130;

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
	const logo = header.logo ? await loadImage(header.logo) : null;
	const logoScale = logo ? Math.min(LOGO_HEIGHT / logo.naturalHeight, LOGO_WIDTH / logo.naturalWidth) : 0;
	const logoWidth = logo ? logo.naturalWidth * logoScale : 0;
	const logoHeight = logo ? logo.naturalHeight * logoScale : 0;
	const textHeight = (lines.institution ? 14 : 0) + 20 + (lines.subtitle ? 14 : 0) + (lines.meta ? 13 : 0);
	const headerHeight = Math.max(textHeight, logoHeight) + 14;
	const g: Geometry = { ...A4, headerHeight };
	const pages = layout(sheets, measure, g);
	const showLegend = header.color_marks && pages.some((p) => p.tones.some((r) => r.some((t) => t && t !== 'missing')));
	const onAccent = contrastText(header.accent);
	const stripe = tint(header.accent, 0.05);
	const line = tint(header.accent, 0.3);

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
		c.textBaseline = 'alphabetic';
		const text = (t: string, size: number, x: number, atY: number, bold = false, color = '#111', align: CanvasTextAlign = 'left') => {
			c.font = font(size, bold);
			c.fillStyle = color;
			c.textAlign = align;
			c.fillText(t, x, atY);
		};
		// The header: logo, then the lines beside it, on a rule in the classroom's colour.
		const blockHeight = headerHeight - 14;
		if (logo) c.drawImage(logo, g.margin, g.margin + (blockHeight - logoHeight) / 2, logoWidth, logoHeight);
		const tx = g.margin + (logo ? logoWidth + 12 : 0);
		let y = g.margin + (blockHeight - textHeight) / 2;
		if (lines.institution) {
			y += 11;
			text(lines.institution, 10, tx, y, true, '#444');
			y += 3;
		}
		y += 16;
		text(lines.title, 15, tx, y, true, header.accent);
		y += 4;
		if (lines.subtitle) {
			y += 11;
			text(lines.subtitle, 10, tx, y, false, '#444');
			y += 3;
		}
		if (lines.meta) {
			y += 11;
			text(lines.meta, 9, tx, y, false, '#555');
		}
		c.fillStyle = header.accent;
		c.fillRect(g.margin, g.margin + blockHeight + 5, g.width - g.margin * 2, 2);
		y = g.margin + headerHeight;
		if (p.heading) {
			text(p.heading, 11, g.margin, y + 13, true);
			y += g.headingHeight;
		}
		// The table.
		const totalWidth = p.widths.reduce((a, b) => a + b, 0);
		const drawRow = (cells: string[][], height: number, bold: boolean, fill: string, ink: string, tones?: (Tone | null)[]) => {
			c.fillStyle = fill;
			c.fillRect(g.margin, y, totalWidth, height);
			let x = g.margin;
			cells.forEach((cellLines, i) => {
				const tone = header.color_marks ? tones?.[i] : null;
				if (tone) {
					c.fillStyle = TONE_COLOURS[tone].bg;
					c.fillRect(x, y, p.widths[i], height);
				}
				cellLines.forEach((l, k) => text(l, 9, x + g.padding, y + g.padding + g.lineHeight * (k + 1) - 3, bold, tone ? TONE_COLOURS[tone].fg : ink));
				x += p.widths[i];
			});
			c.strokeStyle = line;
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
		drawRow(p.headLines, p.headHeight, true, header.accent, onAccent);
		p.rows.forEach((r, i) => drawRow(r, p.rowHeights[i], false, i % 2 ? stripe : '#fff', '#111', p.tones[i]));
		// The foot.
		const foot = g.height - g.margin + 12;
		c.fillStyle = header.accent;
		c.fillRect(g.margin, foot - 12, g.width - g.margin * 2, 1);
		if (header.footer) text(header.footer, 8, g.margin, foot, false, '#555');
		if (header.page_numbers) text(pageLabel(n, pages.length), 8, g.width - g.margin, foot, false, '#555', 'right');
		if (showLegend) {
			// The bands, in the middle of the foot's line above it.
			let lx = g.width / 2 - 120;
			const ly = foot - 22;
			for (const item of LEGEND) {
				c.fillStyle = TONE_COLOURS[item.tone].bg;
				c.fillRect(lx, ly - 7, 8, 8);
				c.strokeStyle = '#999';
				c.strokeRect(lx, ly - 7, 8, 8);
				text(item.label, 7.5, lx + 11, ly, false, '#555');
				lx += 80;
			}
		}

		if (n > 0) doc.addPage('a4', 'landscape');
		doc.addImage(canvas, 'PNG', 0, 0, doc.internal.pageSize.getWidth(), doc.internal.pageSize.getHeight(), undefined, 'FAST');
	});
	doc.save(pdfName(fileName));
}
