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

import { describe as say, SETTINGS } from './settingsMeta';
const meta = (k: string) => SETTINGS.find((m) => m.key === k)!;
describe('settings in words (D-63)', () => {
	it('keeps "I" and "AI", and reads long times in bigger units', () => {
		expect(say(meta('start_mode'), 'teacher')).toBe('I press Start');
		expect(say(meta('evaluation_method'), 'key')).toBe('answer key');
		expect(say(meta('evaluation_method'), 'llm')).toBe('AI');
		expect(say(meta('quiz_time_limit_sec'), 900)).toBe('15 min');
		expect(say(meta('quiz_time_limit_sec'), 0)).toBe('no limit');
		expect(say(meta('countdown_seconds'), 45)).toBe('45 s');
		expect(say(meta('blur_grace_ms'), 2000)).toBe('2 s');
		expect(say(meta('pass_mark_pct'), 50)).toBe('50%');
		expect(say(meta('one_way_navigation'), true)).toBe('not allowed');
	});
});
