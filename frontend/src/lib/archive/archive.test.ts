import { describe, expect, it } from 'vitest';
import { deleteDialog, fromTutoring, kindInfo, KINDS } from './archive';

describe('finished activities and the archive (D-149)', () => {
	it('covers every kind of activity', () => {
		expect(KINDS.map((k) => k.v)).toEqual(['quiz', 'poll', 'meeting', 'presentation', 'tutoring']);
		expect(kindInfo('meeting').label).toBe('Meeting');
	});

	it('says what goes before anything is deleted', () => {
		expect(deleteDialog('quiz', 'Term test').body).toMatch(/marks/);
		expect(deleteDialog('meeting', 'Help').body).toMatch(/attended/);
		expect(deleteDialog('tutoring', 'Extra').body).toMatch(/recordings/);
		for (const k of KINDS) {
			const d = deleteDialog(k.v, 'X');
			expect(d.title).toContain('X');
			expect(d.body).toMatch(/can’t be undone/);
			expect(d.danger).toBe(true);
		}
	});

	it('shows a tutoring session like the rest, from when it ran', () => {
		const base = { id: 's1', title: 'Extra', classroom_id: 'c1', classroom_name: 'Science', status: 'ended', created_at: '2026-01-01T00:00:00Z' };
		const ran = fromTutoring({ ...base, started_at: '2026-01-02T00:00:00Z', ended_at: '2026-01-02T01:00:00Z' });
		expect(ran).toMatchObject({ kind: 'tutoring', classroom: 'Science', started_at: '2026-01-02T00:00:00Z', ended_at: '2026-01-02T01:00:00Z', detail: '' });
		const off = fromTutoring({ ...base, status: 'cancelled', scheduled_at: '2026-02-01T00:00:00Z', cancelled_at: '2026-01-20T00:00:00Z' });
		expect(off).toMatchObject({ started_at: '2026-02-01T00:00:00Z', detail: 'Cancelled' });
	});
});
