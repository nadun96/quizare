import { localeTag } from '$lib/i18n/i18n.svelte';
// Optional details of a classroom, meeting or tutoring session (D-128):
// department, course code, year, semester, tags, and a classroom's term.
// Lists filter by them; this turns them into words and filters.

export type Props = {
	department: string;
	course_code: string;
	year: number | null;
	semester: string;
	tags: string[];
	starts_on?: string | null;
	ends_on?: string | null;
};
export type FacetCount = { value: string; count: number };
export type Facets = { departments: FacetCount[]; course_codes: FacetCount[]; years: FacetCount[]; semesters: FacetCount[]; tags: FacetCount[] };
export type PropFilter = { department: string; course_code: string; year: string; semester: string; tags: string[]; when: '' | 'current' | 'upcoming' | 'past' };

export const emptyProps = (): Props => ({ department: '', course_code: '', year: null, semester: '', tags: [], starts_on: null, ends_on: null });
export const emptyFilter = (): PropFilter => ({ department: '', course_code: '', year: '', semester: '', tags: [], when: '' });
export const noFacets = (): Facets => ({ departments: [], course_codes: [], years: [], semesters: [], tags: [] });

/** "IS2201 · Information Systems · 2025 · Semester 1": the details on one line, course code first. */
export function propsLine(p: Props | undefined, withCode = true): string {
	if (!p) return '';
	return [withCode ? p.course_code : '', p.department, p.year ? String(p.year) : '', p.semester].filter(Boolean).join(' · ');
}

const day = (s: string) => new Date(s + 'T00:00:00');
const fmt = (d: Date, year: boolean) => d.toLocaleDateString(localeTag(), { day: 'numeric', month: 'short', ...(year ? { year: 'numeric' } : {}) });

export type Term = { state: 'upcoming' | 'running' | 'ended'; label: string; range: string; progress: number | null };

/**
 * Where a classroom's term is today: not started, running (with how far
 * through, 0–1, when both dates are set) or ended. Null without dates.
 */
export function term(p: Props | undefined, today = new Date()): Term | null {
	const s = p?.starts_on ? day(p.starts_on) : null;
	const e = p?.ends_on ? day(p.ends_on) : null;
	if (!s && !e) return null;
	const t = new Date(today.getFullYear(), today.getMonth(), today.getDate());
	const sameYear = s && e && s.getFullYear() === e.getFullYear();
	const range = s && e ? `${fmt(s, !sameYear)} – ${fmt(e, true)}` : s ? `From ${fmt(s, true)}` : `Until ${fmt(e!, true)}`;
	if (s && t < s) return { state: 'upcoming', label: `Starts ${fmt(s, s.getFullYear() !== t.getFullYear())}`, range, progress: null };
	if (e && t > e) return { state: 'ended', label: 'Ended', range, progress: null };
	const progress = s && e ? Math.min(1, Math.max(0, (t.getTime() - s.getTime()) / Math.max(1, e.getTime() - s.getTime()))) : null;
	const left = e ? Math.round((e.getTime() - t.getTime()) / 86400000) : null;
	return { state: 'running', label: left === null ? 'Running' : left === 0 ? 'Ends today' : left <= 30 ? `${left} ${left === 1 ? 'day' : 'days'} left` : 'Running', range, progress };
}

/** The filter as query parameters, leaving out what's empty. */
export function filterParams(f: PropFilter): Record<string, string | undefined> {
	return {
		department: f.department || undefined,
		course_code: f.course_code || undefined,
		year: f.year || undefined,
		semester: f.semester || undefined,
		tags: f.tags.length ? f.tags.join(',') : undefined,
		when: f.when || undefined
	};
}

/** Reads a filter back from the address. */
export function filterFrom(q: URLSearchParams): PropFilter {
	const when = q.get('when') ?? '';
	return {
		department: q.get('department') ?? '',
		course_code: q.get('course_code') ?? '',
		year: q.get('year') ?? '',
		semester: q.get('semester') ?? '',
		tags: (q.get('tags') ?? '').split(',').map((t) => t.trim()).filter(Boolean),
		when: (['current', 'upcoming', 'past'].includes(when) ? when : '') as PropFilter['when']
	};
}

/** How many filters are on. */
export const activeFilters = (f: PropFilter) => [f.department, f.course_code, f.year, f.semester, f.when].filter(Boolean).length + f.tags.length;

/** Tags typed as text: "lab, first year" → ["lab", "first year"], each once whatever the case. */
export function parseTags(text: string, existing: string[] = []): string[] {
	const out = [...existing];
	for (const raw of text.split(',')) {
		const t = raw.trim().replace(/^#/, '').replace(/\s+/g, ' ').slice(0, 30);
		if (t && !out.some((x) => x.toLowerCase() === t.toLowerCase())) out.push(t);
	}
	return out.slice(0, 10);
}

/** A steady hue for a department, so its classrooms share a colour. */
export function hueOf(s: string): number {
	let h = 0;
	for (const ch of s.toLowerCase()) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
	return h % 360;
}

/** Rows grouped by a detail, groups in order and "no value" last. */
export function groupBy<T extends { props: Props }>(rows: T[], key: '' | 'department' | 'year' | 'semester' | 'course_code', none: string): { label: string; rows: T[] }[] {
	if (!key) return [{ label: '', rows }];
	const m = new Map<string, T[]>();
	for (const r of rows) {
		const v = r.props?.[key];
		const k = v === null || v === undefined || v === '' ? '' : String(v);
		if (!m.has(k)) m.set(k, []);
		m.get(k)!.push(r);
	}
	const keys = [...m.keys()].sort((a, b) => (a === '' ? 1 : b === '' ? -1 : key === 'year' ? Number(b) - Number(a) : a.localeCompare(b)));
	return keys.map((k) => ({ label: k || none, rows: m.get(k)! }));
}
