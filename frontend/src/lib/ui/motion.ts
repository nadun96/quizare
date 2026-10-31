// Motion with GSAP (D-108), always honouring reduced motion (WCAG 2.3.3,
// D-39): the OS setting or the in-app "Reduce motion" preference turns every
// animation off, leaving things where they end.
//
// Two kinds: Svelte transitions (for things that come and go, which Svelte
// must time to remove them), eased with GSAP's curves; and actions that run
// GSAP tweens on what is already there: a page arriving, cards cascading in,
// numbers counting, bars growing, lines drawing, the nav's marker gliding.
import { gsap } from 'gsap';
import { prefersReducedMotion } from 'svelte/motion';
import { fade, fly, scale, slide, type FadeParams, type FlyParams, type ScaleParams, type SlideParams } from 'svelte/transition';
import { prefs } from './prefs.svelte';

export const reduced = () => prefersReducedMotion.current || prefs.motion === 'reduce';
const ms = (d: number | undefined, def: number) => (reduced() ? 0 : (d ?? def));

/** GSAP's curves, as functions Svelte transitions take. */
export const ease = {
	out: gsap.parseEase('power3.out'),
	inOut: gsap.parseEase('power2.inOut'),
	back: gsap.parseEase('back.out(1.4)')
};

export const fadeIn = (node: Element, p: FadeParams = {}) => fade(node, { easing: ease.out, ...p, duration: ms(p.duration, 180) });
export const flyIn = (node: Element, p: FlyParams = {}) => fly(node, { y: 8, easing: ease.out, ...p, duration: ms(p.duration, 240) });
export const scaleIn = (node: Element, p: ScaleParams = {}) => scale(node, { start: 0.96, easing: ease.back, ...p, duration: ms(p.duration, 220) });
export const slideIn = (node: Element, p: SlideParams = {}) => slide(node, { easing: ease.inOut, ...p, duration: ms(p.duration, 220) });
/** For animate:flip on reordering lists. */
export const flipMs = () => (reduced() ? 0 : 250);

type Kill = { destroy(): void };
const done = (t?: gsap.core.Animation | null): Kill => ({ destroy: () => void t?.kill() });

/** A page arriving: up a little and in. */
export function pageIn(node: HTMLElement): Kill {
	if (reduced()) return done();
	return done(gsap.fromTo(node, { y: 10, autoAlpha: 0 }, { y: 0, autoAlpha: 1, duration: 0.35, ease: 'power3.out', clearProps: 'transform,opacity,visibility' }));
}

/** Children (or those matching selector) cascading in, one after another. */
export function cascade(node: HTMLElement, opts: { selector?: string; y?: number; stagger?: number; delay?: number } = {}): Kill {
	if (reduced()) return done();
	const items = opts.selector ? node.querySelectorAll(opts.selector) : node.children;
	if (!items.length) return done();
	return done(gsap.from(items, { y: opts.y ?? 14, autoAlpha: 0, duration: 0.45, ease: 'power3.out', stagger: opts.stagger ?? 0.06, delay: opts.delay ?? 0, clearProps: 'transform,opacity,visibility' }));
}

/** A number counting from where it was to its value (from 0 the first time). */
export function countTo(node: HTMLElement, value: number): { update(v: number): void; destroy(): void } {
	const fmt = (n: number) => (Number.isInteger(value) ? Math.round(n) : Math.round(n * 10) / 10).toLocaleString();
	const state = { n: 0 };
	let tween: gsap.core.Tween | null = null;
	const run = (to: number) => {
		tween?.kill();
		if (reduced()) {
			state.n = to;
			node.textContent = fmt(to);
			return;
		}
		tween = gsap.to(state, { n: to, duration: 0.9, ease: 'power2.out', onUpdate: () => void (node.textContent = fmt(state.n)) });
	};
	run(value);
	return { update: (v: number) => run(v), destroy: () => void tween?.kill() };
}

/** Bars and columns growing from their baseline: [data-grow="y"] upwards, [data-grow="x"] rightwards. */
export function grow(node: Element, key?: unknown): { update(k: unknown): void; destroy(): void } {
	let tl: gsap.core.Timeline | null = null;
	const run = () => {
		tl?.kill();
		if (reduced()) return;
		tl = gsap.timeline();
		const ys = node.querySelectorAll('[data-grow="y"]');
		const xs = node.querySelectorAll('[data-grow="x"]');
		if (ys.length) tl.from(ys, { scaleY: 0, transformOrigin: '50% 100%', duration: 0.6, ease: 'power3.out', stagger: 0.04 }, 0);
		if (xs.length) tl.from(xs, { scaleX: 0, transformOrigin: '0% 50%', duration: 0.7, ease: 'power3.out', stagger: 0.06 }, 0);
	};
	run();
	return { update: (k: unknown) => k !== key && ((key = k), run()), destroy: () => void tl?.kill() };
}

/** Lines drawing themselves from left to right ([data-draw] paths), then their dots popping in ([data-pop]). */
export function draw(node: Element, key?: unknown): { update(k: unknown): void; destroy(): void } {
	let tl: gsap.core.Timeline | null = null;
	const run = () => {
		tl?.kill();
		if (reduced()) return;
		tl = gsap.timeline();
		node.querySelectorAll<SVGPathElement>('path[data-draw]').forEach((p) => {
			const len = p.getTotalLength?.() ?? 0;
			if (!len) return;
			tl!.fromTo(p, { strokeDasharray: len, strokeDashoffset: len }, { strokeDashoffset: 0, duration: 0.9, ease: 'power2.inOut', clearProps: 'strokeDasharray,strokeDashoffset' }, 0);
		});
		const fades = node.querySelectorAll('[data-fade]');
		if (fades.length) tl.from(fades, { autoAlpha: 0, duration: 0.6, ease: 'power1.out' }, 0.2);
		const dots = node.querySelectorAll('[data-pop]');
		if (dots.length) tl.from(dots, { scale: 0, transformOrigin: '50% 50%', duration: 0.3, ease: 'back.out(2)', stagger: 0.03 }, 0.5);
	};
	run();
	return { update: (k: unknown) => k !== key && ((key = k), run()), destroy: () => void tl?.kill() };
}

/** Glides an element to a new left and width (the nav's marker). */
export function glide(node: HTMLElement, to: { left: number; width: number }): { update(t: { left: number; width: number }): void; destroy(): void } {
	gsap.set(node, { x: to.left, width: to.width });
	return {
		update(t) {
			if (reduced()) gsap.set(node, { x: t.left, width: t.width });
			else gsap.to(node, { x: t.left, width: t.width, duration: 0.42, ease: 'power3.out', overwrite: true });
		},
		destroy: () => void gsap.killTweensOf(node)
	};
}
