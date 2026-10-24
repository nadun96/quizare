import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PageWatch } from './watch';

function visibility(state: 'visible' | 'hidden') {
	Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
	document.dispatchEvent(new Event('visibilitychange'));
}

describe('PageWatch', () => {
	let times: [number, boolean][];
	let leaves: [number, boolean][];
	let active: boolean;
	let w: PageWatch;
	beforeEach(() => {
		vi.useFakeTimers();
		times = [];
		leaves = [];
		active = true;
		w = new PageWatch({ time: (s, k) => times.push([s, k]), leave: (s, v) => leaves.push([s, v]), active: () => active, idleMs: 60_000 });
		w.start();
	});
	afterEach(() => {
		w.stop();
		Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
		vi.restoreAllMocks();
		vi.useRealTimers();
	});

	it('reports the seconds on screen every 15 seconds', () => {
		vi.advanceTimersByTime(15_000);
		expect(times).toEqual([[15, false]]);
		vi.advanceTimersByTime(4_000);
		w.flush();
		expect(times.at(-1)).toEqual([4, false]);
	});

	it('stops counting when the page is left alone', () => {
		vi.advanceTimersByTime(120_000);
		const total = times.reduce((n, [s]) => n + s, 0);
		expect(total).toBeGreaterThanOrEqual(59);
		expect(total).toBeLessThanOrEqual(61);
		document.dispatchEvent(new Event('keydown'));
		vi.advanceTimersByTime(15_000);
		expect(times.at(-1)![0]).toBeGreaterThanOrEqual(14);
	});

	it('reports leaving the app on return, with the time away, and does not count it', () => {
		vi.advanceTimersByTime(5_000);
		visibility('hidden');
		expect(times).toEqual([[5, true]]); // sent at once: the page may be closed
		vi.advanceTimersByTime(30_000);
		visibility('visible');
		expect(leaves).toEqual([[30, false]]);
		w.flush();
		expect(times.length).toBe(1);
	});

	it('counts a leave through the page’s own link as one', () => {
		w.viaLink();
		vi.advanceTimersByTime(1_000);
		visibility('hidden');
		vi.advanceTimersByTime(10_000);
		visibility('visible');
		expect(leaves).toEqual([[10, true]]);
		vi.advanceTimersByTime(10_000);
		visibility('hidden');
		visibility('visible');
		expect(leaves.at(-1)).toEqual([0, false]);
	});

	it('treats a long blur as leaving, but not a short one or a click into a video', () => {
		vi.spyOn(document, 'hasFocus').mockReturnValue(false);
		window.dispatchEvent(new Event('blur'));
		vi.advanceTimersByTime(1_000);
		window.dispatchEvent(new Event('focus'));
		expect(leaves).toEqual([]);
		const frame = document.createElement('iframe');
		document.body.append(frame);
		frame.focus();
		vi.spyOn(document, 'activeElement', 'get').mockReturnValue(frame);
		window.dispatchEvent(new Event('blur'));
		vi.advanceTimersByTime(5_000);
		window.dispatchEvent(new Event('focus'));
		expect(leaves).toEqual([]);
		vi.spyOn(document, 'activeElement', 'get').mockReturnValue(document.body);
		window.dispatchEvent(new Event('blur'));
		vi.advanceTimersByTime(8_000);
		window.dispatchEvent(new Event('focus'));
		expect(leaves).toEqual([[8, false]]);
		frame.remove();
	});

	it('records nothing once the tutorial is read-only', () => {
		active = false;
		vi.advanceTimersByTime(20_000);
		visibility('hidden');
		visibility('visible');
		expect(times).toEqual([]);
		expect(leaves).toEqual([]);
	});
});
