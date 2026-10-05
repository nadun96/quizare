// Word-cloud layout (D-40): sizes by frequency (square-root scale, so one
// very common word doesn't dwarf the rest) and placement along an Archimedean
// spiral from the centre. Deterministic: the same words always give the same
// picture, which keeps a live cloud calm as counts change.
import type { WordCount } from './types';

export type Placed = { w: WordCount; x: number; y: number; size: number; width: number; height: number; rank: number };
export type Measure = (text: string, size: number) => number;

export function layoutCloud(words: WordCount[], width: number, height: number, measure: Measure, opts: { maxWords?: number; minSize?: number; maxSize?: number } = {}): Placed[] {
	if (!width || !height || !words.length) return [];
	const list = [...words].sort((a, b) => b.count - a.count || a.word.localeCompare(b.word)).slice(0, opts.maxWords ?? 80);
	const max = list[0].count;
	const min = list[list.length - 1].count;
	const maxSize = Math.min(opts.maxSize ?? 56, width / 6);
	const minSize = Math.min(opts.minSize ?? 14, maxSize);
	const rects: { x: number; y: number; w: number; h: number }[] = [];
	const out: Placed[] = [];
	const cx = width / 2;
	const cy = height / 2;
	list.forEach((w, rank) => {
		const t = max === min ? 1 : Math.sqrt((w.count - min) / (max - min));
		const size = Math.round(minSize + (maxSize - minSize) * t);
		const tw = measure(w.word, size) + 8;
		const th = size * 1.05 + 4;
		for (let i = 0; i < 1600; i++) {
			const a = i * 0.32;
			const r = 2.2 * a;
			const x = cx + r * Math.cos(a) * 1.6 - tw / 2;
			const y = cy + r * Math.sin(a) - th / 2;
			if (x < 2 || y < 2 || x + tw > width - 2 || y + th > height - 2) continue;
			if (rects.some((o) => x < o.x + o.w && x + tw > o.x && y < o.y + o.h && y + th > o.y)) continue;
			rects.push({ x, y, w: tw, h: th });
			out.push({ w, x: x + tw / 2, y: y + th / 2, size, width: tw, height: th, rank });
			return;
		}
	});
	return out;
}
