// @vitest-environment jsdom
import { describe, expect, test } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import { BoardState, drawStroke, hits, newGesture, round, thin, type Stroke } from './strokes.svelte';
import Whiteboard from './Whiteboard.svelte';

const s = (over: Partial<Stroke>): Stroke => ({ id: 1, by: 't', gesture: 'g', tool: 'pen', color: '#111827', size: 4, points: [0, 0, 100, 0], ...over });

describe('board state', () => {
	test('applies socket events and ignores echoes of strokes it already has', () => {
		const b = new BoardState();
		b.load({ type: 'board_state', open: true, mode: 'everyone', can_draw: true, me: 'k1', strokes: [s({ id: 1 })] });
		expect(b.apply({ type: 'board', op: 'add', strokes: [s({ id: 1 }), s({ id: 2, by: 'k1' })] })).toBe(false);
		expect(b.strokes.map((x) => x.id)).toEqual([1, 2]);
		expect(b.mine(b.strokes[1])).toBe(true);
		b.apply({ type: 'board', op: 'remove', ids: [1] });
		expect(b.strokes.map((x) => x.id)).toEqual([2]);
		b.apply({ type: 'board', op: 'clear' });
		expect(b.strokes).toEqual([]);
		// Access changes are personal: the view must be fetched again.
		expect(b.apply({ type: 'board', op: 'access' })).toBe(true);
	});
	test('gesture ids are unique and valid for the server', () => {
		const ids = new Set(Array.from({ length: 200 }, newGesture));
		expect(ids.size).toBe(200);
		for (const id of ids) expect(id).toMatch(/^[A-Za-z0-9_-]{1,40}$/);
	});
});

describe('hit-testing for the eraser', () => {
	test('lines, shapes and text', () => {
		expect(hits(s({}), 50, 3, 2)).toBe(true);
		expect(hits(s({}), 50, 30, 2)).toBe(false);
		expect(hits(s({ tool: 'rect', points: [0, 0, 100, 100] }), 100, 50, 3)).toBe(true);
		expect(hits(s({ tool: 'rect', points: [0, 0, 100, 100] }), 50, 50, 3)).toBe(false); // inside an outline isn't on it
		expect(hits(s({ tool: 'ellipse', points: [0, 0, 200, 100] }), 100, 1, 3)).toBe(true);
		expect(hits(s({ tool: 'ellipse', points: [0, 0, 200, 100] }), 100, 50, 3)).toBe(false);
		expect(hits(s({ tool: 'text', points: [10, 10], text: 'Hello' }), 20, 20, 2)).toBe(true);
		expect(hits(s({ tool: 'highlighter', size: 6, points: [0, 0, 100, 0] }), 50, 14, 2)).toBe(true); // wide marker
	});
	test('thinning keeps the ends and drops near-duplicates; rounding stays sub-pixel', () => {
		expect(thin([0, 0, 0.5, 0, 1, 0, 5, 0, 5.2, 0, 9, 9])).toEqual([0, 0, 5, 0, 9, 9]);
		expect(round([1.234, 5.678])).toEqual([1.2, 5.7]);
	});
	test('every tool draws without throwing', () => {
		const calls: string[] = [];
		const ctx = new Proxy({}, { get: (_t, k) => (typeof k === 'string' && !['strokeStyle', 'fillStyle'].includes(k) ? (...a: unknown[]) => calls.push(k) : undefined), set: () => true }) as unknown as CanvasRenderingContext2D;
		for (const tool of ['pen', 'highlighter', 'line', 'arrow', 'rect', 'ellipse', 'text'] as const) drawStroke(ctx, s({ tool, points: tool === 'text' ? [1, 1] : [0, 0, 10, 10], text: 'Hi' }));
		expect(calls).toContain('ellipse');
		expect(calls).toContain('fillText');
		expect(calls).toContain('strokeRect');
	});
});

describe('Whiteboard', () => {
	function render(canDraw: boolean, teacher = false) {
		const b = new BoardState();
		b.load({ type: 'board_state', open: true, mode: canDraw ? 'everyone' : 'teacher', can_draw: canDraw, me: 'k', strokes: [s({ id: 7 })] });
		const target = document.createElement('div');
		document.body.append(target);
		const client = { add: async () => [], erase: async () => [], clear: async () => {} };
		const c = mount(Whiteboard, { target, props: { board: b, client, teacher } });
		flushSync();
		return { target, done: () => (unmount(c), target.remove()) };
	}
	test('view-only shows no tools but still exports', () => {
		const v = render(false);
		expect(v.target.textContent).toContain('View only');
		expect(v.target.querySelector('[aria-label="Pen"]')).toBeNull();
		expect(v.target.textContent).toContain('PNG');
		expect(v.target.querySelector('[role=img]')?.getAttribute('aria-label')).toBe('Whiteboard with 1 mark');
		v.done();
	});
	test('drawers get tools, colours and sizes; only teachers can clear', () => {
		const v = render(true);
		expect(v.target.querySelectorAll('[role=toolbar] button[aria-pressed]').length).toBeGreaterThanOrEqual(8 + 8 + 3);
		v.target.querySelector<HTMLButtonElement>('[aria-label="Rectangle"]')!.click();
		flushSync();
		expect(v.target.querySelector('[aria-label="Rectangle"]')!.getAttribute('aria-pressed')).toBe('true');
		expect(v.target.textContent).not.toContain('Clear');
		v.done();
		const t = render(true, true);
		expect(t.target.textContent).toContain('Clear');
		t.done();
	});
});
