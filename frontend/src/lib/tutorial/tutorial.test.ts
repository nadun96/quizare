import { describe, expect, it } from 'vitest';
import { BLOCKS, changed, checkpointNote, CHECKPOINTS, duration, emptyBlock, FEEDBACK, fromLocalInput, LEAVING, leavingNote, move, progressText, toLocalInput, type QuestionView } from './tutorial';

const q = (id: string, more: Partial<QuestionView> = {}): QuestionView => ({ id, code: id, type: 'SINGLE', text: '', body: {}, marks: 1, resources: [], checked: false, locked: false, tries: 0, ...more });

describe('tutorials', () => {
	it('moves pages and blocks, and ignores moves off the ends', () => {
		expect(move(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c']);
		expect(move(['a', 'b', 'c'], 2, -1)).toEqual(['a', 'c', 'b']);
		const list = ['a', 'b'];
		expect(move(list, 0, -1)).toBe(list);
		expect(move(list, 1, 1)).toBe(list);
	});

	it('says what a checkpoint needs, until it is passed', () => {
		const page = { checkpoint: 'correct' as const, max_tries: 2, passed: false, questions: [q('a')] };
		expect(checkpointNote(page)).toBe('Get the answers right to go on (2 tries).');
		expect(checkpointNote({ ...page, max_tries: 1 })).toMatch(/1 try\)/);
		expect(checkpointNote({ ...page, checkpoint: 'answered' })).toMatch(/Answer every question/);
		expect(checkpointNote({ ...page, passed: true })).toBe('');
		expect(checkpointNote({ ...page, questions: [] })).toBe('');
		expect(checkpointNote({ ...page, checkpoint: 'none' })).toBe('');
	});

	it('sends only answers that changed and can still change', () => {
		const qs = [q('new'), q('same', { checked: true, response: { selected: ['o1'] } }), q('edited', { checked: true, response: { selected: ['o1'] } }), q('locked', { locked: true })];
		const out = changed(qs, { new: { selected: ['o2'] }, same: { selected: ['o1'] }, edited: { selected: ['o2'] }, locked: { selected: ['o1'] } });
		expect(Object.keys(out).sort()).toEqual(['edited', 'new']);
	});

	it('describes progress for the student list', () => {
		const t = { id: '1', title: 'T', classroom: 'C', page_count: 5, started: false, reached: 0, finished: false };
		expect(progressText(t)).toBe('5 pages');
		expect(progressText({ ...t, page_count: 1 })).toBe('1 page');
		expect(progressText({ ...t, started: true, reached: 2 })).toBe('Page 3 of 5');
		expect(progressText({ ...t, started: true, finished: true })).toBe('Finished');
	});

	it('round-trips dates through the date input', () => {
		const iso = fromLocalInput('2026-11-02T09:30');
		expect(iso).toBeTruthy();
		expect(toLocalInput(iso)).toBe('2026-11-02T09:30');
		expect(toLocalInput(undefined)).toBe('');
		expect(fromLocalInput('')).toBeUndefined();
	});

	it('offers every kind of block, checkpoint and feedback timing', () => {
		expect(BLOCKS.map((b) => b.type)).toEqual(['text', 'image', 'video', 'document', 'link']);
		expect(CHECKPOINTS.map((c) => c.v)).toEqual(['none', 'answered', 'correct']);
		expect(FEEDBACK.map((f) => f.v)).toEqual(['page', 'end', 'release']);
		expect(emptyBlock('text')).toEqual({ type: 'text', text: '' });
		expect(emptyBlock('video').url).toBe('');
	});

	it('tells students about leaving the app before they do', () => {
		const p = { leaving: 'allowed' as const, lock_after: 0, leaves: 0, locked: false };
		expect(leavingNote(p)).toBe('');
		expect(leavingNote({ ...p, leaving: 'recorded' })).toMatch(/recorded for your teacher/);
		expect(leavingNote({ ...p, leaving: 'blocked' })).not.toMatch(/locks/);
		expect(leavingNote({ ...p, leaving: 'blocked', lock_after: 3 })).toMatch(/After 3 times, the page locks\./);
		expect(leavingNote({ ...p, leaving: 'blocked', lock_after: 3, leaves: 2 })).toMatch(/\(1 left\)/);
		expect(leavingNote({ ...p, leaving: 'blocked', lock_after: 1, locked: true })).toMatch(/^This page is locked/);
		expect(LEAVING.map((l) => l.v)).toEqual(['allowed', 'recorded', 'blocked']);
	});

	it('writes durations briefly', () => {
		expect(duration(undefined)).toBe('—');
		expect(duration(45)).toBe('45 s');
		expect(duration(185)).toBe('3 min 05 s');
		expect(duration(4800)).toBe('1 h 20 min');
	});
});
