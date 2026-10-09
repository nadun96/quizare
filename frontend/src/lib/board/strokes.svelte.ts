// Whiteboard strokes (V2-09, D-47): drawing, hit-testing and export. Every
// coordinate is in board units on a fixed 1600×900 canvas, so a stroke looks
// the same on a phone, a laptop and the projector.

export const BOARD_W = 1600;
export const BOARD_H = 900;

export type Tool = 'pen' | 'highlighter' | 'line' | 'rect' | 'ellipse' | 'arrow' | 'text';
export type Stroke = { id: number; by: string; gesture: string; tool: Tool; color: string; size: number; points: number[]; text?: string };
export type NewStroke = Omit<Stroke, 'id' | 'by'>;
export type BoardView = { type: 'board_state'; open: boolean; mode: string; can_draw: boolean; me: string; strokes: Stroke[]; access?: BoardAccess };
export type BoardAccess = { open: boolean; mode: 'teacher' | 'everyone' | 'selected'; groups: string[]; participants: string[] };
export type BoardEvent = { type: 'board'; op: 'add' | 'remove' | 'clear' | 'access'; strokes?: Stroke[]; ids?: number[]; open?: boolean; upto?: number };

/** Ink colours, chosen to read on the white board (contrast ≥ 3:1 for marks). */
export const INKS: [string, string][] = [
	['#111827', 'Black'], ['#1d4ed8', 'Blue'], ['#b91c1c', 'Red'], ['#15803d', 'Green'],
	['#7e22ce', 'Purple'], ['#c2410c', 'Orange'], ['#0e7490', 'Teal'], ['#ca8a04', 'Yellow']
];
export const SIZES: [number, string][] = [[3, 'Thin'], [6, 'Medium'], [12, 'Thick']];

let seq = 0;
/** A fresh gesture id: joins the pieces of one gesture for undo. */
export function newGesture(): string {
	seq = (seq + 1) % 1e6;
	return Date.now().toString(36) + '-' + seq.toString(36) + '-' + Math.random().toString(36).slice(2, 6);
}

/**
 * The board's state on one screen, kept in sync by socket events (D-52).
 * Events can arrive out of order: a stroke's add can come after its removal
 * (the eraser was quicker than the save's echo) or after a clear. Removed ids
 * are remembered and a clear records the highest id it took, so neither kind
 * of late add brings a stroke back.
 */
export class BoardState {
	/** Replaced, never mutated: strokes can be many, so they aren't deeply reactive. */
	strokes = $state.raw<Stroke[]>([]);
	open = $state(false);
	canDraw = $state(false);
	mode = $state('teacher');
	me = $state('');
	access = $state<BoardAccess | null>(null);
	/** Bumped whenever strokes change other than by appending, so a screen knows to redraw everything. */
	rev = 0;

	private ids = new Set<number>();
	private removed = new Set<number>();
	private clearedUpTo = 0;
	/** Adds seen while the view is being fetched, merged into it on load. */
	private fresh: Stroke[] | null = null;

	private keep = (s: Stroke) => s.id > this.clearedUpTo && !this.removed.has(s.id);
	private set(list: Stroke[], appended = false) {
		if (!appended) {
			this.rev++;
			this.ids = new Set(list.map((s) => s.id));
		}
		this.strokes = list;
	}

