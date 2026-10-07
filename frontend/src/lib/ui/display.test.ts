// @vitest-environment jsdom
// Display preferences added in D-48: daisyUI themes, the user's main colour,
// button labels; plus the icon button and the prompt dialog.
import { afterEach, describe, expect, test } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import { ratio } from './colors.js';
import DialogHost from './DialogHost.svelte';
import { dialogs, promptDialog } from './dialog.svelte';
import IconBtn from './IconBtn.svelte';
import { daisyTheme, prefs, themeValue } from './prefs.svelte';

const html = document.documentElement;
afterEach(() => {
	localStorage.clear();
	prefs.load();
});

describe('themes', () => {
	test('a daisyUI theme loads its own stylesheet and sets data-theme', () => {
		prefs.set('theme', 'cupcake');
		expect(html.dataset.theme).toBe('cupcake');
		expect(document.getElementById('qp-theme')?.getAttribute('href')).toBe('/themes/cupcake.css');
		prefs.set('theme', 'light'); // ours again: the extra stylesheet goes
		expect(html.dataset.theme).toBe('quiz');
		expect(document.getElementById('qp-theme')).toBeNull();
	});
	test("daisyUI's own light and dark don't clash with ours", () => {
		expect(themeValue('light')).toBe('daisy-light');
		expect(themeValue('cupcake')).toBe('cupcake');
		expect(daisyTheme('light')).toBeUndefined();
		expect(daisyTheme('daisy-dark')?.name).toBe('dark');
		prefs.set('theme', 'daisy-dark');
		expect(html.dataset.theme).toBe('dark');
	});
	test('unknown stored themes and colours fall back to defaults', () => {
		localStorage.setItem('qp:prefs', JSON.stringify({ theme: 'no-such-theme', color: 'red', labels: 'sometimes' }));
		prefs.load();
		expect(prefs.theme).toBe('system');
		expect(prefs.color).toBe('');
		expect(prefs.labels).toBe('hover');
	});
});

describe('main colour', () => {
	test('a custom colour sets primary and a readable text colour', () => {
		html.style.setProperty('--color-base-100', '#ffffff');
		html.style.setProperty('--color-base-200', '#f3f5f8');
		prefs.set('color', '#ffd54f'); // pale yellow: too light on white
		const primary = html.style.getPropertyValue('--color-primary');
		const content = html.style.getPropertyValue('--color-primary-content');
		expect(prefs.colorAdjusted).toBe(true);
		expect(ratio(primary, '#ffffff')).toBeGreaterThanOrEqual(4.5);
		expect(ratio(content, primary)).toBeGreaterThanOrEqual(4.5);
		prefs.set('color', '');
		expect(html.style.getPropertyValue('--color-primary')).toBe('');
	});
});

describe('IconBtn', () => {
	function render(props: Record<string, unknown>) {
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(IconBtn, { target, props: props as never });
		flushSync();
		return { target, done: () => (unmount(c), target.remove()) };
	}
	test('hover mode: icon only, label in a daisyUI tooltip and the accessible name', () => {
		const v = render({ icon: 'pencil', label: 'Rename', hint: 'Rename Team 1' });
		const btn = v.target.querySelector('button')!;
		expect(btn.getAttribute('aria-label')).toBe('Rename Team 1');
		expect(btn.textContent?.trim()).toBe('');
		expect(v.target.querySelector('.tooltip')?.getAttribute('data-tip')).toBe('Rename Team 1');
		v.done();
	});
	test('always mode: the label is shown', () => {
		prefs.set('labels', 'always');
		const v = render({ icon: 'pencil', label: 'Rename', hint: 'Rename Team 1' });
		expect(v.target.querySelector('button')!.textContent?.trim()).toBe('Rename');
		expect(v.target.querySelector('.tooltip')).toBeNull();
		expect(html.dataset.labels).toBe('always');
		v.done();
	});
});

describe('promptDialog', () => {
	test('resolves to the typed text, or null when cancelled; empty answers wait', async () => {
		const target = document.createElement('div');
		document.body.append(target);
		HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) { this.open = true; };
		HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) { this.open = false; };
		const c = mount(DialogHost, { target });
		const p = promptDialog({ title: 'Rename team', label: 'Team name', value: 'Owls' });
		await tick();
		flushSync();
		const input = target.querySelector<HTMLInputElement>('#dlg-field')!;
		expect(input.value).toBe('Owls');
		dialogs.value = '';
		dialogs.close(true); // empty: stays open
		expect(dialogs.current).not.toBeNull();
		dialogs.value = 'Foxes';
		dialogs.close(true);
		expect(await p).toBe('Foxes');
		const q = promptDialog({ title: 'Rename', value: 'x' });
		await tick();
		dialogs.close(false);
		expect(await q).toBeNull();
		unmount(c);
		target.remove();
	});
});
