import { describe, expect, it } from 'vitest';
import { classSignInSeconds } from './signin';

describe('sign-in limits (D-126)', () => {
	it('works out how long a class on one network waits', () => {
		// The old limit: 500 at once, 300 go straight in, the other 200 at 5 a second.
		expect(classSignInSeconds(500, 300, 5, 0)).toBeCloseTo(40, 0);
		// Arriving over 30 s helps, but the bucket can't save up past 300.
		const spread = classSignInSeconds(500, 300, 5, 30);
		expect(spread).toBeGreaterThan(5);
		expect(spread).toBeLessThan(40);
		// The recommended limit: nobody waits.
		expect(classSignInSeconds(500, 600, 20, 30)).toBe(0);
		expect(classSignInSeconds(500, 600, 20, 0)).toBe(0);
		expect(classSignInSeconds(1000, 600, 20, 0)).toBeCloseTo(20, 0);
		expect(classSignInSeconds(10, 0, 5, 0)).toBe(Infinity);
	});
});
