// Colour maths for themes and custom colours (D-48). Plain JavaScript so the
// theme generator (scripts/gen-themes.mjs, Node) and the browser share it.
// Colours are mixed in OKLab, like CSS color-mix(in oklab, …), and checked
// with the WCAG 2.1 contrast formula on sRGB.

/** @typedef {{ L: number, a: number, b: number }} Lab */

/** @param {string} s '#rrggbb', '#rgb' or 'oklch(L% C H)' @returns {Lab} */
export function parse(s) {
	s = s.trim();
	const ok = /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)/i.exec(s);
	if (ok) {
		const L = Number(ok[1]) / (ok[2] ? 100 : 1);
		const C = Number(ok[3]);
		const h = (Number(ok[4]) * Math.PI) / 180;
		return { L, a: C * Math.cos(h), b: C * Math.sin(h) };
	}
	let hex = s.replace('#', '');
	if (hex.length === 3) hex = [...hex].map((c) => c + c).join('');
	if (!/^[0-9a-f]{6}$/i.test(hex)) throw new Error('unsupported colour: ' + s);
	return fromRgb([0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255));
}

/** @param {number} c */
const toLinear = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
/** @param {number} c */
const toGamma = (c) => (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055);

/** @param {number[]} rgb gamma sRGB 0..1 @returns {Lab} */
function fromRgb(rgb) {
	const [r, g, b] = rgb.map(toLinear);
	const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
	const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
	const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
	return { L: 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, a: 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s, b: 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s };
}

/** @param {Lab} c @returns {number[]} gamma sRGB 0..1, clamped */
export function toRgb(c) {
	const l = (c.L + 0.3963377774 * c.a + 0.2158037573 * c.b) ** 3;
	const m = (c.L - 0.1055613458 * c.a - 0.0638541728 * c.b) ** 3;
	const s = (c.L - 0.0894841775 * c.a - 1.291485548 * c.b) ** 3;
	const lin = [4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s, -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s, -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s];
	return lin.map((x) => Math.min(1, Math.max(0, toGamma(Math.min(1, Math.max(0, x))))));
}

/** @param {Lab | string} c @returns {string} '#rrggbb' */
export function hex(c) {
	const lab = typeof c === 'string' ? parse(c) : c;
	return '#' + toRgb(lab).map((v) => Math.round(v * 255).toString(16).padStart(2, '0')).join('');
}

/** WCAG relative luminance. @param {Lab | string} c */
export function luminance(c) {
	// Measure the colour as it will be shown: rounded to 8-bit sRGB.
	const rgb = toRgb(typeof c === 'string' ? parse(c) : c).map((v) => Math.round(v * 255) / 255);
	const [r, g, b] = rgb.map(toLinear);
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG contrast ratio (1..21). @param {Lab | string} x @param {Lab | string} y */
export function ratio(x, y) {
	const [a, b] = [luminance(x), luminance(y)].sort((p, q) => q - p);
	return (a + 0.05) / (b + 0.05);
}

/** color-mix(in oklab, x t, y). @param {Lab | string} x @param {Lab | string} y @param {number} t share of x */
export function mix(x, y, t) {
	const a = typeof x === 'string' ? parse(x) : x;
	const b = typeof y === 'string' ? parse(y) : y;
	return { L: a.L * t + b.L * (1 - t), a: a.a * t + b.a * (1 - t), b: a.b * t + b.b * (1 - t) };
}

const BLACK = '#000000';
const WHITE = '#ffffff';

/** Black or white, whichever reads better on bg (always ≥ 4.5:1). @param {Lab | string} bg */
export function textOn(bg) {
	return ratio(BLACK, bg) >= ratio(WHITE, bg) ? BLACK : WHITE;
}

/**
 * The colour nearest to c (same hue, moved toward black or white) that has
 * at least min contrast against every background. Returns c unchanged when
 * it already passes.
 * @param {string} c @param {string[]} bgs @param {number} [min]
 * @returns {{ color: string, adjusted: boolean }}
 */
export function readableOn(c, bgs, min = 4.5) {
	const ok = (/** @type {Lab | string} */ x) => bgs.every((bg) => ratio(x, bg) >= min);
	if (ok(c)) return { color: c, adjusted: false };
	// Move away from the backgrounds: darker on light pages, lighter on dark
	// ones. Only lightness changes, so the hue and colourfulness stay.
	const dark = luminance(bgs[0]) > 0.18;
	const lab = parse(c);
	for (let i = 1; i <= 100; i++) {
		const L = dark ? lab.L * (1 - i / 100) : lab.L + (1 - lab.L) * (i / 100);
		const x = parse(hex({ L, a: lab.a, b: lab.b }));
		if (ok(x)) return { color: hex(x), adjusted: true };
	}
	return { color: dark ? BLACK : WHITE, adjusted: true };
}

/**
 * A theme's colour made safe as text on the page and as a button: readable on
 * base-100/200 and on its own soft (10 %) tint, with black or white text on it.
 * @param {string} c @param {string} base100 @param {string} base200
 */
export function semantic(c, base100, base200) {
	let { color, adjusted } = readableOn(c, [base100, base200]);
	// Soft badges and alerts put the colour on a 10 % tint of itself. Aim a
	// little above 4.5 so rounding in the browser's own mix can't drop below it.
	for (let i = 0; i < 100 && ratio(color, mix(color, base100, 0.1)) < 4.6; i++) {
		const next = readableOn(color, [hex(mix(color, base100, 0.1))], 4.6);
		color = next.color;
		adjusted = true;
		if (!next.adjusted) break;
	}
	return { color, content: textOn(color), adjusted };
}
