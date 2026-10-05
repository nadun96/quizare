// @vitest-environment jsdom
import { describe, expect, test } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { hasAnswer, TYPE_META, TYPES_BY_GROUP } from './meta';
import PollInput from './PollInput.svelte';
import PollResults from './PollResults.svelte';
import type { PollAnswer, PollBody, PollQuestion, PollResult, PollType } from './types';
import { layoutCloud } from './wordcloud';

const ALL: PollType[] = ['SINGLE', 'MULTI', 'MATCH', 'BLANK_OPT', 'BLANK_TEXT', 'DRAG', 'ESSAY', 'SHORT_TEXT', 'WORD_CLOUD', 'NUMBER', 'DATE', 'TIME', 'RATING', 'SLIDER', 'LIKERT', 'MATRIX', 'FILE', 'AUDIO', 'VIDEO', 'CODE'];

function q(type: PollType, body: PollBody = TYPE_META[type].body(), text = TYPE_META[type].text): PollQuestion {
	// Give choices ids like the server does.
	const ids = (cs: { id: string; text: string }[] | undefined, p: string) => cs?.map((c, i) => ({ ...c, id: c.id || p + (i + 1) }));
	const b = { ...body, options: ids(body.options, 'o'), left: ids(body.left, 'l'), right: ids(body.right, 'r'), zones: ids(body.zones, 'z'), rows: ids(body.rows, 'row'), columns: ids(body.columns, 'col') };
	b.blanks = [...text.matchAll(/\[\[(\d{1,2})\]\]/g)].map((m) => m[1]);
	return { id: 'q-' + type, poll_id: 'p', position: 0, type, text, body: b, required: false };
}

function render(question: PollQuestion, value?: PollAnswer) {
	const target = document.createElement('div');
	document.body.append(target);
	const changes: PollAnswer[] = [];
	const c = mount(PollInput, { target, props: { question, value, onchange: (a: PollAnswer) => changes.push(a), upload: async () => {} } });
	flushSync();
	return { target, changes, done: () => (unmount(c), target.remove()) };
}

describe('poll metadata', () => {
	test('every type has a label, group and starter body', () => {
		expect(Object.keys(TYPE_META).sort()).toEqual([...ALL].sort());
		expect(TYPES_BY_GROUP.flatMap((g) => g.types).sort()).toEqual([...ALL].sort());
	});
	test('hasAnswer matches what the server treats as empty', () => {
		expect(hasAnswer(q('SINGLE'), { selected: [] })).toBe(false);
		expect(hasAnswer(q('SINGLE'), { selected: ['o1'] })).toBe(true);
		expect(hasAnswer(q('ESSAY'), { text: '   ' })).toBe(false);
		expect(hasAnswer(q('RATING'), { number: 0 })).toBe(true);
		expect(hasAnswer(q('MATRIX', { rows: [], columns: [], mode: 'text' }), { cells: {} })).toBe(false);
		expect(hasAnswer(q('AUDIO'), { file: { id: 'f', name: 'a', size: 1, content_type: 'audio/webm' } })).toBe(true);
	});
});

describe('every input renders', () => {
	for (const t of ALL) {
		test(t, () => {
			const v = render(q(t));
			expect(v.target.querySelector('.poll-input')).not.toBeNull();
			expect(v.target.textContent?.length).toBeGreaterThan(0);
			v.done();
		});
	}
});

describe('inputs report answers', () => {
	test('star rating', () => {
		const v = render(q('RATING'));
		(v.target.querySelectorAll<HTMLButtonElement>('[role=radio]')[3]).click();
		flushSync();
		expect(v.changes.at(-1)).toEqual({ number: 4 });
		v.done();
	});
	test('word cloud chips', () => {
		const v = render(q('WORD_CLOUD', { max_entries: 2 }));
		const input = v.target.querySelector<HTMLInputElement>('input')!;
		for (const w of ['Fun', 'fun', 'fast']) {
			input.value = w;
			input.dispatchEvent(new Event('input', { bubbles: true }));
			input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
			flushSync();
		}
		// Case-insensitive duplicates are ignored; the limit stops a third entry.
		expect(v.changes.map((c) => c.words)).toEqual([['Fun'], ['Fun', 'fast']]);
		v.done();
		const full = render(q('WORD_CLOUD', { max_entries: 2 }), { words: ['a', 'b'] });
		expect(full.target.querySelector<HTMLInputElement>('input')!.disabled).toBe(true);
		full.done();
	});
	test('likert and matrix', () => {
		const v = render(q('LIKERT'));
		(v.target.querySelectorAll<HTMLInputElement>('input[type=radio]')[3]).click();
		flushSync();
		expect(v.changes.at(-1)).toEqual({ rows: { row1: '4' } });
		v.done();
		const m = render(q('MATRIX', { rows: [{ id: '', text: 'A' }], columns: [{ id: '', text: 'x' }, { id: '', text: 'y' }], mode: 'multi' }));
		const boxes = m.target.querySelectorAll<HTMLInputElement>('input[type=checkbox]');
		boxes[0].click();
		flushSync();
		expect(m.changes.at(-1)).toEqual({ multi: { row1: ['col1'] } });
		m.done();
	});
	test('dropdown single choice and dates', () => {
		const v = render(q('SINGLE', { options: [{ id: '', text: 'a' }, { id: '', text: 'b' }], display: 'dropdown' }));
		const sel = v.target.querySelector<HTMLSelectElement>('select')!;
		sel.value = 'o2';
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		flushSync();
		expect(v.changes.at(-1)).toEqual({ selected: ['o2'] });
		v.done();
		const d = render(q('DATE'));
		const di = d.target.querySelector<HTMLInputElement>('input[type=date]')!;
		di.value = '2026-10-05';
		di.dispatchEvent(new Event('change', { bubbles: true }));
		flushSync();
		expect(d.changes.at(-1)).toEqual({ date: '2026-10-05' });
		d.done();
	});
});

