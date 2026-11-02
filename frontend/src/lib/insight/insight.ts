// Explore (D-120): the catalog and results of /api/insight, and the small
// pieces the page needs: a pivot of two dimensions, CSV, the query kept in
// the address, and date ranges.

export type Fact = 'activity' | 'answers';
export type CatalogMeasure = { key: string; label: string; unit?: string; fact: Fact; help?: string };
export type CatalogDim = { key: string; label: string; facts: Fact[] };
export type Cell = { id: string; label: string };
export type Catalog = { measures: CatalogMeasure[]; dimensions: CatalogDim[]; grains: string[]; classrooms: Cell[]; staff: boolean; min_group?: number };

export type Query = { measures: string[]; dimensions: string[]; grain: string; from: string; to: string; filters: Record<string, string[]>; totals: boolean };
export type Column = { key: string; label: string; kind: 'dimension' | 'measure'; unit?: string };
export type Row = { dims: Record<string, Cell> | null; values: Record<string, number | null>; level: number; hidden?: boolean };
export type Result = { columns: Column[]; rows: Row[]; truncated: boolean; from: string; to: string; grain: string; loaded_at: string | null; min_group?: number };

const iso = (d: Date) => d.toISOString().slice(0, 10);

export const RANGES = [
	{ key: '30d', label: 'Last 30 days', days: 30 },
	{ key: '90d', label: 'Last 90 days', days: 90 },
	{ key: '1y', label: 'Last 12 months', days: 365 },
	{ key: '2y', label: 'Last 2 years', days: 730 },
	{ key: '5y', label: 'Last 5 years', days: 1826 }
] as const;

/** The dates of a range ending today. */
export function range(key: string, today = new Date()): { from: string; to: string } {
	const r = RANGES.find((x) => x.key === key) ?? RANGES[2];
	const from = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() - r.days + 1));
	return { from: iso(from), to: iso(today) };
}

export const DEFAULT: Query = { measures: ['results', 'avg_score', 'students'], dimensions: ['date'], grain: 'month', ...range('1y'), filters: {}, totals: true };

/** The query in the address, so a view can be bookmarked and shared. */
export function toParams(q: Query): URLSearchParams {
	const p = new URLSearchParams();
	p.set('m', q.measures.join(','));
	if (q.dimensions.length) p.set('d', q.dimensions.join(','));
	p.set('g', q.grain);
	p.set('from', q.from);
	p.set('to', q.to);
	for (const [k, v] of Object.entries(q.filters)) if (v.length) p.set('f.' + k, v.join(','));
	if (!q.totals) p.set('totals', '0');
	return p;
}

export function fromParams(p: URLSearchParams, today = new Date()): Query {
	if (!p.get('m')) return { ...DEFAULT, ...range('1y', today), filters: {} };
	const list = (k: string) => (p.get(k) ?? '').split(',').filter(Boolean);
	const filters: Record<string, string[]> = {};
	for (const [k, v] of p) if (k.startsWith('f.')) filters[k.slice(2)] = v.split(',').filter(Boolean);
	const r = range('1y', today);
	return { measures: list('m'), dimensions: list('d').slice(0, 3), grain: p.get('g') || 'month', from: p.get('from') || r.from, to: p.get('to') || r.to, filters, totals: p.get('totals') !== '0' };
}

/** A date dimension's value, as people read it at the query's grain. */
export function dateLabel(v: string, grain: string): string {
	const [y, m, d] = v.split('-').map(Number);
	const dt = new Date(Date.UTC(y, (m || 1) - 1, d || 1));
	const o: Intl.DateTimeFormatOptions = { timeZone: 'UTC' };
	if (grain === 'year') return String(y);
	if (grain === 'quarter') return `Q${Math.floor(((m || 1) - 1) / 3) + 1} ${y}`;
	if (grain === 'month') return dt.toLocaleDateString(undefined, { ...o, month: 'short', year: 'numeric' });
	const s = dt.toLocaleDateString(undefined, { ...o, day: 'numeric', month: 'short', year: 'numeric' });
	return grain === 'week' ? `Week of ${s}` : s;
}

/** A cell's text: the label, or the date at the grain. */
export function cellText(dim: string, c: Cell | undefined, grain: string): string {
	if (!c) return 'All';
	return dim === 'date' ? dateLabel(c.label, grain) : c.label;
}

export function fmt(v: number | null | undefined, unit?: string): string {
	if (v === null || v === undefined) return '—';
	const n = v.toLocaleString(undefined, { maximumFractionDigits: 1 });
	return unit === '%' ? n + '%' : unit ? `${n} ${unit}` : n;
}

/** Two dimensions as a grid: the first down, the second across, one measure in the cells. */
export function pivot(r: Result, measure: string) {
	const [a, b] = r.columns.filter((c) => c.kind === 'dimension').map((c) => c.key);
	const rows: Cell[] = [];
	const cols: Cell[] = [];
	const seenR = new Set<string>();
	const seenC = new Set<string>();
	const cells = new Map<string, number | null>();
	for (const row of r.rows) {
		if (row.level !== 0 || !row.dims) continue;
		const x = row.dims[a];
		const y = row.dims[b];
		if (!x || !y) continue;
		if (!seenR.has(x.id)) (seenR.add(x.id), rows.push(x));
		if (!seenC.has(y.id)) (seenC.add(y.id), cols.push(y));
		cells.set(x.id + '|' + y.id, row.hidden ? null : (row.values[measure] ?? null));
	}
	return { rowDim: a, colDim: b, rows, cols, at: (x: string, y: string) => cells.get(x + '|' + y) };
}

const csvCell = (s: string) => (/[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);

/** The table as CSV: a column per dimension and measure; totals say "All". */
export function toCSV(r: Result): string {
	const head = r.columns.map((c) => c.label + (c.unit ? ` (${c.unit})` : ''));
	const lines = [head.map(csvCell).join(',')];
	for (const row of r.rows) {
		lines.push(
			r.columns
				.map((c) => (c.kind === 'dimension' ? cellText(c.key, row.dims?.[c.key], r.grain) : row.hidden || row.values[c.key] == null ? '' : String(row.values[c.key])))
				.map(csvCell)
				.join(',')
		);
	}
	return lines.join('\n') + '\n';
}
