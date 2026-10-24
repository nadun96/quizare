// Time on a tutorial page and leaving the app (D-75, TU-FR-22, TU-FR-34).
// Like proctoring (lib/proctor.ts), this is evidence, not proof: a page
// can't stop anyone leaving. Time counts only while the page is on screen
// and in use; a page left alone stops counting after a few idle minutes.

export interface WatchOptions {
	/** Report seconds on the page; keepalive when the page may be closing. */
	time: (seconds: number, keepalive: boolean) => void;
	/** Report coming back after leaving the app. */
	leave: (awaySeconds: number, viaLink: boolean) => void;
	/** Whether anything is recorded now (not once the tutorial is read-only). */
	active: () => boolean;
	idleMs?: number;
	blurGraceMs?: number;
	flushMs?: number;
	now?: () => number;
	win?: Window;
	doc?: Document;
}

const LINK_MS = 3000; // a leave this soon after clicking one of the page's links went through it

export class PageWatch {
	private ms = 0; // counted, not yet reported
	private lastTick = 0;
	private lastInput = 0;
	private awayAt: number | null = null;
	private awayVia = false;
	private linkAt = -Infinity;
	private blurTimer: ReturnType<typeof setTimeout> | null = null;
	private timers: ReturnType<typeof setInterval>[] = [];
	private cleanup: (() => void)[] = [];

	constructor(private o: WatchOptions) {}

	private now() {
		return this.o.now?.() ?? Date.now();
	}
	private get doc() {
		return this.o.doc ?? document;
	}
	private get idleMs() {
		return this.o.idleMs ?? 5 * 60_000;
	}

	/** In a video or document: the frame has focus, and the student is using it. */
	private inFrame() {
		return this.doc.activeElement?.tagName === 'IFRAME';
	}

	start() {
		const win = this.o.win ?? window;
		const doc = this.doc;
		const on = (target: EventTarget, type: string, fn: () => void, opts?: AddEventListenerOptions) => {
			target.addEventListener(type, fn, opts);
			this.cleanup.push(() => target.removeEventListener(type, fn, opts));
		};
		this.lastTick = this.lastInput = this.now();
		const input = () => (this.lastInput = this.now());
		for (const t of ['pointerdown', 'keydown', 'wheel', 'touchstart', 'scroll']) on(doc, t, input, { passive: true, capture: true });
		on(doc, 'visibilitychange', () => (doc.visibilityState === 'hidden' ? this.away(this.now()) : this.back()));
		on(win, 'blur', () => {
			if (this.inFrame()) return; // clicked into a video or document
			this.clearBlur();
			const at = this.now();
			this.blurTimer = setTimeout(() => {
				if (doc.visibilityState !== 'hidden' && !doc.hasFocus()) this.away(at);
			}, this.o.blurGraceMs ?? 2000);
		});
		on(win, 'focus', () => {
			this.clearBlur();
			this.back();
		});
		on(win, 'pagehide', () => this.flush(true));
		this.timers.push(setInterval(() => this.tick(), 1000));
		this.timers.push(setInterval(() => this.flush(false), this.o.flushMs ?? 15_000));
	}

	private tick() {
		const now = this.now();
		const dt = Math.min(now - this.lastTick, 5000);
		this.lastTick = now;
		if (this.awayAt !== null || this.doc.visibilityState === 'hidden' || !this.o.active()) return;
		if (now - this.lastInput > this.idleMs && !this.inFrame()) return;
		this.ms += dt;
	}

	private away(at: number) {
		if (this.awayAt !== null) return;
		this.tick();
		this.awayAt = at;
		this.awayVia = at - this.linkAt < LINK_MS;
		this.flush(true); // the page may be frozen or closed now
	}

	private back() {
		if (this.awayAt === null) return;
		const secs = Math.round((this.now() - this.awayAt) / 1000);
		const via = this.awayVia;
		this.awayAt = null;
		this.lastTick = this.lastInput = this.now();
		if (this.o.active()) this.o.leave(secs, via);
	}

	/** The student clicked one of the page's own links: the leave that follows is allowed. */
	viaLink() {
		this.linkAt = this.now();
	}

	/** Reports the time counted so far, for the page the student is on now. */
	flush(keepalive = false) {
		this.tick();
		const secs = Math.floor(this.ms / 1000);
		if (secs < 1 || !this.o.active()) return;
		this.ms -= secs * 1000;
		this.o.time(secs, keepalive);
	}

	private clearBlur() {
		if (this.blurTimer) clearTimeout(this.blurTimer);
		this.blurTimer = null;
	}

	stop() {
		this.flush(true);
		this.clearBlur();
		for (const t of this.timers.splice(0)) clearInterval(t);
		for (const c of this.cleanup.splice(0)) c();
	}
}
