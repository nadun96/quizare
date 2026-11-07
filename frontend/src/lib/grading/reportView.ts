// What a classroom report shows (D-154): who's in it, in what order, and each
// row's number. The figures come from the server untouched; this only chooses
// and arranges rows, for the screen, CSV, Print and PDF alike.
import type { Bands, Sheet } from './reportPdf';
import { rangeLabel } from './reportPdf';

export type SortDir = 'asc' | 'desc';
export type ViewCol<T> = {
	key: string;
	label: string;
	value: (row: T) => string | number | null | undefined;
	/** Marks and counts read best highest first. */
	numeric?: boolean;
};
export type ViewState = { q: string; show: string; group: string; sort: string; dir: SortDir };
export type ShowOption = { v: string; label: string };

export const newView = (sort = 'student'): ViewState => ({ q: '', show: '', group: '', sort, dir: 'asc' });

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** Blanks go last whichever way it's sorted: nobody wants the missing ones first. */
function compare(a: string | number | null | undefined, b: string | number | null | undefined, dir: SortDir): number {
	const blank = (x: unknown) => x == null || x === '';
	if (blank(a) || blank(b)) return blank(a) === blank(b) ? 0 : blank(a) ? 1 : -1;
	const by = typeof a === 'number' && typeof b === 'number' ? a - b : collator.compare(String(a), String(b));
	return dir === 'asc' ? by : -by;
}

/** Rows in the chosen order; equal ones keep the order they came in. */
export function sortRows<T>(rows: T[], cols: ViewCol<T>[], sort: string, dir: SortDir): T[] {
	const col = cols.find((c) => c.key === sort);
	if (!col) return rows;
	return rows.map((row, i) => ({ row, i })).sort((x, y) => compare(col.value(x.row), col.value(y.row), dir) || x.i - y.i).map((x) => x.row);
}

/** The view after pressing a column: the same one flips, a new one starts its natural way. */
export function nextSort<T>(view: ViewState, cols: ViewCol<T>[], key: string): ViewState {
	if (view.sort === key) return { ...view, dir: view.dir === 'asc' ? 'desc' : 'asc' };
	return { ...view, sort: key, dir: cols.find((c) => c.key === key)?.numeric ? 'desc' : 'asc' };
}

export const matches = (text: string, q: string) => {
	const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
	const hay = text.toLowerCase();
	return words.every((w) => hay.includes(w));
};

/** One choice per colour range, for the Show filter. */
export function bandOptions(bands: Bands, what: string): ShowOption[] {
	return bands.map((b, i) => ({ v: `band:${b.from}`, label: `${what}: ${rangeLabel(bands, i)}` }));
}

/** Whether a percentage is in the range the Show filter names. A row with none is in no range. */
export function inBand(pct: number | null | undefined, show: string, bands: Bands): boolean {
	if (pct == null) return false;
	const from = Number(show.slice('band:'.length));
	const i = bands.findIndex((b) => b.from === from);
	if (i < 0) return true;
	return pct >= from && (i === 0 || pct < bands[i - 1].from);
}

export const isFiltered = (v: ViewState) => !!(v.q.trim() || v.show || v.group);

/** "Showing 3 of 12 students", for paper, so nobody takes part of a class for all of it. */
export function shownNote(shown: number, total: number, noun: string): string {
	return shown === total ? '' : `Showing ${shown} of ${total} ${noun}`;
}

/** A sheet with its rows numbered in the order they're shown. */
export function numberSheet(s: Sheet): Sheet {
	return {
		...s,
		head: ['No.', ...s.head],
		rows: s.rows.map((r, i) => [i + 1, ...r]),
		tones: s.tones?.map((t) => [null, ...t]),
		frozen: (s.frozen ?? 1) + 1
	};
}

/** CSV rows with the first `count` after the heading numbered, and lines below them moved over to match. */
export function numberCSV(rows: (string | number)[][], count: number): (string | number)[][] {
	return rows.map((r, i) => (i === 0 ? ['No.', ...r] : i <= count ? [i, ...r] : r.length ? ['', ...r] : r));
}