describe('word cloud layout', () => {
	const measure = (t: string, s: number) => t.length * s * 0.55;
	const words = Array.from({ length: 40 }, (_, i) => ({ word: 'word' + i, count: 40 - i }));
	test('no overlaps, inside the frame, biggest first', () => {
		const p = layoutCloud(words, 800, 440, measure);
		expect(p.length).toBeGreaterThanOrEqual(25); // words that can't fit are left out
		for (const a of p) {
			expect(a.x - a.width / 2).toBeGreaterThanOrEqual(0);
			expect(a.y + a.height / 2).toBeLessThanOrEqual(440);
			for (const b of p) {
				if (a === b) continue;
				const overlap = Math.abs(a.x - b.x) * 2 < a.width + b.width && Math.abs(a.y - b.y) * 2 < a.height + b.height;
				expect(overlap, `${a.w.word} overlaps ${b.w.word}`).toBe(false);
			}
		}
		const sizes = p.map((x) => x.size);
		expect(sizes[0]).toBe(Math.max(...sizes));
	});
	test('deterministic', () => {
		expect(layoutCloud(words, 600, 330, measure)).toEqual(layoutCloud([...words].reverse(), 600, 330, measure));
	});
});

describe('results', () => {
	function results(question: PollQuestion, result: PollResult, teacher = false) {
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(PollResults, { target, props: { question, result, teacher } });
		flushSync();
		return { target, done: () => (unmount(c), target.remove()) };
	}
	test('choice bars show shares and a table view', () => {
		const sq = q('SINGLE');
		const v = results(sq, { question_id: sq.id, type: 'SINGLE', responses: 4, counts: { o1: 3, o2: 1 } });
		expect(v.target.textContent).toContain('75%');
		(v.target.querySelector('button[aria-pressed]') as HTMLButtonElement).click();
		flushSync();
		expect(v.target.querySelector('table')).not.toBeNull();
		v.done();
	});
	test('likert diverging bar centres on neutral', () => {
		const lq = q('LIKERT');
		const v = results(lq, { question_id: lq.id, type: 'LIKERT', responses: 4, grid: { row1: { '1': 2, '5': 2 } }, row_means: { row1: 3 } });
		const bar = v.target.querySelector<HTMLElement>('.lk-bar')!;
		expect(bar.style.marginLeft).toBe('0%'); // half disagree → starts at the far left
		expect(v.target.querySelectorAll('.seg').length).toBe(2);
		v.done();
	});
	test('participants never see the file list', () => {
		const fq = q('FILE');
		const r: PollResult = { question_id: fq.id, type: 'FILE', responses: 1, files: [{ participant_id: 'x', file: { id: 'f', name: 'a.png', size: 10, content_type: 'image/png' }, at: 1 }] };
		const pub = results(fq, r);
		expect(pub.target.querySelector('.files')).toBeNull();
		pub.done();
		const t = results(fq, r, true);
		expect(t.target.querySelector('.files a[download]')).not.toBeNull();
		t.done();
	});
});

describe('question editor', () => {
	test('every type opens with a working preview', async () => {
		const { default: Editor } = await import('./PollQuestionEditor.svelte');
		for (const t of ALL) {
			const target = document.createElement('div');
			document.body.append(target);
			const c = mount(Editor, { target, props: { pollId: 'p', question: null, onsaved: () => {}, oncancel: () => {} } });
			flushSync();
			const card = [...target.querySelectorAll<HTMLButtonElement>('.type-card')].find((b) => b.textContent?.includes(TYPE_META[t].label));
			expect(card, t).toBeDefined();
			card!.click();
			flushSync();
			expect(target.querySelector('form.editor'), t).not.toBeNull();
			expect(target.querySelector('.preview .poll-input'), t).not.toBeNull();
			unmount(c);
			target.remove();
		}
	});
});