	/** Call before fetching the view, so strokes drawn meanwhile aren't lost. */
	fetching() {
		this.fresh = [];
	}
	load(v: BoardView) {
		const snap = v.strokes ?? [];
		const have = new Set(snap.map((s) => s.id));
		const extra = (this.fresh ?? []).filter((s) => !have.has(s.id));
		this.fresh = null;
		const all = [...snap, ...extra].filter(this.keep);
		if (extra.length) all.sort((a, b) => a.id - b.id);
		this.set(all);
		this.open = v.open;
		this.canDraw = v.can_draw;
		this.mode = v.mode;
		this.me = v.me;
		this.access = v.access ?? null;
	}
	/** Applies a socket event; returns true when the view should be fetched again. */
	apply(e: BoardEvent): boolean {
		switch (e.op) {
			case 'add': {
				const strokes = e.strokes ?? [];
				this.fresh?.push(...strokes);
				const fresh = strokes.filter((s) => this.keep(s) && !this.ids.has(s.id));
				if (fresh.length) {
					for (const s of fresh) this.ids.add(s.id);
					this.set([...this.strokes, ...fresh], true);
				}
				return false;
			}
			case 'remove': {
				const gone = e.ids ?? [];
				for (const id of gone) this.removed.add(id);
				if (gone.some((id) => this.ids.has(id))) this.set(this.strokes.filter((s) => !this.removed.has(s.id)));
				return false;
			}
			case 'clear': {
				// Without the server's figure (a local clear), everything known so far.
				const upto = e.upto ?? this.strokes.reduce((m, s) => Math.max(m, s.id), 0);
				this.clearedUpTo = Math.max(this.clearedUpTo, upto);
				for (const id of this.removed) if (id <= this.clearedUpTo) this.removed.delete(id);
				this.set(this.strokes.filter(this.keep));
				return false;
			}
		}
		return true; // access changed: who may draw is personal
	}
	mine(s: Stroke) {
		return s.by === this.me;
	}
	/** Every saved piece of the gestures these strokes belong to. */
	gestures(hit: Iterable<Stroke>): Stroke[] {
		const keys = new Set<string>();
		for (const s of hit) keys.add(s.by + '|' + s.gesture);
		return this.strokes.filter((s) => keys.has(s.by + '|' + s.gesture));
	}
}

/**
 * Sends new strokes one request at a time (D-52). Whatever is drawn while a
 * request is on its way goes in the next one (up to 20 strokes), so pieces of
 * a line are saved in order, and a slow connection sends fewer, larger
 * requests instead of falling behind.
 */
export class StrokeQueue {
	/** Queued or on their way; drawn on the overlay until saved. */
	pending = $state.raw<NewStroke[]>([]);
	private queue: NewStroke[] = [];
	private busy: Promise<void> | null = null;

	constructor(
		private send: (s: NewStroke[]) => Promise<Stroke[]>,
		private saved: (s: Stroke[]) => void,
		private failed: (e: unknown) => void
	) {}

	push(s: NewStroke) {
		this.queue.push(s);
		this.pending = [...this.pending, s];
		this.busy ??= this.run();
	}
	private async run() {
		while (this.queue.length) {
			const batch = this.queue.splice(0, 20);
			try {
				this.saved(await this.send(batch));
			} catch (e) {
				this.failed(e);
			} finally {
				const done = new Set(batch);
				this.pending = this.pending.filter((x) => !done.has(x));
			}
		}
		this.busy = null;
	}
	/** Resolves once everything queued so far has been sent. */
	idle(): Promise<void> {
		return this.busy ?? Promise.resolve();
	}
}

// ---------- drawing ----------

function path(ctx: CanvasRenderingContext2D, p: number[]) {
	ctx.beginPath();
	ctx.moveTo(p[0], p[1]);
	if (p.length === 2) {
		ctx.lineTo(p[0] + 0.01, p[1]); // a dot
		return;
	}
	// Smooth through the midpoints of successive points.
	for (let i = 2; i < p.length - 2; i += 2) {
		const mx = (p[i] + p[i + 2]) / 2;
		const my = (p[i + 1] + p[i + 3]) / 2;
		ctx.quadraticCurveTo(p[i], p[i + 1], mx, my);
	}
	ctx.lineTo(p[p.length - 2], p[p.length - 1]);
}

