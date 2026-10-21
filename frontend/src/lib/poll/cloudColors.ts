// Colours for the word cloud (D-67): the categorical palette (validated for
// colour-blind separation), with each colour's lightness nudged until it
// reads on the current theme's background. Large words need 3:1, smaller
// ones 4.5:1 (WCAG 1.4.3). A word's colour comes from the word itself, so it
// keeps its colour while counts change and words move.
import { readableOn } from '../ui/colors.js';

/** Large text for WCAG: 18 pt (24 px). The cloud's semibold (600) isn't
 * "bold", so the 14 pt bold threshold doesn't apply (D-68). */
export const LARGE_TEXT_PX = 24;

export type CloudPalette = { large: string[]; small: string[] };

export function cloudPalette(colors: string[], backgrounds: string[]): CloudPalette {
	const usable = colors.filter(Boolean);
	return {
		large: usable.map((c) => readableOn(c, backgrounds, 3).color),
		small: usable.map((c) => readableOn(c, backgrounds, 4.5).color)
	};
}

/** A stable palette slot for a word (FNV-1a hash, case-insensitive). */
export function colorIndex(word: string, n: number): number {
	let h = 0x811c9dc5;
	for (const ch of word.toLowerCase()) {
		h ^= ch.codePointAt(0)!;
		h = Math.imul(h, 0x01000193) >>> 0;
	}
	return n > 0 ? h % n : 0;
}

export function wordColor(p: CloudPalette, word: string, size: number): string | undefined {
	const list = size >= LARGE_TEXT_PX ? p.large : p.small;
	return list.length ? list[colorIndex(word, list.length)] : undefined;
}
