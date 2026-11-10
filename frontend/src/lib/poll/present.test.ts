import { describe, expect, it } from 'vitest';
import { presenterText, ratingText, step } from './present';

describe('presentations in polls (D-165)', () => {
	const st = { submission_id: 's', title: 'Volcanoes', owner: 'Red', slide: 2, slides: 3, rating: true };

	it('says who presents', () => {
		expect(presenterText({ ...st, presenter_key: 'k1', presenter_name: 'Ann' }, 'k1')).toBe('You are presenting');
		expect(presenterText({ ...st, presenter_key: 'k1', presenter_name: 'Ann' }, 'k2')).toBe('Ann is presenting');
		expect(presenterText(st, 'k2')).toBe('The teacher is presenting');
	});

	it('steps within the slides', () => {
		expect(step(st, 1)).toBe(3);
		expect(step(st, -1)).toBe(1);
		expect(step({ slide: 3, slides: 3 }, 1)).toBeNull();
		expect(step({ slide: 1, slides: 3 }, -1)).toBeNull();
	});

	it('shows the audience rating', () => {
		expect(ratingText({ rating: null, ratings: 0 })).toBe('');
		expect(ratingText({ rating: 3.666, ratings: 3 })).toBe('3.7 ★ (3)');
	});
});
