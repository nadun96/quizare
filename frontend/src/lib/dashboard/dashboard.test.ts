import { describe, expect, it } from 'vitest';
import { tone, when } from './dashboard';

describe('dashboard items', () => {
	const now = new Date(2026, 9, 31, 10, 0).getTime(); // 31 Oct, 10:00 local

	it('say when, in plain words', () => {
		const at = (d: number, h = 15) => new Date(2026, 9, d, h, 0).toISOString();
		expect(when({ status: 'due_soon', at: at(32) }, now, 'en-GB')).toBe('Due tomorrow, 15:00');
		expect(when({ status: 'due', at: at(31, 18) }, now, 'en-GB')).toBe('Due today, 18:00');
		expect(when({ status: 'overdue', at: at(30) }, now, 'en-GB')).toBe('Was due yesterday, 15:00');
		expect(when({ status: 'opens', at: at(38) }, now, 'en-GB')).toBe('Opens 7 Nov');
		expect(when({ status: 'missed', at: at(20) }, now, 'en-GB')).toBe('Missed, ended 20 Oct');
		expect(when({ status: 'live' }, now)).toBe('Live now');
		expect(when({ status: 'not_started' }, now)).toBe('Not started');
		expect(when({ status: 'to_mark' }, now)).toBe('To mark');
	});

	it('colour what needs attention', () => {
		expect([tone('live'), tone('missed'), tone('overdue'), tone('due_soon'), tone('opens'), tone('in_progress')]).toEqual(['live', 'bad', 'bad', 'warn', 'info', 'plain']);
	});
});

describe('insights (D-105)', async () => {
	const { compact, delta, deltaText, step } = await import('./insights');
	it('compares with the period before', () => {
		expect(delta({ value: 72.5, prev: 66 })).toBe(6.5);
		expect(delta({ value: 5, prev: null })).toBeNull();
		expect(deltaText(6.5, ' pts')).toBe('+6.5 pts');
		expect(deltaText(-2)).toBe('−2');
		expect(deltaText(0)).toBe('no change');
	});
	it('steps counts onto the five-step ramp, none for zero', () => {
		expect([0, 1, 2, 5, 10].map((n) => step(n, 10))).toEqual([0, 1, 1, 3, 5]);
		expect(step(3, 0)).toBe(0);
	});
	it('shortens big numbers only', () => {
		expect(compact(1284)).toBe('1,284');
		expect(compact(12900)).toMatch(/12\.9K/);
	});
});
