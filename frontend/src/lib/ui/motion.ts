// Transitions that honour reduced motion (WCAG 2.3.3, D-39): the OS setting
// or the in-app "Reduce motion" preference turns every one of them off.
import { cubicOut } from 'svelte/easing';
import { prefersReducedMotion } from 'svelte/motion';
import { fade, fly, scale, slide, type FadeParams, type FlyParams, type ScaleParams, type SlideParams } from 'svelte/transition';
import { prefs } from './prefs.svelte';

export const reduced = () => prefersReducedMotion.current || prefs.motion === 'reduce';
const ms = (d: number | undefined, def: number) => (reduced() ? 0 : (d ?? def));

export const fadeIn = (node: Element, p: FadeParams = {}) => fade(node, { ...p, duration: ms(p.duration, 180) });
export const flyIn = (node: Element, p: FlyParams = {}) => fly(node, { y: 8, easing: cubicOut, ...p, duration: ms(p.duration, 220) });
export const scaleIn = (node: Element, p: ScaleParams = {}) => scale(node, { start: 0.96, easing: cubicOut, ...p, duration: ms(p.duration, 200) });
export const slideIn = (node: Element, p: SlideParams = {}) => slide(node, { easing: cubicOut, ...p, duration: ms(p.duration, 200) });
/** For animate:flip on reordering lists. */
export const flipMs = () => (reduced() ? 0 : 250);
