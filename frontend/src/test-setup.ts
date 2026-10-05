// jsdom has no matchMedia; Svelte's prefersReducedMotion needs one at import.
if (typeof window !== 'undefined' && !window.matchMedia) {
	window.matchMedia = (query: string) =>
		({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false }) as unknown as MediaQueryList;
}

// jsdom has no Web Animations; Svelte transitions call element.animate().
if (typeof Element !== 'undefined' && !Element.prototype.animate) {
	Element.prototype.animate = function () {
		const a = { onfinish: null as null | (() => void), cancel() {}, finish() {}, play() {}, pause() {}, currentTime: 0, playState: 'finished' };
		queueMicrotask(() => a.onfinish?.());
		return a as unknown as Animation;
	};
}
