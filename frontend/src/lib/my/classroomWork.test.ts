import { describe, expect, it } from 'vitest';
import type { CalendarEntry } from '../tutoring/types';
import { action, dateLine, markLine, withTutoring, type ClassroomWork, type WorkItem } from './classroomWork';

const item = (more: Partial<WorkItem>): WorkItem => ({ kind: 'tutorial', id: 'x', title: 'T', classroom: 'C', status: 'not_started', href: '/learn/x', ...more });
const work = (): ClassroomWork => ({ classroom_id: 'c1', name: 'Science', description: '', teacher: 'Ms P', status: 'active', student_number: null, live: [], due: [], upcoming: [], completed: [{ kind: 'quiz', id: 'a', title: 'Old quiz', at: '2026-10-01T09:00:00Z', status: 'submitted', released: true, score: 3, max: 4, pct: 75 }] });
const entry = (more: Partial<CalendarEntry>): CalendarEntry => ({ id: 's', title: 'Lesson', classroom_id: 'c1', classroom_name: 'Science', teacher_name: 'Ms P', join_code: 'ABC', status: 'open', duration_min: 60, time_zone: 'UTC', role: 'student', ...more });

describe('a student’s classroom (D-138)', () => {
	it('says what an item’s date means', () => {
		expect(dateLine({ status: 'overdue', at: '2026-11-01T09:00:00Z' })).toMatch(/^Was due /);
		expect(dateLine({ status: 'due_soon', at: '2026-11-01T09:00:00Z' })).toMatch(/^Due /);
		expect(dateLine({ status: 'opens', at: '2026-11-01T09:00:00Z' })).toMatch(/^Opens /);
		expect(dateLine({ status: 'not_started' })).toBe('');
	});

	it('names the button', () => {
		expect(action(item({ kind: 'quiz', status: 'live', href: '/j/ABC' }))).toBe('Join');
		expect(action(item({ kind: 'quiz', status: 'live', href: '/attempt/1' }))).toBe('Continue');
		expect(action(item({ kind: 'quiz', status: 'missed', href: '/my' }))).toBe('');
		expect(action(item({ status: 'in_progress' }))).toBe('Continue');
		expect(action(item({}))).toBe('Open');
	});

	it('shows a mark only when there is one', () => {
		expect(markLine({ kind: 'quiz', id: 'a', title: '', status: 'submitted', released: true, score: 3, max: 4, pct: 75 })).toBe('3 / 4 (75%)');
		expect(markLine({ kind: 'quiz', id: 'a', title: '', status: 'submitted', released: true, pending: true })).toBe('Still being marked');
		expect(markLine({ kind: 'quiz', id: 'a', title: '', status: 'submitted', released: false })).toBe('Marks not released yet');
		expect(markLine({ kind: 'presentation', id: 'a', title: '', status: 'finished', released: false })).toBe('');
		expect(markLine({ kind: 'tutoring', id: 'a', title: '', status: 'attended', released: false })).toBe('');
	});

	it('adds this classroom’s tutoring sessions, and no other’s', () => {
		const now = Date.parse('2026-11-04T09:00:00Z');
		const w = withTutoring(work(), [
			entry({ id: 'live', status: 'live', started_at: '2026-11-04T08:50:00Z' }),
			entry({ id: 'later', scheduled_at: '2026-11-08T09:00:00Z', opens_at: '2026-11-08T08:45:00Z' }),
			entry({ id: 'other', classroom_id: 'c2', status: 'live' }),
			entry({ id: 'gone', status: 'cancelled' })
		], [{ classroom_id: 'c1', classroom_name: 'Science', sessions: 2, attended: 1, seconds: 60, recent: [{ title: 'Came', started_at: '2026-11-02T09:00:00Z', attended: true, seconds: 60 }, { title: 'Absent', started_at: '2026-11-03T09:00:00Z', attended: false, seconds: 0 }] }], now);
		expect(w.live.map((x) => x.id)).toEqual(['live']);
		expect(w.live[0].href).toBe('/tutor/ABC');
		expect(w.upcoming.map((x) => x.id)).toEqual(['later']);
		expect(w.upcoming[0].href).toBe(''); // not open yet
		expect(w.completed.map((x) => x.title)).toEqual(['Came', 'Old quiz']); // newest first; absent left out
	});
});
