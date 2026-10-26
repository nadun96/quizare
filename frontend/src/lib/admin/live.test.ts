import { describe, expect, it } from 'vitest';
import { everyonePause, kindLabel, liveFor, peopleText, summary, type Activity } from './live';

const act = (kind: Activity['kind'], people: number): Activity => ({
	kind, people, id: kind + people, title: 't', status: 'open', teacher_id: 'x', teacher_name: 'X', teacher_email: 'x@e', classroom: '', started_at: ''
});

describe('live activity', () => {
	it('says how long something has been live', () => {
		const now = Date.parse('2026-10-26T12:00:00Z');
		expect(liveFor('2026-10-26T11:56:30Z', now)).toBe('3 min');
		expect(liveFor('2026-10-26T10:00:00Z', now)).toBe('2 h');
		expect(liveFor('2026-10-26T09:55:00Z', now)).toBe('2 h 5 min');
		expect(liveFor('2026-10-22T12:00:00Z', now)).toBe('4 days');
		expect(liveFor('2026-10-26T12:01:00Z', now)).toBe('0 min'); // a clock a little ahead
	});

	it('counts each kind and its people, every kind shown', () => {
		const s = summary([act('poll', 3), act('poll', 4), act('meeting', 2)]);
		expect(s.map((x) => [x.kind, x.count, x.people])).toEqual([['quiz_session', 0, 0], ['poll', 2, 7], ['meeting', 1, 2], ['presentation', 0, 0]]);
	});

	it('names kinds and finds the pause for everyone', () => {
		expect(kindLabel('presentation')).toBe('Live presentation');
		expect(peopleText('quiz_session', 1)).toBe('1 student');
		expect(peopleText('poll', 3)).toBe('3 participants');
		expect(peopleText('meeting', 1)).toBe('1 in it now');
		expect(everyonePause([{ id: '1', teacher_id: 't', reason: '', created_at: '' }])).toBeNull();
		expect(everyonePause([{ id: '1', teacher_id: 't', reason: '', created_at: '' }, { id: '2', teacher_id: null, reason: 'x', created_at: '' }])?.id).toBe('2');
	});
});
