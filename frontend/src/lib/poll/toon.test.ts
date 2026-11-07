// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { contrast, INK, MARKS, markFor, toonVars } from './toon';
import ToonChoices from './ToonChoices.svelte';

describe('the cartoon look of polls (D-150)', () => {
	it('keeps ink readable on every colour', () => {
		for (const m of MARKS) expect(contrast(INK, m.color), m.name).toBeGreaterThanOrEqual(4.5);
	});

	it('gives every option a colour and shape, more options going round again', () => {
		expect(new Set(MARKS.map((m) => m.color)).size).toBe(MARKS.length);
		expect(new Set(MARKS.map((m) => m.name)).size).toBe(MARKS.length);
		expect(markFor(0)).toBe(MARKS[0]);
		expect(markFor(MARKS.length + 1)).toBe(MARKS[1]);
	});

	it('hands the shapes to CSS as safe image data', () => {
		expect(toonVars).toContain('--toon-8:');
		expect(toonVars).toContain('--shape-1:url("data:image/svg+xml,');
		expect(toonVars.split('--shape-1:')[1].split(';')[0]).not.toContain('<');
	});

	const options = [{ id: 'a', label: 'Cells', n: 3 }, { id: 'b', label: 'Atoms', n: 1 }];
	const render = (props: Record<string, unknown>) => {
		const target = document.createElement('div');
		const c = mount(ToonChoices, { target, props: { options, responses: 4, ...props } });
		flushSync();
		return { target, done: () => unmount(c) };
	};

	it('says each share in words, not only by height', () => {
		const { target, done } = render({});
		const tiles = [...target.querySelectorAll('.tile')].map((t) => t.textContent?.replace(/\s+/g, ' ').trim());
		expect(tiles).toEqual(['Cells 75%, 3 people', 'Atoms 25%, 1 person']);
		expect(target.querySelector('.chart')?.getAttribute('aria-hidden')).toBe('true');
		done();
	});

	it('scales columns to the most answered, and shows an empty one', () => {
		const { target, done } = render({ options: [...options, { id: 'c', label: 'Rocks', n: 0 }] });
		const heights = [...target.querySelectorAll<HTMLElement>('.column')].map((c) => parseFloat(c.style.height));
		expect(heights[0]).toBe(100);
		expect(heights[1]).toBeCloseTo(36);
		expect(heights[2]).toBe(4);
		done();
	});

	it('marks the answer with a tick, in words too', () => {
		const { target, done } = render({ correct: ['a'] });
		const [first, second] = target.querySelectorAll('.tile');
		expect(first.classList.contains('right')).toBe(true);
		expect(first.textContent).toContain('Correct');
		expect(second.textContent).not.toContain('Correct');
		done();
	});

	it('handles no answers yet', () => {
		const { target, done } = render({ responses: 0, options: [{ id: 'a', label: 'Cells', n: 0 }] });
		expect(target.querySelector('.share')?.textContent).toContain('0%');
		done();
	});
});
