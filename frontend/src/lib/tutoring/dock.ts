// Where the tutoring room's side panel sits (D-116): docked beside the
// video, floating over it as a window the person drags, or closed. Kept per
// device, so a teacher's arrangement survives a reload.

export type DockMode = 'docked' | 'floating';
export type Dock = { mode: DockMode; width: number; x: number; y: number; h: number };

export const MIN_W = 280;
export const MAX_W = 640;
export const MIN_H = 240;
const KEY = 'qp.tutor.dock';
export const DEFAULT_DOCK: Dock = { mode: 'docked', width: 360, x: -1, y: -1, h: 520 };

const clamp = (n: number, lo: number, hi: number) => Math.min(Math.max(n, lo), Math.max(lo, hi));

// A floating panel stays clear of the controls along the bottom.
export const TOOLBAR_H = 72;

/** Keeps the panel usable in a window of vw × vh: never wider than the room
 * allows, and a floating panel stays whole on the screen, above the
 * controls. A floating panel not placed yet (x < 0) goes to the top right. */
export function fit(d: Dock, vw: number, vh: number): Dock {
	const width = clamp(Math.round(d.width), MIN_W, Math.min(MAX_W, vw - 32));
	const h = clamp(Math.round(d.h), MIN_H, vh - TOOLBAR_H - 16);
	const x = d.x < 0 ? vw - width - 16 : clamp(Math.round(d.x), 0, vw - width);
	const y = clamp(d.y < 0 ? 72 : Math.round(d.y), 8, vh - TOOLBAR_H - h);
	return { mode: d.mode === 'floating' ? 'floating' : 'docked', width, x, y, h };
}

export function loadDock(): Dock {
	try {
		const s = JSON.parse(localStorage.getItem(KEY) ?? 'null');
		if (s && typeof s === 'object') {
			const n = (v: unknown, def: number) => (typeof v === 'number' && Number.isFinite(v) ? v : def);
			return { mode: s.mode === 'floating' ? 'floating' : 'docked', width: n(s.width, DEFAULT_DOCK.width), x: n(s.x, -1), y: n(s.y, -1), h: n(s.h, DEFAULT_DOCK.h) };
		}
	} catch {
		// No storage (private window): the default.
	}
	return { ...DEFAULT_DOCK };
}

export function saveDock(d: Dock) {
	try {
		localStorage.setItem(KEY, JSON.stringify(d));
	} catch {
		// Not kept; it still works for this visit.
	}
}
