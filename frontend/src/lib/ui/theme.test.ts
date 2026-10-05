// WCAG 2.1 AA contrast for both themes (NFR-14, D-39), read from app.css so a
// colour change that breaks contrast fails the build.
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';

// Read from disk: importing it would run it through Tailwind, which compiles the theme blocks away.
const css = readFileSync('src/app.css', 'utf8').replace(/\r\n/g, '\n');

const hex = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255);
const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const lum = (h: string) => {
	const [r, g, b] = hex(h).map(lin);
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const ratio = (a: string, b: string) => {
	const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
	return (x + 0.05) / (y + 0.05);
};
// daisyUI's soft variants: the colour mixed into base-100 (8%; test at 10% for margin).
const mix = (a: string, b: string, t: number) =>
	'#' + hex(a).map((v, i) => Math.round((v * t + hex(b)[i] * (1 - t)) * 255).toString(16).padStart(2, '0')).join('');

function theme(name: string): Record<string, string> {
	const block = css.split("@plugin 'daisyui/theme'").find((b) => b.includes(`name: '${name}'`));
	if (!block) throw new Error('theme not found: ' + name);
	return Object.fromEntries([...block.matchAll(/--color-([a-z0-9-]+):\s*(#[0-9a-f]{6})/gi)].map((m) => [m[1], m[2].toLowerCase()]));
}
function extra(selector: string): Record<string, string> {
	const i = css.indexOf(selector);
	const block = css.slice(i, css.indexOf('}', i));
	return Object.fromEntries([...block.matchAll(/--color-(muted|field):\s*(#[0-9a-f]{6})/gi)].map((m) => [m[1], m[2].toLowerCase()]));
}

const themes = {
	quiz: { ...theme('quiz'), ...extra(":root,\n[data-theme='quiz']") },
	'quiz-dark': { ...theme('quiz-dark'), ...extra("[data-theme='quiz-dark'] {") }
};

describe.each(Object.entries(themes))('%s theme', (_, t) => {
	test('body and secondary text', () => {
		for (const bg of ['base-100', 'base-200']) {
			expect(ratio(t['base-content'], t[bg]), `text on ${bg}`).toBeGreaterThanOrEqual(4.5);
			expect(ratio(t.muted, t[bg]), `muted on ${bg}`).toBeGreaterThanOrEqual(4.5);
		}
	});
	test('form field borders reach 3:1 (WCAG 1.4.11)', () => {
		for (const bg of ['base-100', 'base-200']) expect(ratio(t.field, t[bg]), `field border on ${bg}`).toBeGreaterThanOrEqual(3);
	});
	for (const k of ['primary', 'secondary', 'accent', 'neutral', 'info', 'success', 'warning', 'error']) {
		test(k, () => {
			expect(ratio(t[`${k}-content`], t[k]), `${k}-content on ${k}`).toBeGreaterThanOrEqual(4.5);
			if (k === 'neutral') return;
			expect(ratio(t[k], t['base-100']), `${k} as text`).toBeGreaterThanOrEqual(4.5);
			expect(ratio(t[k], mix(t[k], t['base-100'], 0.1)), `${k} on soft ${k}`).toBeGreaterThanOrEqual(4.5);
		});
	}
});
