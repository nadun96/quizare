// WCAG 2.1 AA for every daisyUI theme users can pick (NFR-14, D-48), read
// from the generated files in static/themes, plus the colour maths behind them.
import { describe, expect, test } from 'vitest';
import { readFileSync } from 'node:fs';
import { hex, mix, parse, ratio, readableOn, semantic, textOn } from './colors.js';
import { DAISY_THEMES } from './themes.gen';

const daisyVersion = JSON.parse(readFileSync('node_modules/daisyui/package.json', 'utf8')).version;

/** All --color-* values of a generated theme file; later declarations win, as in CSS. */
function colours(name: string): Record<string, string> {
	const css = readFileSync(`static/themes/${name}.css`, 'utf8');
	expect(css, `${name}.css is from the installed daisyUI (run node scripts/gen-themes.mjs)`).toContain(`daisyUI ${daisyVersion} theme "${name}"`);
	return Object.fromEntries([...css.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/gi)].map((m) => [m[1], m[2].trim()]));
}

describe('colour maths', () => {
	test('contrast matches WCAG', () => {
		expect(ratio('#000000', '#ffffff')).toBeCloseTo(21, 1);
		expect(ratio('#777777', '#ffffff')).toBeCloseTo(4.48, 1);
		expect(hex(parse('#1f5aa6'))).toBe('#1f5aa6');
		expect(hex(parse('oklch(100% 0 0)'))).toBe('#ffffff');
	});
	test('black or white text always reaches 4.5:1, on any colour', () => {
		for (let i = 0; i < 400; i++) {
			const c = '#' + Math.floor(Math.random() * 0xffffff).toString(16).padStart(6, '0');
			expect(ratio(textOn(c), c), c).toBeGreaterThanOrEqual(4.5);
		}
	});
	test('readableOn keeps passing colours and fixes failing ones', () => {
		expect(readableOn('#1f5aa6', ['#ffffff'])).toEqual({ color: '#1f5aa6', adjusted: false });
		const pale = readableOn('#ffd54f', ['#ffffff', '#f3f5f8']); // yellow on white
		expect(pale.adjusted).toBe(true);
		expect(ratio(pale.color, '#ffffff')).toBeGreaterThanOrEqual(4.5);
		const s = semantic('#ffd54f', '#17202b', '#0f151d'); // already fine on dark
		expect(s.adjusted).toBe(false);
		expect(ratio(s.content, s.color)).toBeGreaterThanOrEqual(4.5);
	});
});

test('every installed daisyUI theme has a generated file and picker entry', async () => {
	const installed = Object.keys((await import('daisyui/theme/object.js')).default).sort();
	expect(DAISY_THEMES.map((t) => t.name)).toEqual(installed);
});

describe.each(DAISY_THEMES.map((t) => [t.name]))('daisyUI theme %s', (name) => {
	const t = colours(name);
	test('body and secondary text, field borders', () => {
		for (const bg of ['base-100', 'base-200']) {
			expect(ratio(t['base-content'], t[bg]), `text on ${bg}`).toBeGreaterThanOrEqual(4.5);
			expect(ratio(t.muted, t[bg]), `muted on ${bg}`).toBeGreaterThanOrEqual(4.5);
			expect(ratio(t.field, t[bg]), `field border on ${bg}`).toBeGreaterThanOrEqual(3);
		}
	});
	test('semantic colours as fills, as text and on soft tints', () => {
		for (const k of ['primary', 'secondary', 'accent', 'info', 'success', 'warning', 'error']) {
			expect(ratio(t[`${k}-content`], t[k]), `${k}-content on ${k}`).toBeGreaterThanOrEqual(4.5);
			expect(ratio(t[k], t['base-100']), `${k} as text`).toBeGreaterThanOrEqual(4.5);
			expect(ratio(t[k], mix(t[k], t['base-100'], 0.1)), `${k} on soft ${k}`).toBeGreaterThanOrEqual(4.5);
		}
		expect(ratio(t['neutral-content'], t.neutral), 'neutral').toBeGreaterThanOrEqual(4.5);
	});
});
