// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from 'vitest';
import { flushSync, mount, tick, unmount } from 'svelte';
import { auth } from '../session.svelte';
import Avatar from '../ui/Avatar.svelte';
import AccountSecurity from './AccountSecurity.svelte';

const calls: { method: string; url: string; body?: unknown; type?: string }[] = [];
afterEach(() => {
	vi.unstubAllGlobals();
	calls.length = 0;
	auth.user = null;
});
function stub(handler: (url: string, init: RequestInit) => Response) {
	vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
		calls.push({ method: init.method ?? 'GET', url, body: typeof init.body === 'string' ? JSON.parse(init.body) : init.body, type: (init.headers as Record<string, string>)?.['Content-Type'] });
		return handler(url, init);
	});
}
const json = (x: unknown, status = 200) => new Response(JSON.stringify(x), { status, headers: { 'Content-Type': 'application/json' } });
function render() {
	auth.user = { id: 'u1', email: 'a@x', name: 'Amaya', role: 'student', status: 'active', email_verified: true };
	const target = document.createElement('div');
	document.body.append(target);
	const c = mount(AccountSecurity, { target });
	flushSync();
	return { target, done: () => (unmount(c), target.remove()) };
}
function pick(target: HTMLElement, file: File) {
	const input = target.querySelector<HTMLInputElement>('input[type=file]')!;
	Object.defineProperty(input, 'files', { value: [file], configurable: true });
	input.dispatchEvent(new Event('change', { bubbles: true }));
}

describe('Avatar', () => {
	test('initial without a picture; a versioned image with one', () => {
		const target = document.createElement('div');
		const a = mount(Avatar, { target, props: { id: 'u1', name: 'amaya', size: 40 } });
		flushSync();
		expect(target.textContent?.trim()).toBe('A');
		unmount(a);
		const b = mount(Avatar, { target, props: { id: 'u1', name: 'amaya', avatar: 'abc123' } });
		flushSync();
		expect(target.querySelector('img')?.getAttribute('src')).toBe('/api/auth/users/u1/avatar?v=abc123');
		target.querySelector('img')!.dispatchEvent(new Event('error'));
		flushSync();
		expect(target.querySelector('img')).toBeNull(); // falls back to the initial
		unmount(b);
	});
});

describe('profile picture', () => {
	test('too big or not an image: refused before uploading', async () => {
		stub(() => json({}));
		const v = render();
		pick(v.target, new File([new Uint8Array(600 * 1024)], 'big.png', { type: 'image/png' }));
		await tick();
		expect(v.target.textContent).toContain('the limit is 512 KB');
		pick(v.target, new File(['<svg/>'], 'x.svg', { type: 'image/svg+xml' }));
		await tick();
		expect(v.target.textContent).toContain('PNG, JPEG, WebP or GIF');
		expect(calls).toHaveLength(0);
		v.done();
	});
	test('a valid picture is uploaded as-is and the header updates', async () => {
		stub(() => json({ avatar: 'v2' }));
		const v = render();
		pick(v.target, new File([new Uint8Array(1000)], 'me.jpg', { type: 'image/jpeg' }));
		await vi.waitFor(() => expect(auth.user?.avatar).toBe('v2'));
		expect(calls[0]).toMatchObject({ method: 'PUT', url: '/api/auth/me/avatar', type: 'image/jpeg' });
		v.done();
	});
	test('server errors are shown', async () => {
		stub(() => json({ code: 'invalid', message: 'invalid', fields: { avatar: 'use a PNG, JPEG, WebP or GIF picture' } }, 422));
		const v = render();
		pick(v.target, new File([new Uint8Array(10)], 'me.png', { type: 'image/png' }));
		await vi.waitFor(() => expect(v.target.querySelector('[role=alert]')?.textContent).toContain('use a PNG'));
		v.done();
	});
});

describe('change password', () => {
	const type = (el: HTMLInputElement, value: string) => {
		el.value = value;
		el.dispatchEvent(new Event('input', { bubbles: true }));
	};
	test('mismatched passwords disable saving; success clears the form', async () => {
		stub(() => new Response(null, { status: 204 }));
		const v = render();
		const [cur, next, again] = ['#pw-cur', '#pw-new', '#pw-again'].map((s) => v.target.querySelector<HTMLInputElement>(s)!);
		const button = [...v.target.querySelectorAll('button')].find((b) => b.textContent?.includes('Change password'))!;
		type(cur, 'old password');
		type(next, 'a new password');
		type(again, 'a new passwrd');
		flushSync();
		expect(button.disabled).toBe(true);
		expect(v.target.textContent).toContain("don't match");
		type(again, 'a new password');
		flushSync();
		expect(button.disabled).toBe(false);
		v.target.querySelector('form:last-of-type')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
		await vi.waitFor(() => expect(calls).toHaveLength(1));
		expect(calls[0]).toMatchObject({ method: 'POST', url: '/api/auth/me/password', body: { current_password: 'old password', new_password: 'a new password' } });
		await vi.waitFor(() => expect(cur.value).toBe(''));
		v.done();
	});
	test('a wrong current password is shown on its field', async () => {
		stub(() => json({ code: 'invalid', message: 'invalid', fields: { current_password: "that isn't your current password" } }, 422));
		const v = render();
		type(v.target.querySelector<HTMLInputElement>('#pw-cur')!, 'guess');
		type(v.target.querySelector<HTMLInputElement>('#pw-new')!, 'a new password');
		type(v.target.querySelector<HTMLInputElement>('#pw-again')!, 'a new password');
		flushSync();
		v.target.querySelector('form:last-of-type')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
		await vi.waitFor(() => expect(v.target.querySelector('#pw-cur-e')?.textContent).toBe("That isn't your current password."));
		expect(v.target.querySelector('#pw-cur')!.getAttribute('aria-invalid')).toBe('true');
		v.done();
	});
});
