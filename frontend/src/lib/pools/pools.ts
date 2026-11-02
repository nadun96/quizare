// Question pools (D-123): a teacher's bank and each classroom's pool, and
// where an imported file goes, read from its name.
import type { PoolCount, Question } from '../types';

export type PoolSummary = { kind: 'bank' | 'pool'; quiz_id: string; classroom_id?: string; classroom?: string; role?: 'owner' | 'coteacher'; questions: number; categories: PoolCount[] };
export type PreviewRow = { row: number; code: string; renamed?: string; type: string; text: string; category: string; marks: number; errors?: Record<string, string> };
export type ImportResult = { imported: number; rejected: { row: number; code: string; errors: Record<string, string> }[]; preview: boolean; rows: PreviewRow[]; categories: Record<string, number>; renamed: number };

const norm = (s: string) => s.toLowerCase().replace(/[_\-.]+/g, ' ').replace(/\s+/g, ' ').trim();

/**
 * Where a file goes, from its name: "Classroom__Category.csv" or
 * "Classroom - Category.csv". A name that is only a classroom's fills that
 * classroom; one that matches no classroom is a category. Classrooms match
 * whatever the case, spaces, dashes or underscores.
 */
export function fromFileName(file: string, classrooms: { id: string; name: string }[]): { classroomId: string | null; category: string } {
	const base = file.replace(/\.[^.]+$/, '').trim();
	const find = (s: string) => classrooms.find((c) => norm(c.name) === norm(s))?.id ?? null;
	for (const sep of ['__', ' - ', ' – ', ' — ']) {
		const i = base.indexOf(sep);
		if (i > 0) {
			const room = find(base.slice(0, i));
			if (room) return { classroomId: room, category: tidy(base.slice(i + sep.length)) };
		}
	}
	const whole = find(base);
	if (whole) return { classroomId: whole, category: '' };
	// "Grade 10 Science_Algebra": the longest classroom name the file starts with.
	const starts = classrooms.filter((c) => norm(base).startsWith(norm(c.name) + ' ')).sort((a, b) => b.name.length - a.name.length)[0];
	if (starts) return { classroomId: starts.id, category: tidy(base.slice(starts.name.length).replace(/^[\s_\-–—]+/, '')) };
	return { classroomId: null, category: tidy(base) };
}

/** A category as the server keeps it: single spaces, at most 60 characters. */
export const tidy = (s: string) => s.replace(/_/g, ' ').replace(/\s+/g, ' ').trim().slice(0, 60);

/** Questions by category, the categories in name order and "no category" last. */
export function byCategory(qs: Question[]): { category: string; questions: Question[] }[] {
	const m = new Map<string, Question[]>();
	for (const q of qs) {
		const k = q.category ?? '';
		if (!m.has(k)) m.set(k, []);
		m.get(k)!.push(q);
	}
	return [...m.entries()].sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b))).map(([category, questions]) => ({ category, questions }));
}

/** "Algebra 100 · Geometry 100": an import's categories. */
export function categoryLine(c: Record<string, number>): string {
	return Object.entries(c).sort(([a], [b]) => a.localeCompare(b)).map(([k, n]) => `${k || 'No category'} ${n}`).join(' · ');
}
