import { describe, expect, it } from 'vitest';
import { flaggedList, submitWarning } from './flags';

describe('flagged questions (D-61)', () => {
	it('lists the flagged questions in order, with whether they are answered', () => {
		expect(flaggedList([true, false, true, false], [false, true, true, false])).toEqual([
			{ index: 1, answered: false },
			{ index: 2, answered: true }
		]);
		expect(flaggedList([true], undefined)).toEqual([]);
	});

	it('warns about unanswered and flagged questions before submitting', () => {
		expect(submitWarning([true, true], [false, false])).toBe("You can't change your answers afterwards.");
		expect(submitWarning([true, false], [true, false])).toBe("1 question is still unanswered. 1 question is flagged for review. You can't change your answers afterwards.");
		expect(submitWarning([false, false, true], [true, true, true])).toBe("2 questions are still unanswered. 3 questions are flagged for review. You can't change your answers afterwards.");
	});
});
