// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import RowMenu from './RowMenu.svelte';

describe('RowMenu', () => {
	it('runs the picked action and closes; Esc and a click elsewhere close it', () => {
		const rename = vi.fn();
		const del = vi.fn();
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(RowMenu, { target, props: { hint: 'More actions for Term 1', actions: [{ label: 'Rename', onclick: rename }, { label: 'Delete module', danger: true, onclick: del }] } });
		flushSync();
		const details = target.querySelector('details')!;
		const summary = target.querySelector('summary')!;
		expect(summary.getAttribute('aria-label')).toBe('More actions for Term 1');

		details.open = true;
		const buttons = [...target.querySelectorAll<HTMLButtonElement>('ul button')];
		expect(buttons.map((b) => b.textContent?.trim())).toEqual(['Rename', 'Delete module']);
		expect(buttons[1].classList.contains('text-error')).toBe(true);
		buttons[0].click();
		expect(rename).toHaveBeenCalledOnce();
		expect(del).not.toHaveBeenCalled();
		expect(details.open).toBe(false);

		details.open = true;
		document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
		expect(details.open).toBe(false);

		details.open = true;
		document.body.click();
		expect(details.open).toBe(false);
		unmount(c);
		target.remove();
	});
});
