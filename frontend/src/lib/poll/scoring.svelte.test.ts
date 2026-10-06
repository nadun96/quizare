// @vitest-environment jsdom
import { describe, expect, test } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import KeyEditor from './KeyEditor.svelte';
import Leaderboard from './Leaderboard.svelte';
import PollResults from './PollResults.svelte';
import { cleanKey, describeKey, ordinal, SCORABLE, secondsLeft } from './scoring';
import type { PollKey, PollQuestion, Rank } from './types';

const opts = [{ id: 'o1', text: 'Paris' }, { id: 'o2', text: 'Rome' }, { id: 'o3', text: 'Oslo' }];
const single: PollQuestion = { id: 'q1', poll_id: 'p', position: 0, type: 'SINGLE', text: 'Capital of France?', body: { options: opts }, required: false, points: 100 };

function host() {
	const target = document.createElement('div');
	document.body.append(target);
	return target;
}

describe('scoring helpers', () => {
	test('opinion types are not scorable', () => {
		for (const t of ['ESSAY', 'WORD_CLOUD', 'RATING', 'LIKERT', 'MATRIX', 'FILE'] as const) expect(SCORABLE.has(t)).toBe(false);
		expect(SCORABLE.has('MULTI')).toBe(true);
	});
	test('cleanKey drops stale parts and empty keys', () => {
		expect(cleanKey('SINGLE', { options: opts }, { correct: ['o9', 'o2', 'o3'] })).toEqual({ correct: ['o2'] });
		expect(cleanKey('MULTI', { options: opts }, { correct: ['o9'] })).toBeNull();
		expect(cleanKey('ESSAY', {}, { accepted: ['x'] })).toBeNull();
		expect(cleanKey('BLANK_TEXT', { blanks: ['1'] }, { blanks: { '1': [' cat ', ''], '2': ['dog'] }, case_sensitive: true })).toEqual({ blanks: { '1': ['cat'] }, case_sensitive: true });
		expect(cleanKey('DRAG', { options: opts }, { order: ['o3', 'o1'] })).toBeNull(); // a ranking must cover every item
		expect(cleanKey('NUMBER', {}, { value: 9.8, tolerance: -0.1 })).toEqual({ value: 9.8, tolerance: 0.1 });
		expect(cleanKey('SHORT_TEXT', {}, { accepted: ['  ', 'Paris'] })).toEqual({ accepted: ['Paris'] });
	});
	test('describeKey says the answer in words', () => {
		expect(describeKey(single, { correct: ['o1'] })).toBe('Paris');
		expect(describeKey({ ...single, type: 'NUMBER', body: { unit: 'm/s²' } }, { value: 9.8, tolerance: 0.1 })).toBe('9.8 ± 0.1 m/s²');
		expect(describeKey({ ...single, type: 'DRAG' }, { order: ['o3', 'o1', 'o2'] })).toBe('1. Oslo, 2. Paris, 3. Rome');
	});
	test('secondsLeft and ordinal', () => {
		expect(secondsLeft(1000, 20, 1000 + 4500)).toBe(16);
		expect(secondsLeft(1000, 20, 99_000)).toBe(0);
		expect(secondsLeft(undefined, 20, 0)).toBeNull();
		expect(secondsLeft(1000, null, 0)).toBeNull();
		expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 101, 111].map(ordinal)).toEqual(['1st', '2nd', '3rd', '4th', '11th', '12th', '13th', '21st', '22nd', '101st', '111th']);
	});
});

describe('KeyEditor', () => {
	test('choosing the right option fills the key', () => {
		const target = host();
		const props = $state({ type: 'MULTI' as const, body: { options: opts }, key: {} as PollKey });
		const c = mount(KeyEditor, { target, props });
		flushSync();
		const boxes = target.querySelectorAll<HTMLInputElement>('input[type=checkbox]');
		expect(boxes).toHaveLength(3);
		boxes[0].click();
		boxes[2].click();
		flushSync();
		expect(props.key.correct).toEqual(['o1', 'o3']);
		boxes[0].click();
		flushSync();
		expect(props.key.correct).toEqual(['o3']);
		unmount(c);
		target.remove();
	});
});

describe('Leaderboard', () => {
	const ranks: Rank[] = Array.from({ length: 12 }, (_, i) => ({ rank: i + 1, key: 'k' + i, name: 'P' + i, score: 1000 - i * 50, correct: 10 - i, answered: 10 }));
	test('shows the top 10, marks you, and pins your place below', () => {
		const target = host();
		const c = mount(Leaderboard, { target, props: { ranks, me: ranks[11], meKey: 'k11' } });
		flushSync();
		expect(target.querySelectorAll('li')).toHaveLength(10);
		expect(target.textContent).toContain("You're 12th");
		expect(target.textContent).toContain('and 2 more');
		unmount(c);
		const c2 = mount(Leaderboard, { target, props: { ranks, me: ranks[1], meKey: 'k1' } });
		flushSync();
		expect(target.querySelector('li.me')?.textContent).toContain('P1');
		expect(target.textContent).not.toContain("You're");
		unmount(c2);
		target.remove();
	});
});

describe('PollResults with a key', () => {
	test('marks the correct option in words, not only colour', () => {
		const target = host();
		const c = mount(PollResults, { target, props: { question: single, result: { question_id: 'q1', type: 'SINGLE', responses: 3, counts: { o1: 2, o2: 1 } }, answerKey: { correct: ['o1'] } } });
		flushSync();
		const rows = target.querySelectorAll('li.bar-row');
		expect(rows[0].classList.contains('correct')).toBe(true);
		expect(rows[0].getAttribute('aria-label')).toContain('(correct)');
		expect(rows[1].classList.contains('correct')).toBe(false);
		unmount(c);
		target.remove();
	});
});
