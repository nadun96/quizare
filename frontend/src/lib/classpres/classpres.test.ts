import { describe, expect, it } from 'vitest';
import { fromLocalInput, presenterText, ratingText, statusOf, step, taskForm, toLocalInput, type GroupPres } from './classpres';

const pres = (o: Partial<GroupPres> = {}): GroupPres => ({
	id: 'p', task_id: 't', group_id: 'g', group: 'Lions', title: 'Lava', file_name: 'lava.pdf', slides: 3, ready: true, files_removed: false,
	allowed: false, uploaded_by: 'Ben', presented_at: null, mark: null, comment: '', rating: null, ratings: 0, updated_at: '2026-11-10T10:00:00Z', ...o
});

describe('group presentations in a classroom (D-166)', () => {
	it('says who presents', () => {
		expect(presenterText({ presenting: true, presenter_name: 'Ann' })).toBe('You are presenting');
		expect(presenterText({ presenting: false, presenter_name: 'Ann' })).toBe('Ann is presenting');
		expect(presenterText({ presenting: false })).toBe('The teacher is presenting');
	});

	it('steps within the slides', () => {
		expect(step({ slide: 2, slides: 3 }, 1)).toBe(3);
		expect(step({ slide: 3, slides: 3 }, 1)).toBeNull();
		expect(step({ slide: 1, slides: 3 }, -1)).toBeNull();
	});

	it('shows where a group’s presentation stands', () => {
		expect(statusOf(null).label).toBe('Nothing yet');
		expect(statusOf(pres({ ready: false })).label).toBe('Uploading');
		expect(statusOf(pres()).label).toBe('Uploaded');
		expect(statusOf(pres({ allowed: true })).label).toBe('Allowed to present');
		expect(statusOf(pres({ allowed: true, presented_at: '2026-11-10T11:00:00Z' })).label).toBe('Presented');
		expect(statusOf(pres({ files_removed: true, ready: false })).label).toBe('Slides deleted');
	});

	it('shows classmates’ rating', () => {
		expect(ratingText({ rating: null, ratings: 0 })).toBe('');
		expect(ratingText({ rating: 3.666, ratings: 3 })).toBe('3.7 ★ (3)');
	});

	it('fills the task form', () => {
		expect(taskForm()).toEqual({ title: '', instructions: '', topic_id: '', upload_by: 'any', out_of: 0, rating: false, due_at: null });
		const f = taskForm({ title: 'Talks', topic_id: null, upload_by: 'leader', out_of: 20, rating: true, due_at: null, instructions: 'x' } as never);
		expect(f.topic_id).toBe('');
		expect(f.upload_by).toBe('leader');
	});

	it('round-trips a due date through the date input', () => {
		const iso = new Date(2026, 10, 12, 9, 30).toISOString();
		expect(toLocalInput(iso)).toBe('2026-11-12T09:30');
		expect(fromLocalInput('2026-11-12T09:30')).toBe(iso);
		expect(toLocalInput(null)).toBe('');
		expect(fromLocalInput('')).toBeNull();
	});
});
