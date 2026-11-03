import { describe, expect, it } from 'vitest';
import { nextTodo, reviewList, rightAt, spread, spreadLine } from './review';

const opts = (n: number) => Array.from({ length: n }, (_, i) => ({ id: 'o' + (i + 1), text: 'x' + i }));
const single = (right: number, category = 'HTML', n = 4) => ({ type: 'SINGLE' as const, body: { options: opts(n) }, key: { correct: ['o' + (right + 1)] }, category });

describe('reviewing and balancing a pool (D-132)', () => {
	it('finds where the right answer is', () => {
		expect(rightAt(single(2))).toBe(2);
		expect(rightAt({ type: 'MULTI', body: { options: opts(4) }, key: { correct: ['o1', 'o2'] } })).toBeNull();
		expect(rightAt({ type: 'SINGLE', body: { options: opts(4) }, key: { correct: ['nope'] } })).toBeNull();
	});

	it('sees that most right answers are A', () => {
		const qs = [...Array(8).fill(0).map(() => single(0)), single(1), single(2), single(0, 'CSS'), single(1, 'CSS'), single(2, 'CSS'), single(3, 'CSS')];
		const s = spread(qs);
		const html = s.categories.find((c) => c.category === 'HTML')!;
		expect(html).toMatchObject({ singles: 10, at: [8, 1, 1, 0], top: 0, skewed: true });
		expect(spreadLine(html)).toBe('Mostly A (80%)');
		const css = s.categories.find((c) => c.category === 'CSS')!;
		expect(css.skewed).toBe(false); // too few to judge, and even
		expect(spreadLine(css)).toBe('Spread evenly');
		expect(s.all.singles).toBe(14);
		expect(s.categories.map((c) => c.category)).toEqual(['CSS', 'HTML']);
		// True/False: half each is fair.
		expect(spread(Array(6).fill(0).map((_, i) => single(i % 2, 'TF', 2))).all.skewed).toBe(false);
	});

	it('lists questions to review and finds the next one not done', () => {
		const qs = [{ reviewed_at: 'x', category: 'A' }, { reviewed_at: null, category: 'A' }, { reviewed_at: null, category: 'B' }, { reviewed_at: 'y', category: 'B' }];
		expect(reviewList(qs, 'todo', null)).toHaveLength(2);
		expect(reviewList(qs, 'done', 'B')).toEqual([qs[3]]);
		expect(reviewList(qs, 'all', 'A')).toHaveLength(2);
		expect(nextTodo(qs, 1)).toBe(2);
		expect(nextTodo(qs, 2)).toBe(1);
		expect(nextTodo([{ reviewed_at: 'x' }], 0)).toBe(-1);
	});
});
