// Keeps the app translatable (D-157): a word added to a screen without tr(), or a
// phrase missing from the list translators work from, fails here. To fix either,
// run `npm run i18n`, which wraps new words and refreshes the list.
// @vitest-environment node
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { bareWords } from '../../../scripts/i18n/wrap-lib.mjs';
import { allMessages } from '../../../scripts/i18n/catalog.mjs';
import list from './catalog/en.json';
import si from './catalog/si.json';
import ta from './catalog/ta.json';

const SRC = join(process.cwd(), 'src');
function svelteFiles(dir: string): string[] {
	return readdirSync(dir).flatMap((f) => {
		const p = join(dir, f);
		if (statSync(p).isDirectory()) return /[\\/]docs$/.test(p) ? [] : svelteFiles(p);
		return f.endsWith('.svelte') ? [p] : [];
	});
}

describe('the app stays translatable (D-157)', () => {
	it('has no bare words on any screen', () => {
		const found = svelteFiles(SRC).flatMap((f) => bareWords(readFileSync(f, 'utf8')).map((w) => `${f.split(/[\\/]src[\\/]/)[1]}: ${w}`));
		expect(found).toEqual([]);
	});
	it('lists every phrase for translators', () => {
		const known = new Set(list as string[]);
		expect(allMessages().filter((m) => !known.has(m))).toEqual([]);
	});
});

describe('translations stay usable (D-157)', () => {
	const marks = (s: string) => new Set(s.match(/\{\d+\}/g) ?? []);
	for (const [name, phrases] of Object.entries({ si, ta }) as [string, Record<string, string>][]) {
		it(`${name} keeps each phrase's placeholders`, () => {
			const wrong = Object.entries(phrases).filter(([en, v]) => {
				const has = marks(v);
				const needed = [...marks(en)].filter((m) => !new RegExp('[A-Za-z]' + m.replace(/[{}]/g, '\\$&')).test(en));
				return needed.some((m) => !has.has(m)) || [...has].some((m) => !marks(en).has(m));
			});
			expect(wrong).toEqual([]);
		});
	}
});