/** Draws one stroke in board units (the caller scales the context). */
export function drawStroke(ctx: CanvasRenderingContext2D, s: Pick<Stroke, 'tool' | 'color' | 'size' | 'points' | 'text'>) {
	const p = s.points;
	ctx.save();
	ctx.strokeStyle = ctx.fillStyle = s.color;
	ctx.lineWidth = s.size;
	ctx.lineCap = ctx.lineJoin = 'round';
	switch (s.tool) {
		case 'highlighter':
			ctx.globalAlpha = 0.35;
			ctx.lineWidth = s.size * 4;
			ctx.lineCap = 'butt';
			path(ctx, p);
			ctx.stroke();
			break;
		case 'pen':
			path(ctx, p);
			ctx.stroke();
			break;
		case 'line':
		case 'arrow': {
			const [x1, y1, x2, y2] = p;
			ctx.beginPath();
			ctx.moveTo(x1, y1);
			ctx.lineTo(x2, y2);
			ctx.stroke();
			if (s.tool === 'arrow') {
				const a = Math.atan2(y2 - y1, x2 - x1);
				const h = Math.max(14, s.size * 4);
				ctx.beginPath();
				ctx.moveTo(x2, y2);
				ctx.lineTo(x2 - h * Math.cos(a - 0.45), y2 - h * Math.sin(a - 0.45));
				ctx.moveTo(x2, y2);
				ctx.lineTo(x2 - h * Math.cos(a + 0.45), y2 - h * Math.sin(a + 0.45));
				ctx.stroke();
			}
			break;
		}
		case 'rect': {
			const [x1, y1, x2, y2] = p;
			ctx.strokeRect(Math.min(x1, x2), Math.min(y1, y2), Math.abs(x2 - x1), Math.abs(y2 - y1));
			break;
		}
		case 'ellipse': {
			const [x1, y1, x2, y2] = p;
			ctx.beginPath();
			ctx.ellipse((x1 + x2) / 2, (y1 + y2) / 2, Math.abs(x2 - x1) / 2 || 0.5, Math.abs(y2 - y1) / 2 || 0.5, 0, 0, Math.PI * 2);
			ctx.stroke();
			break;
		}
		case 'text': {
			const size = textSize(s.size);
			ctx.font = `600 ${size}px system-ui, sans-serif`;
			ctx.textBaseline = 'top';
			(s.text ?? '').split('\n').forEach((line, i) => ctx.fillText(line, p[0], p[1] + i * size * 1.25));
			break;
		}
	}
	ctx.restore();
}

/** Text tool sizes are bigger than line widths. */
export const textSize = (size: number) => Math.max(28, size * 6);

/** Renders strokes on a white board of the given pixel size. */
export function render(ctx: CanvasRenderingContext2D, strokes: Iterable<Pick<Stroke, 'tool' | 'color' | 'size' | 'points' | 'text'>>, w: number, h: number, background = '#ffffff') {
	ctx.setTransform(1, 0, 0, 1, 0, 0);
	ctx.fillStyle = background;
	ctx.fillRect(0, 0, w, h);
	ctx.setTransform(w / BOARD_W, 0, 0, h / BOARD_H, 0, 0);
	for (const s of strokes) drawStroke(ctx, s);
}

// ---------- hit-testing (eraser) ----------

function segDist(px: number, py: number, x1: number, y1: number, x2: number, y2: number) {
	const dx = x2 - x1, dy = y2 - y1;
	const len = dx * dx + dy * dy;
	const t = len ? Math.max(0, Math.min(1, ((px - x1) * dx + (py - y1) * dy) / len)) : 0;
	return Math.hypot(px - (x1 + t * dx), py - (y1 + t * dy));
}

const boxes = new WeakMap<object, [number, number, number, number]>();
/** A stroke's bounding box in board units, remembered per stroke. */
function box(s: Pick<Stroke, 'tool' | 'size' | 'points' | 'text'>): [number, number, number, number] {
	let b = boxes.get(s);
	if (!b) {
		const p = s.points;
		b = [Infinity, Infinity, -Infinity, -Infinity];
		for (let i = 0; i + 1 < p.length; i += 2) {
			b[0] = Math.min(b[0], p[i]);
			b[1] = Math.min(b[1], p[i + 1]);
			b[2] = Math.max(b[2], p[i]);
			b[3] = Math.max(b[3], p[i + 1]);
		}
		if (s.tool === 'text') {
			// Generous: the exact test below decides.
			const size = textSize(s.size);
			const lines = (s.text ?? '').split('\n');
			b[2] += Math.max(...lines.map((l) => l.length)) * size;
			b[3] += lines.length * size * 1.5;
		}
		boxes.set(s, b);
	}
	return b;
}

