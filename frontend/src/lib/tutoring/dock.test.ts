import { afterEach, describe, expect, it } from 'vitest';
import { DEFAULT_DOCK, fit, loadDock, MAX_W, MIN_W, saveDock, TOOLBAR_H } from './dock';

describe('the tutoring room panel', () => {
	it('stays between its narrowest and widest, and inside the window', () => {
		expect(fit({ ...DEFAULT_DOCK, width: 100 }, 1400, 800).width).toBe(MIN_W);
		expect(fit({ ...DEFAULT_DOCK, width: 2000 }, 1400, 800).width).toBe(MAX_W);
		expect(fit({ ...DEFAULT_DOCK, width: 600 }, 500, 800).width).toBe(468);
		expect(fit({ ...DEFAULT_DOCK, width: 600 }, 300, 800).width).toBe(MIN_W); // a narrow window still gets a usable panel
	});

	it('puts a new floating panel in the top right corner', () => {
		const d = fit({ ...DEFAULT_DOCK, mode: 'floating' }, 1400, 800);
		expect(d).toMatchObject({ mode: 'floating', x: 1400 - 360 - 16, y: 72 });
	});

	it('keeps a dragged panel whole on the screen, above the controls', () => {
		const d = fit({ mode: 'floating', width: 360, x: 5000, y: 5000, h: 2000 }, 1400, 800);
		expect(d.x).toBe(1400 - 360);
		expect(d.h).toBe(800 - TOOLBAR_H - 16);
		expect(d.y + d.h).toBe(800 - TOOLBAR_H);
		const low = fit({ mode: 'floating', width: 360, x: 1, y: 700, h: 400 }, 1400, 800);
		expect(low).toMatchObject({ x: 1, h: 400, y: 800 - TOOLBAR_H - 400 }); // pushed up, not shrunk
		expect(fit({ mode: 'floating', width: 360, x: 10, y: 2, h: 400 }, 1400, 800).y).toBe(8);
	});

	afterEach(() => localStorage.clear());

	it('is remembered on this device, and bad data gives the default', () => {
		expect(loadDock()).toEqual(DEFAULT_DOCK);
		saveDock({ mode: 'floating', width: 400, x: 20, y: 30, h: 500 });
		expect(loadDock()).toEqual({ mode: 'floating', width: 400, x: 20, y: 30, h: 500 });
		localStorage.setItem('qp.tutor.dock', '{"mode":"sideways","width":"wide"}');
		expect(loadDock()).toMatchObject({ mode: 'docked', width: DEFAULT_DOCK.width });
		localStorage.setItem('qp.tutor.dock', 'not json');
		expect(loadDock()).toEqual(DEFAULT_DOCK);
	});
});
