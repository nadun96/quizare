// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { prefs } from './prefs.svelte';
import { toasts, toast } from './toast.svelte';
import { dialogs, confirmDialog } from './dialog.svelte';
import DialogHost from './DialogHost.svelte';

const html = document.documentElement;

describe('display preferences', () => {
	afterEach(() => {
		localStorage.clear();
		prefs.load();
	});
	test('apply as attributes on <html> and persist', () => {
		prefs.set('theme', 'dark');
		prefs.set('text', 'xl');
		prefs.set('motion', 'reduce');
		expect(html.dataset.theme).toBe('quiz-dark');
		expect(html.dataset.text).toBe('xl');
		expect(html.dataset.motion).toBe('reduce');
		expect(JSON.parse(localStorage.getItem('qp:prefs')!)).toMatchObject({ theme: 'dark', text: 'xl', motion: 'reduce' });
	});
	test('system defaults leave the attributes off', () => {
		prefs.set('theme', 'light');
		expect(html.dataset.theme).toBe('quiz');
		prefs.set('theme', 'system');
		prefs.set('text', 'md');
		expect(html.hasAttribute('data-theme')).toBe(false);
		expect(html.hasAttribute('data-text')).toBe(false);
	});
	test('a broken stored value falls back to defaults', () => {
		localStorage.setItem('qp:prefs', '{not json');
		prefs.load();
		expect(prefs.theme).toBe('system');
		expect(prefs.timer).toBe('shown');
	});
});

describe('toasts', () => {
	test('show, keep at most four, and expire', () => {
		vi.useFakeTimers();
		for (let i = 0; i < 6; i++) toast('t' + i);
		expect(toasts.list.map((t) => t.text)).toEqual(['t2', 't3', 't4', 't5']);
		vi.advanceTimersByTime(4000);
		expect(toasts.list).toEqual([]);
		vi.useRealTimers();
	});
	test('errors stay longer', () => {
		vi.useFakeTimers();
		toast('bad', 'error');
		vi.advanceTimersByTime(4000);
		expect(toasts.list.length).toBe(1);
		vi.advanceTimersByTime(2500);
		expect(toasts.list.length).toBe(0);
		vi.useRealTimers();
	});
});

describe('confirm dialog', () => {
	// jsdom has no showModal; enough for the component to open and close.
	HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
		this.open = true;
	};
	HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
		this.open = false;
	};

	test('resolves true on confirm and false on cancel', async () => {
		const target = document.createElement('div');
		document.body.append(target);
		const c = mount(DialogHost, { target });
		const yes = confirmDialog({ title: 'Delete it?', confirm: 'Delete', danger: true });
		flushSync();
		expect(target.querySelector('#dlg-title')?.textContent).toBe('Delete it?');
		(target.querySelector('.btn-error') as HTMLButtonElement).click();
		await expect(yes).resolves.toBe(true);

		const no = confirmDialog({ title: 'Submit?' });
		flushSync();
		(target.querySelector('.modal-action .btn:not(.btn-primary)') as HTMLButtonElement).click();
		await expect(no).resolves.toBe(false);
		expect(dialogs.current).toBeNull();
		unmount(c);
		target.remove();
	});
	test('a second dialog cancels the first', async () => {
		const first = confirmDialog({ title: 'One' });
		const second = confirmDialog({ title: 'Two' });
		await expect(first).resolves.toBe(false);
		dialogs.close(true);
		await expect(second).resolves.toBe(true);
	});
});