/** Whether a stroke passes within r board units of (x, y). */
export function hits(s: Pick<Stroke, 'tool' | 'size' | 'points' | 'text'>, x: number, y: number, r: number): boolean {
	const p = s.points;
	const reach = r + (s.tool === 'highlighter' ? s.size * 2 : s.size / 2);
	const [x1, y1, x2, y2] = box(s);
	if (x < x1 - reach || x > x2 + reach || y < y1 - reach || y > y2 + reach) return false;
	switch (s.tool) {
		case 'pen':
		case 'highlighter':
			if (p.length === 2) return Math.hypot(x - p[0], y - p[1]) <= reach;
			for (let i = 0; i + 3 < p.length; i += 2) if (segDist(x, y, p[i], p[i + 1], p[i + 2], p[i + 3]) <= reach) return true;
			return false;
		case 'line':
		case 'arrow':
			return segDist(x, y, p[0], p[1], p[2], p[3]) <= reach;
		case 'rect': {
			const [x1, y1, x2, y2] = p;
			return [[x1, y1, x2, y1], [x2, y1, x2, y2], [x2, y2, x1, y2], [x1, y2, x1, y1]].some(([a, b, c, d]) => segDist(x, y, a, b, c, d) <= reach);
		}
		case 'ellipse': {
			const cx = (p[0] + p[2]) / 2, cy = (p[1] + p[3]) / 2;
			const rx = Math.abs(p[2] - p[0]) / 2, ry = Math.abs(p[3] - p[1]) / 2;
			for (let i = 0; i < 48; i++) {
				const a1 = (i / 48) * Math.PI * 2, a2 = ((i + 1) / 48) * Math.PI * 2;
				if (segDist(x, y, cx + rx * Math.cos(a1), cy + ry * Math.sin(a1), cx + rx * Math.cos(a2), cy + ry * Math.sin(a2)) <= reach) return true;
			}
			return false;
		}
		case 'text': {
			const size = textSize(s.size);
			const lines = (s.text ?? '').split('\n');
			const w = Math.max(...lines.map((l) => l.length)) * size * 0.58;
			const h = lines.length * size * 1.25;
			return x >= p[0] - r && x <= p[0] + w + r && y >= p[1] - r && y <= p[1] + h + r;
		}
	}
	return false;
}

/** Drops points closer than min board units to the last kept one. */
export function thin(points: number[], min = 1.5): number[] {
	if (points.length <= 4) return points;
	const out = [points[0], points[1]];
	for (let i = 2; i < points.length; i += 2) {
		const lx = out[out.length - 2], ly = out[out.length - 1];
		if (Math.hypot(points[i] - lx, points[i + 1] - ly) >= min || i === points.length - 2) out.push(points[i], points[i + 1]);
	}
	return out;
}

const r1 = (n: number) => Math.round(n * 10) / 10;
/** Board coordinates rounded to 0.1, which is far below a screen pixel. */
export const round = (points: number[]) => points.map(r1);

/** A PNG of the board at 1920×1080 on white. */
export function toPNG(strokes: Stroke[], doc: Document = document): Promise<Blob | null> {
	const c = doc.createElement('canvas');
	c.width = 1920;
	c.height = 1080;
	const ctx = c.getContext('2d');
	if (!ctx) return Promise.resolve(null);
	render(ctx, strokes, c.width, c.height);
	return new Promise((resolve) => c.toBlob((b) => resolve(b), 'image/png'));
}
