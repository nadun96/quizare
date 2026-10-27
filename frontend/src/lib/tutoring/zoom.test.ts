import { describe, expect, it } from 'vitest';
import { clampPan, MAX_SCALE, NONE, panBy, percent, zoomAt } from './zoom';

describe('zooming into a shared screen', () => {
	it('keeps the point under the pointer in place', () => {
		const z = zoomAt(NONE, 2, 300, 100, 800, 450);
		expect(z).toEqual({ scale: 2, x: -300, y: -100 });
		// The point (300, 100) of the tile still shows the same pixel: (300 - x) / scale.
		expect((300 - z.x) / z.scale).toBe(300);
	});

	it('stays between 100 % and the most, and never leaves a gap', () => {
		expect(zoomAt(NONE, 0.5, 100, 100, 800, 450)).toEqual(NONE);
		expect(zoomAt({ scale: 5, x: 0, y: 0 }, 4, 0, 0, 800, 450).scale).toBe(MAX_SCALE);
		expect(clampPan({ scale: 2, x: 50, y: -2000 }, 800, 450)).toEqual({ scale: 2, x: 0, y: -450 });
		// Zooming out again pulls the picture back over the tile.
		const out = zoomAt({ scale: 3, x: -1600, y: -900 }, 0.5, 0, 0, 800, 450);
		expect(out.x).toBeGreaterThanOrEqual(800 - 800 * out.scale);
	});

	it('pans only while zoomed in, and says the size', () => {
		expect(panBy(NONE, 40, 40, 800, 450)).toEqual(NONE);
		expect(panBy({ scale: 2, x: -100, y: -100 }, -50, 30, 800, 450)).toEqual({ scale: 2, x: -150, y: -70 });
		expect(percent({ scale: 2.5, x: 0, y: 0 })).toBe('250 %');
	});
});
