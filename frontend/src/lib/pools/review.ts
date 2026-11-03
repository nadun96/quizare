// Reviewing and balancing a pool's questions (D-132): which questions a
// teacher has checked, and where the right answers sit (A, B, C…), since a
// hand-written file often puts the right one first.
import type { Question } from '../types';

export const LETTERS = 'ABCDEFGHIJ';
export const letter = (i: number) => LETTERS[i] ?? String(i + 1);

/** Position of a single-answer question's right option (0 = A), or null when it has none or isn't single-answer. */
export function rightAt(q: Pick<Question, 'type' | 'body' | 'key'>): number | null {
	if (q.type !== 'SINGLE') return null;
	const id = q.key?.correct?.[0];
	const i = (q.body?.options ?? []).findIndex((o) => o.id === id);
	return id && i >= 0 ? i : null;
}

export type Spread = {
	category: string;
	/** Single-answer questions counted. */
	singles: number;
	/** How many have the right answer at A, B, C… (as long as the most options any has). */
	at: number[];
	/** The most used position and its share, 0–1. */
	top: number;
	share: number;
	/** Lopsided: one position has clearly more than its fair share. */
	skewed: boolean;
};

/**
 * Where the right answers sit, per category (in name order, "no category"
 * last) and in all. Lopsided means the busiest position holds over half
 * again its fair share, with at least 6 questions to judge by.
 */
export function spread(questions: Pick<Question, 'type' | 'body' | 'key' | 'category'>[]): { all: Spread; categories: Spread[] } {
	const make = (category: string, qs: typeof questions): Spread => {
		const at: number[] = [];
		const sizes: number[] = [];
		for (const q of qs) {
			const p = rightAt(q);
			if (p === null) continue;
			while (at.length <= p) at.push(0);
			at[p]++;
			sizes.push((q.body?.options ?? []).length);
		}
		const singles = at.reduce((a, n) => a + n, 0);
		const top = at.length ? at.indexOf(Math.max(...at)) : 0;
		const share = singles ? (at[top] ?? 0) / singles : 0;
		const typical = sizes.length ? sizes.reduce((a, n) => a + n, 0) / sizes.length : 0; // options per question, on average
		const fair = typical ? 1 / typical : 1;
		while (at.length < Math.round(typical)) at.push(0);
		return { category, singles, at, top, share, skewed: singles >= 6 && share > fair * 1.5 };
	};
	const names = [...new Set(questions.map((q) => q.category ?? ''))].sort((a, b) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b)));
	return { all: make('', questions), categories: names.map((c) => make(c, questions.filter((q) => (q.category ?? '') === c))) };
}

/** "Mostly A (80%)", or "Spread evenly". */
export function spreadLine(s: Spread): string {
	if (!s.singles) return 'No single-answer questions';
	return s.skewed ? `Mostly ${letter(s.top)} (${Math.round(s.share * 100)}%)` : 'Spread evenly';
}

export type ReviewFilter = 'all' | 'todo' | 'done';

/** The questions shown for review: by state and category, in pool order. */
export function reviewList<T extends Pick<Question, 'reviewed_at' | 'category'>>(qs: T[], filter: ReviewFilter, category: string | null): T[] {
	return qs.filter((q) => (category === null || (q.category ?? '') === category) && (filter === 'all' || (filter === 'done') === !!q.reviewed_at));
}

/** The next question not yet reviewed after index i, wrapping round; -1 when every one is. */
export function nextTodo(qs: Pick<Question, 'reviewed_at'>[], i: number): number {
	for (let k = 1; k <= qs.length; k++) {
		const j = (i + k) % qs.length;
		if (!qs[j].reviewed_at) return j;
	}
	return -1;
}
