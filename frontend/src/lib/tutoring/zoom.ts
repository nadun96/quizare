// Zooming into a shared screen (TS-FR-14): a scale and an offset, with the
// point under the pointer staying put, and the picture never pulled away
// from the tile's edges. Offsets are in the tile's pixels; the transform is
// translate(x, y) scale(scale) from the top-left corner.

export type Zoom = { scale: number; x: number; y: number };

export const NONE: Zoom = { scale: 1, x: 0, y: 0 };
export const MAX_SCALE = 6;

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/** Keeps the zoomed picture covering the tile: no gap at any edge. */
export function clampPan(z: Zoom, w: number, h: number): Zoom {
	const scale = clamp(z.scale, 1, MAX_SCALE);
	return { scale, x: clamp(z.x, w - w * scale, 0), y: clamp(z.y, h - h * scale, 0) };
}

/** Zooms by factor around the point (px, py) of a w × h tile. */
export function zoomAt(z: Zoom, factor: number, px: number, py: number, w: number, h: number): Zoom {
	const scale = clamp(z.scale * factor, 1, MAX_SCALE);
	const k = scale / z.scale;
	return clampPan({ scale, x: px - (px - z.x) * k, y: py - (py - z.y) * k }, w, h);
}

/** Moves a zoomed picture by (dx, dy). */
export const panBy = (z: Zoom, dx: number, dy: number, w: number, h: number): Zoom => clampPan({ ...z, x: z.x + dx, y: z.y + dy }, w, h);

export const percent = (z: Zoom) => `${Math.round(z.scale * 100)} %`;
