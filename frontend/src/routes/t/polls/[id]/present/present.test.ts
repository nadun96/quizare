// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import Present from './+page.svelte';

const sockets = vi.hoisted(() => [] as { onMessage: (m: unknown) => void }[]);
vi.mock('$app/state', () => ({ page: { params: { id: 'p1' } } }));
vi.mock('$lib/guard.svelte', () => ({ requireRole: () => () => true }));
vi.mock('$lib/socket', () => ({
	LiveSocket: class {
		onMessage = (_m: unknown) => {};
		onStatus = (_c: boolean) => {};
		constructor() {
			sockets.push(this);
		}
		open() {}
		close() {}
	}
}));
vi.mock('$lib/QrCode.svelte', async () => ({ default: (await import('$lib/ui/Skeleton.svelte')).default }));

const question = (id: string, text: string) => ({ id, type: 'ESSAY', text, body: {} });
const results = (questions: unknown[], more: Record<string, unknown> = {}) => ({
	type: 'results',
	server_time: Date.now(),
	participants: 3,
	results: {},
	poll: { id: 'p1', title: 'Poll', status: 'open', pacing: 'self', join_url: 'http://x/p/ABC', join_code: 'ABC', show_results: 'always', scoring: false, identity: 'anonymous', current_index: 0, questions, ...more }
});
const settle = async () => {
	for (let i = 0; i < 4; i++) {
		await tick();
		await new Promise((r) => setTimeout(r));
	}
	flushSync();
};
const three = [question('a', 'First'), question('b', 'Second'), question('c', 'Third')];

afterEach(() => {
	sockets.length = 0;
	document.body.innerHTML = '';
});

async function render(questions: unknown[]) {
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(Present, { target });
	await settle();
	sockets[0].onMessage(results(questions));
	await settle();
	const button = (text: string) => [...target.querySelectorAll('button')].find((b) => b.textContent?.includes(text))!;
	return { target, button, done: () => unmount(c) };
}

describe('moving through a self-paced poll on the presenter screen', () => {
	test('Next, Previous and the dots move, and stay moved', async () => {
		const { target, button, done } = await render(three);
		expect(target.textContent).toContain('Question 1 of 3');
		button('Next').click();
		await settle();
		expect(target.textContent).toContain('Question 2 of 3');
		button('Next').click();
		await settle();
		expect(target.textContent).toContain('Question 3 of 3');
		button('Previous').click();
		await settle();
		expect(target.textContent).toContain('Question 2 of 3');
		target.querySelector<HTMLButtonElement>('button[aria-label="Question 1"]')!.click();
		await settle();
		expect(target.textContent).toContain('Question 1 of 3');
		done();
	});

	test('the arrow keys move too', async () => {
		const { target, done } = await render(three);
		document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
		await settle();
		expect(target.textContent).toContain('Question 2 of 3');
		done();
	});

	test('a question added earlier doesn’t change the one on screen (V2-05)', async () => {
		const { target, button, done } = await render(three);
		button('Next').click();
		await settle();
		sockets[0].onMessage(results([question('new', 'Inserted'), ...three]));
		await settle();
		expect(target.textContent).toContain('Question 3 of 4');
		expect(target.textContent).toContain('Second');
		done();
	});
});
