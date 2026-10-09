// Server-side pagination for every list whose data can grow (PL-FR-01, ADR-25).
// A Paged list asks the server for one page at a time and keeps its page,
// size, search, sort and direction in the page's address (TS-FR-94), so the
// back button and shared links bring the same view back. The page size is
// remembered on this device (TS-FR-92).
import { api } from './api';

export const PAGE_SIZES = [10, 25, 50, 100];
const SIZE_KEY = 'qp:page-size';

/** Reads and writes the address's query string. Pages pass urlState (lib/urlstate.ts); tests pass a fake. */
export type UrlState = { read(): URLSearchParams; write(params: URLSearchParams): void };

export type PagedOptions = {
	/** Prefix of this list's query parameters when a page has several lists ("log" → logpage, logsize…). */
	prefix?: string;
	/** Keep the state in the address; off for lists inside dialogs and panels. */
	url?: UrlState | null;
	/** The server's default sort key and direction, which the address leaves out. */
	sort?: string;
	desc?: boolean;
	/** Extra filters sent with every request (status, role…); a change resets to page 1. */
	filters?: () => Record<string, string | undefined>;
};

function storedSize(): number {
	try {
		const n = Number(localStorage.getItem(SIZE_KEY));
		return PAGE_SIZES.includes(n) ? n : 25;
	} catch {
		return 25;
	}
}

export class Paged<T> {
	rows = $state.raw<T[]>([]);
	total = $state(0);
	page = $state(1);
	size = $state(25);
	q = $state('');
	sort = $state('');
	desc = $state(false);
	loaded = $state(false);
	loading = $state(false);
	error = $state('');

	private seq = 0;

	constructor(
		private path: () => string,
		private key: string,
		private opts: PagedOptions = {}
	) {
		this.sort = opts.sort ?? '';
		this.desc = opts.desc ?? false;
		this.size = storedSize();
		const u = opts.url?.read();
		if (u) {
			const p = this.p;
			const n = Number(u.get(p + 'page'));
			if (n > 0) this.page = Math.floor(n);
			const s = Number(u.get(p + 'size'));
			if (PAGE_SIZES.includes(s)) this.size = s;
			this.q = u.get(p + 'q') ?? '';
			if (u.get(p + 'sort')) {
				this.sort = u.get(p + 'sort')!;
				this.desc = this.defaultDesc(this.sort);
			}
			if (u.get(p + 'dir')) this.desc = u.get(p + 'dir') === 'desc';
		}
	}

	/** As on the server: the default sort has its own direction, any other starts ascending. */
	private defaultDesc(sort: string) {
		return sort === (this.opts.sort ?? '') ? !!this.opts.desc : false;
	}

	private get p() {
		return this.opts.prefix ?? '';
	}

	/** A filter's value from the address, for a page to start from (TS-FR-94). */
	static fromUrl(url: UrlState | null | undefined, key: string, prefix = ''): string {
		return url?.read().get(prefix + key) ?? '';
	}

	/** Number of pages (at least 1). */
	get pages() {
		return Math.max(1, Math.ceil(this.total / this.size));
	}
	/** "26–50 of 340", or "No results". */
	get range() {
		if (!this.total) return 'No results';
		const from = (this.page - 1) * this.size + 1;
		return `${from}–${Math.min(this.total, from + this.rows.length - 1)} of ${this.total}`;
	}

	/** The request sent to the server. */
	query(): URLSearchParams {
		const q = new URLSearchParams();
		q.set('page', String(this.page));
		q.set('size', String(this.size));
		if (this.q.trim()) q.set('q', this.q.trim());
		if (this.sort) q.set('sort', this.sort);
		q.set('dir', this.desc ? 'desc' : 'asc');
		for (const [k, v] of Object.entries(this.opts.filters?.() ?? {})) if (v) q.set(k, v);
		return q;
	}

	async load(): Promise<void> {
		const mine = ++this.seq;
		this.loading = true;
		try {
			const path = this.path();
			const res = await api.get<Record<string, unknown>>(path + (path.includes('?') ? '&' : '?') + this.query());
			if (mine !== this.seq) return; // a newer request is on its way
			this.rows = ((res[this.key] as T[]) ?? []) as T[];
			this.total = Number(res.total ?? this.rows.length);
			this.error = '';
			// Rows deleted, or a shared link to a page that no longer exists: show the last page.
			if (this.page > 1 && this.rows.length === 0 && this.total > 0) {
				this.page = this.pages;
				return this.load();
			}
			this.writeUrl();
		} catch (e) {
			if (mine === this.seq) this.error = e instanceof Error ? e.message : 'Could not load';
		} finally {
			if (mine === this.seq) {
				this.loading = false;
				this.loaded = true;
			}
		}
	}

	goTo(n: number) {
		const page = Math.min(Math.max(1, Math.floor(n)), this.pages);
		if (page === this.page) return;
		this.page = page;
		return this.load();
	}
	setSize(n: number) {
		if (!PAGE_SIZES.includes(n)) return;
		// Keep the first row in view: page 3 of 10 (rows 21–30) becomes page 1 of 25.
		const first = (this.page - 1) * this.size;
		this.size = n;
		this.page = Math.floor(first / n) + 1;
		try {
			localStorage.setItem(SIZE_KEY, String(n));
		} catch {
			/* private mode: the size lasts for this page */
		}
		return this.load();
	}
	/** A new search, filter or sort always starts at page 1 (TS-FR-93). */
	search(text: string) {
		this.q = text;
		this.page = 1;
		return this.load();
	}
	refilter() {
		this.page = 1;
		return this.load();
	}
	/** Sorts by a column; the same column again flips the direction. */
	sortBy(key: string, defaultDesc = false) {
		if (this.sort === key) this.desc = !this.desc;
		else {
			this.sort = key;
			this.desc = defaultDesc;
		}
		this.page = 1;
		return this.load();
	}

	private writeUrl() {
		const url = this.opts.url;
		if (!url) return;
		const u = url.read();
		const p = this.p;
		const set = (k: string, v: string, def: string) => (v === def ? u.delete(p + k) : u.set(p + k, v));
		set('page', String(this.page), '1');
		set('size', String(this.size), String(storedSize()));
		set('q', this.q.trim(), '');
		set('sort', this.sort, this.opts.sort ?? '');
		set('dir', this.desc ? 'desc' : 'asc', this.defaultDesc(this.sort) ? 'desc' : 'asc');
		for (const [k, v] of Object.entries(this.opts.filters?.() ?? {})) set(k, v ?? '', '');
		url.write(u);
	}
}
