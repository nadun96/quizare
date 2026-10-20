import { describe, expect, it } from 'vitest';
import { count } from './words';

describe('count', () => {
	it('uses the singular only for one', () => {
		expect(count(1, 'mark')).toBe('1 mark');
		expect(count(0, 'mark')).toBe('0 marks');
		expect(count(2.5, 'mark')).toBe('2.5 marks');
		expect(count(3, 'child', 'children')).toBe('3 children');
	});
});
