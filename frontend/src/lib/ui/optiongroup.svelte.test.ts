// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import OptionGroup from './OptionGroup.svelte';
import SettingsEditor from '../SettingsEditor.svelte';

const host = () => {
	const t = document.createElement('div');
	document.body.append(t);
	return t;
};

describe('OptionGroup (D-63)', () => {
	it('shows the inherited value as a soft default, explains only the chosen option, and moves with the arrow keys', () => {
		let value: string | undefined;
		const target = host();
		const props = $state({
			label: 'Marking',
			value: undefined as string | undefined,
			fallback: 'key',
			options: [
				{ v: 'key', label: 'Answer key', icon: 'check' as const, hint: 'Marked instantly.' },
				{ v: 'llm', label: 'AI', icon: 'sparkles' as const, hint: 'Marked by your AI key.' },
				{ v: 'manual', label: 'By me', disabled: true, why: 'Not today' }
			],
			onchange: (v: string) => {
				value = v;
				props.value = v;
			}
		});
		const c = mount(OptionGroup, { target, props });
		flushSync();
		const radios = () => [...target.querySelectorAll<HTMLButtonElement>('[role=radio]')];
		expect(radios()[0].getAttribute('aria-checked')).toBe('true');
		expect(radios()[0].classList.contains('soft')).toBe(true);
		expect(radios()[0].textContent).toContain('default');
		expect(target.textContent).toContain('Marked instantly.');
		expect(target.textContent).not.toContain('Marked by your AI key.');

		radios()[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
		flushSync();
		expect(value).toBe('llm');
		expect(radios()[1].classList.contains('soft')).toBe(false);
		expect(target.textContent).toContain('Marked by your AI key.');
		// The disabled option is skipped and says why.
		radios()[1].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
		flushSync();
		expect(value).toBe('key');
		expect(radios()[2].getAttribute('aria-label')).toBe('By me (Not today)');
		unmount(c);
		target.remove();
	});
});

describe('SettingsEditor (D-63)', () => {
	it('summarises each section, saves only after a change, and puts a value back to the default', async () => {
		const saved: Record<string, unknown>[] = [];
		const target = host();
		const c = mount(SettingsEditor, {
			target,
			props: { level: 'teacher', value: { countdown_seconds: 45 }, effective: { countdown_seconds: 45, start_mode: 'countdown', evaluation_method: 'key', feedback_mode: 'none' }, onsave: (o: Record<string, unknown>) => void saved.push(o) }
		});
		flushSync();
		const titles = [...target.querySelectorAll('.s-title')].map((e) => e.textContent);
		expect(titles).toEqual(['Starting', 'Marking and feedback']);
		expect(target.querySelector('.s-sum')!.textContent).toBe('Countdown: 45 s; Start: on admission');
		expect(target.textContent).toContain('1 set here');
		const save = [...target.querySelectorAll('button')].find((b) => b.textContent === 'Save settings')!;
		expect(save.disabled).toBe(true);

		// Pick "I press Start", then save.
		[...target.querySelectorAll<HTMLButtonElement>('[role=radio]')].find((b) => b.textContent?.includes('I press Start'))!.click();
		flushSync();
		expect(save.disabled).toBe(false);
		// The countdown back to the default.
		[...target.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim() === 'Use the default')!.click();
		flushSync();
		target.querySelector('form')!.requestSubmit();
		await Promise.resolve();
		expect(saved).toEqual([{ start_mode: 'teacher' }]);
		unmount(c);
		target.remove();
	});
});
