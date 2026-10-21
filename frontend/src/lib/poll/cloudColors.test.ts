import { describe, expect, it } from 'vitest';
import { ratio } from '../ui/colors.js';
import { cloudPalette, colorIndex, wordColor } from './cloudColors';

const CATS = ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#4a3aa7', '#e34948'];

describe('word cloud colours (D-67)', () => {
	it('keeps every colour readable on light and dark backgrounds', () => {
		for (const bgs of [['#ffffff', '#f3f5f8'], ['#17202b', '#0f151d']]) {
			const p = cloudPalette(CATS, bgs);
			for (const c of p.large) for (const bg of bgs) expect(ratio(c, bg)).toBeGreaterThanOrEqual(3);
			for (const c of p.small) for (const bg of bgs) expect(ratio(c, bg)).toBeGreaterThanOrEqual(4.5);
		}
		// Amber is too light on white and is darkened; blue already passes and stays.
		const light = cloudPalette(CATS, ['#ffffff']);
		expect(light.large[3]).not.toBe('#eda100');
		expect(light.large[0]).toBe('#2a78d6');
	});

	it('gives a word the same colour whatever its count or case, and spreads words over the palette', () => {
		const p = cloudPalette(CATS, ['#ffffff']);
		expect(wordColor(p, 'Photosynthesis', 40)).toBe(wordColor(p, 'photosynthesis', 60));
		const used = new Set(['data', 'methods', 'model', 'analysis', 'package', 'variables', 'drug', 'selection', 'regression', 'network', 'tools', 'docker'].map((w) => colorIndex(w, 8)));
		expect(used.size).toBeGreaterThanOrEqual(5);
		expect(wordColor(cloudPalette([], ['#ffffff']), 'x', 20)).toBeUndefined();
	});
});
