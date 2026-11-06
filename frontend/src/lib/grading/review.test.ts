import { describe, expect, it } from 'vitest';
import { reviewNote } from './review';

describe('what a student is told about their results (D-140)', () => {
	it('says nothing when nothing is held back', () => {
		expect(reviewNote(undefined)).toBe('');
		expect(reviewNote({})).toBe('');
		expect(reviewNote({ detail: 'answers' })).toBe('');
	});
	it('says what is held back', () => {
		expect(reviewNote({ detail: 'marks' })).toMatch(/score only/);
		expect(reviewNote({ detail: 'correctness' })).toMatch(/without the correct answers/);
	});
	it('says when the review closes, and that it has', () => {
		expect(reviewNote({ detail: 'answers', until: '2026-11-08T10:00:00Z' })).toMatch(/until .*your score stays/);
		expect(reviewNote({ detail: 'marks', until: '2026-11-08T10:00:00Z', closed: true })).toMatch(/^The review time ended/);
	});
});
