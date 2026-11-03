// @vitest-environment jsdom
import { afterEach, describe, expect, test } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import SettingsEditor from './SettingsEditor.svelte';

let app: ReturnType<typeof mount> | null = null;
afterEach(() => {
	if (app) unmount(app);
	app = null;
	document.body.innerHTML = '';
});

describe('settings editor', () => {
	test('a section the teacher opened stays open while they type in it', () => {
		const target = document.createElement('div');
		document.body.append(target);
		app = mount(SettingsEditor, { target, props: { level: 'quiz', value: {}, effective: { blur_grace_ms: 2000, disconnect_grace_sec: 10 }, onsave: () => {} } });
		const integrity = [...target.querySelectorAll('details')].find((d) => d.textContent?.includes('Ignore leaving for less than'))!;
		expect(integrity.open).toBe(false); // a quiz has many settings, so sections start closed
		integrity.open = true;
		integrity.dispatchEvent(new Event('toggle'));
		flushSync();
		const input = [...integrity.querySelectorAll('input')].find((i) => i.placeholder === '2000')!;
		for (const v of ['3', '30', '300']) {
			input.value = v;
			input.dispatchEvent(new Event('input', { bubbles: true }));
			flushSync();
			expect(integrity.open).toBe(true);
		}
		expect(integrity.textContent).toContain('1 set here');
	});
});
