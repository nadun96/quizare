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
