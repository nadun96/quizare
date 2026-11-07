// @vitest-environment jsdom
import { describe, expect, test } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import ChoiceList from './ChoiceList.svelte';
import { markFor } from './toon';

const items = [{ id: 'o1', text: 'Cells' }, { id: 'o2', text: 'Atoms' }];
function render(props: Record<string, unknown>) {
	const target = document.createElement('div');
	const c = mount(ChoiceList, { target, props: { items, label: 'Options', ...props } });
	flushSync();
	return { target, done: () => unmount(c) };
}

describe('options in the poll editor (D-151)', () => {
	test('show the colour and shape people will see', () => {
		const { target, done } = render({ marks: true });
		const chips = [...target.querySelectorAll<HTMLElement>('.toon-chip')];
		expect(chips.length).toBe(2);
		expect(chips[1].style.background).toBeTruthy();
		expect(chips[1].querySelector('path')?.getAttribute('d')).toBe(markFor(1).path);
		expect(target.querySelector('.mark')).toBeNull();
		done();
	});

	test('other lists keep their numbers', () => {
		const { target, done } = render({});
		expect(target.querySelector('.toon-chip')).toBeNull();
		expect(target.querySelector('.mark')?.textContent).toBe('1');
		done();
	});
});
