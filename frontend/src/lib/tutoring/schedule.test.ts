import { describe, expect, it } from 'vitest';
import { canJoin, happeningSoon, localInput, monthDays, repeatSummary, type CalendarEntry } from './types';

// Scheduling tutoring sessions in the browser (D-84).

const entry = (over: Partial<CalendarEntry>): CalendarEntry => ({
	id: 'a',
	title: 'Waves',
	classroom_id: 'c',
	classroom_name: 'Physics',
	teacher_name: 'Ms Perera',
	join_code: 'ABC',
	status: 'open',
	duration_min: 60,
	time_zone: 'Asia/Colombo',
	role: 'student',
	...over
});

describe('schedule helpers', () => {
	it('shows a moment as the wall clock in a time zone', () => {
		expect(localInput('2026-11-02T10:00:00Z', 'Asia/Colombo')).toBe('2026-11-02T15:30');
		expect(localInput('2026-11-02T10:00:00Z', 'America/New_York')).toBe('2026-11-02T05:00');
		expect(localInput('2026-11-02T23:59:00Z', 'UTC')).toBe('2026-11-02T23:59');
	});

	it('says how a series repeats', () => {
		expect(repeatSummary({ freq: '' })).toBe('once');
		expect(repeatSummary({ freq: 'weekly', days: [3, 1], count: 5 })).toBe('every week on Mon, Wed, 5 times');
		expect(repeatSummary({ freq: 'daily', interval: 2, until: '2026-12-01' })).toBe('every 2 days, until 2026-12-01');
	});

	it('lays a month out in whole weeks, Sunday first', () => {
		const nov = monthDays(2026, 10); // November 2026 starts on a Sunday, ends on a Monday
		expect(nov[0].getDate()).toBe(1);
		expect(nov.length).toBe(35);
		expect(nov[nov.length - 1].getDay()).toBe(6);
		const feb = monthDays(2027, 1); // 1 February 2027 is a Monday
		expect(feb[0].getMonth()).toBe(0);
		expect(feb[0].getDate()).toBe(31);
		expect(feb.length % 7).toBe(0);
	});

	it('picks what is on now or very soon, and who may go in', () => {
		const now = Date.parse('2026-11-02T10:00:00Z');
		const live = entry({ id: 'live', status: 'live' });
		const opening = entry({ id: 'opening', scheduled_at: '2026-11-02T10:08:00Z', opens_at: '2026-11-02T09:58:00Z' });
		const soonClosed = entry({ id: 'soon', scheduled_at: '2026-11-02T10:12:00Z', opens_at: '2026-11-02T10:12:00Z' });
		const later = entry({ id: 'later', scheduled_at: '2026-11-02T12:00:00Z', opens_at: '2026-11-02T11:50:00Z' });
		const cancelled = entry({ id: 'cancelled', status: 'cancelled', scheduled_at: '2026-11-02T10:05:00Z' });
		expect(happeningSoon([live, opening, soonClosed, later, cancelled], now).map((e) => e.id)).toEqual(['live', 'opening', 'soon']);
		expect(canJoin(opening, now)).toBe(true);
		expect(canJoin(soonClosed, now)).toBe(false);
		expect(canJoin({ ...soonClosed, role: 'teacher' }, now)).toBe(true);
		expect(canJoin(cancelled, now)).toBe(false);
	});
});
