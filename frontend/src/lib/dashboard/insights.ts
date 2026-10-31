// The dashboard's charts (D-105): the data from /api/my/dashboard/insights
// and the small pieces of arithmetic the charts share.

export type KPI = { value: number; prev: number | null };
export type Score = { at: string; title: string; classroom: string; pct: number; class_avg: number | null; passed: boolean };
export type SessionStat = { at: string; title: string; classroom: string; avg: number; pass_rate: number; results: number };
export type Week = { week: string; quizzes: number; tutorials: number; students?: number };
export type ClassroomStat = { name: string; avg: number | null; quizzes: number; students?: number; participation?: number | null };
export type Insights = { role: string; days: number; kpis: Record<string, KPI>; scores?: Score[]; sessions?: SessionStat[]; weeks: Week[]; classrooms: ClassroomStat[] };

export const RANGES = [
	{ days: 30, label: '30 days', prev: 'the 30 days before' },
	{ days: 90, label: '90 days', prev: 'the 90 days before' },
	{ days: 365, label: '12 months', prev: 'the year before' }
] as const;

/** The change against the period before, or null when there's nothing to compare. */
export function delta(k: KPI | undefined): number | null {
	if (!k || k.prev === null || k.prev === undefined) return null;
	return Math.round((k.value - k.prev) * 10) / 10;
}

/** "+6 pts", "−2", "no change": signed, with a real minus sign. */
export function deltaText(d: number, unit = ''): string {
	if (d === 0) return 'no change';
	const n = Math.abs(d).toLocaleString(undefined, { maximumFractionDigits: 1 });
	return (d > 0 ? '+' : '−') + n + unit;
}

/** 1,284 / 12.9K: values as tiles show them. */
export function compact(n: number): string {
	return Math.abs(n) >= 10_000 ? new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(n) : n.toLocaleString(undefined, { maximumFractionDigits: 1 });
}

/** Which of five ramp steps a count takes, 0 for none: steps split the largest week evenly. */
export function step(n: number, max: number): number {
	if (n <= 0 || max <= 0) return 0;
	return Math.min(5, Math.max(1, Math.ceil((n / max) * 5)));
}

/** "3 Aug": a week's Monday. */
export const weekLabel = (w: string) => new Date(w + 'T00:00:00').toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
export const dayLabel = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
