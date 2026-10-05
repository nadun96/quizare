// @vitest-environment jsdom
import { describe, expect, test, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import QuestionView from '../QuestionView.svelte';
import type { Response, StudentQuestion } from '../types';

const q = (over: Partial<StudentQuestion>): StudentQuestion => ({ id: 'q1', code: 'Q1', type: 'BLANK_TEXT', text: '', body: {}, marks: 1, resources: [], ...over });

async function render(question: StudentQuestion, value: Response | null = null) {
	const target = document.createElement('div');
	document.body.append(target);
	const changes: Response[] = [];
	const c = mount(QuestionView, { target, props: { question, value, onchange: (r: Response) => changes.push(r) } });
	flushSync();
	// Markdown rendering loads on demand; wait until the text is in.
	await vi.waitFor(() => expect(target.querySelector('.rich')?.innerHTML).toBeTruthy());
	flushSync();
	return { target, changes, done: () => (unmount(c), target.remove()) };
}

describe('QuestionView with rich text', () => {
	test('blank inputs sit inside formatted text and report answers', async () => {
		const v = await render(q({ text: '**Water** boils at [[1]] °C\n\n- at sea level: [[2]]', body: { format: 'markdown', blanks: ['1', '2'] } }), { blanks: { '2': 'yes' } });
		const strong = v.target.querySelector('strong');
		expect(strong?.textContent).toBe('Water');
		const inputs = v.target.querySelectorAll<HTMLInputElement>('.blank-slot input.blank');
		expect(inputs.length).toBe(2);
		expect(v.target.querySelector('li .blank-slot[data-blank="2"] input')).not.toBeNull();
		expect(inputs[1].value).toBe('yes');
		inputs[0].value = '100';
		inputs[0].dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();
		expect(v.changes.at(-1)?.blanks).toEqual({ '1': '100', '2': 'yes' });
		v.done();
	});

	test('BLANK_OPT selects work the same way', async () => {
		const v = await render(q({ type: 'BLANK_OPT', text: 'The [[1]] is *largest*', body: { format: 'markdown', blanks: ['1'], options: [{ id: 'o1', text: 'Jupiter' }, { id: 'o2', text: 'Mars' }] } }));
		const sel = v.target.querySelector<HTMLSelectElement>('.blank-slot select')!;
		expect(sel.options.length).toBe(3);
		sel.value = 'o1';
		sel.dispatchEvent(new Event('change', { bubbles: true }));
		flushSync();
		expect(v.changes.at(-1)?.blanks).toEqual({ '1': 'o1' });
		expect(v.target.querySelector('em')?.textContent).toBe('largest');
		v.done();
	});

	test('plain-text questions render as before', async () => {
		const v = await render(q({ type: 'SINGLE', text: 'What is 5*3*2?\nPick one', body: { options: [{ id: 'o1', text: '30' }, { id: 'o2', text: '10' }] } }));
		const p = v.target.querySelector('.qtext p.plain')!;
		expect(p.textContent).toBe('What is 5*3*2?\nPick one');
		expect(v.target.querySelector('em')).toBeNull();
		v.done();
	});
});
