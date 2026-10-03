// Browser integrity signals (ADR-08, FR-PR-01/02/06). These are evidence,
// not proof: they report leaving the quiz, they cannot prevent it. iPhone
// Safari has no Fullscreen API for pages, so fullscreen is best-effort only.

export type ViolationKind = 'tab_hidden' | 'window_blur' | 'page_close' | 'fullscreen_exit';

export interface ProctorOptions {
	report: (kind: ViolationKind) => void; // over the WebSocket while open
	beacon: (kind: ViolationKind) => void; // navigator.sendBeacon fallback
	blurGraceMs: () => number; // R-01: ignore very short focus losses
	active: () => boolean; // only while the attempt is running
	win?: Window;
	doc?: Document;
}

export class Proctor {
	private blurTimer: ReturnType<typeof setTimeout> | null = null;
	private wasFullscreen = false;
	private cleanup: (() => void)[] = [];
	private lastSent = new Map<ViolationKind, number>();

	constructor(private o: ProctorOptions) {}

	private send(kind: ViolationKind, viaBeacon = false) {
		if (!this.o.active()) return;
		// One signal can fire several events at once (blur + visibilitychange); dedupe within 1 s.
		const now = Date.now();
		if (now - (this.lastSent.get(kind) ?? 0) < 1000) return;
		this.lastSent.set(kind, now);
		if (viaBeacon) this.o.beacon(kind);
		else this.o.report(kind);
	}

	start() {
		const win = this.o.win ?? window;
		const doc = this.o.doc ?? document;
		const on = <K extends string>(target: EventTarget, type: K, fn: (e: Event) => void, opts?: AddEventListenerOptions) => {
			target.addEventListener(type, fn, opts);
			this.cleanup.push(() => target.removeEventListener(type, fn, opts));
		};
		on(doc, 'visibilitychange', () => {
			if (doc.visibilityState === 'hidden') {
				this.clearBlur();
				// The page may be frozen right after this; use the beacon so the report survives.
				this.send('tab_hidden', true);
			}
		});
		on(win, 'blur', () => {
			this.clearBlur();
			this.blurTimer = setTimeout(() => {
				if (doc.visibilityState !== 'hidden') this.send('window_blur');
			}, this.o.blurGraceMs());
		});
		on(win, 'focus', () => this.clearBlur());
		on(win, 'pagehide', () => this.send('page_close', true));
		on(doc, 'fullscreenchange', () => {
			if (doc.fullscreenElement) this.wasFullscreen = true;
			else if (this.wasFullscreen) this.send('fullscreen_exit');
		});
		// FR-PR-06: no copy, paste or context menu during an attempt.
		for (const type of ['copy', 'cut', 'paste', 'contextmenu']) {
			on(doc, type, (e) => {
				if (this.o.active()) e.preventDefault();
			});
		}
	}

	private clearBlur() {
		if (this.blurTimer) clearTimeout(this.blurTimer);
		this.blurTimer = null;
	}

	stop() {
		this.clearBlur();
		for (const c of this.cleanup.splice(0)) c();
	}
}

/** Enter fullscreen where the device allows it (FR-PR-01). */
export async function enterFullscreen(el: HTMLElement = document.documentElement): Promise<boolean> {
	if (!document.fullscreenEnabled || document.fullscreenElement) return !!document.fullscreenElement;
	try {
		await el.requestFullscreen({ navigationUI: 'hide' });
		return true;
	} catch {
		return false;
	}
}
