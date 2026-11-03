// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import PoolsEditor from './PoolsEditor.svelte';

const info = {
	pools: [{ source: 'bank', category: 'MIS - Fundamentals', count: 5 }],
	quiz: [],
	bank: [{ category: 'MIS - Fundamentals', count: 80, marks: 80 }],
	class: []
};

let app: ReturnType<typeof mount> | null = null;
afterEach(() => {
	if (app) unmount(app);
	app = null;
	document.body.innerHTML = '';
	vi.unstubAllGlobals();
});

describe('pools editor', () => {
	test('the number box stays while the teacher clears it to type another number', async () => {
		vi.stubGlobal('fetch', async () => new Response(JSON.stringify(info), { status: 200, headers: { 'Content-Type': 'application/json' } }));
		const target = document.createElement('div');
		document.body.append(target);
		app = mount(PoolsEditor, { target, props: { quizId: 'q1' } });
		for (let i = 0; i < 20 && !target.querySelector('input[type=number]'); i++) await tick(), await new Promise((r) => setTimeout(r, 5));
		const box = () => target.querySelector<HTMLInputElement>('input[aria-label^="How many from"]');
		const save = () => [...target.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Save pools')!;
		expect(box()?.value).toBe('5');

		box()!.value = '';
		box()!.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();
		expect(box(), 'the box disappeared when emptied').not.toBeNull();
		expect(save().disabled).toBe(true); // empty would save as "all of them"
		expect(target.textContent).toContain('How many? 1 to 80.');

		box()!.value = '11';
		box()!.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();
		expect(save().disabled).toBe(false);
		expect(target.textContent).toContain('Each student gets 11 questions');
	});
});
