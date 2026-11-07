// The cartoon look of polls (D-150): each option has a colour and a shape, so
// the room can tell them apart from a distance and without relying on colour.
// Text on every colour is the same dark ink.

export const INK = '#1b2148';

export type ToonMark = { name: string; color: string; path: string };

export const MARKS: ToonMark[] = [
	{ name: 'star', color: '#ffc72e', path: 'M12 2.2l3 6.5 7 .9-5.2 4.8 1.4 7L12 17l-6.2 3.4 1.4-7L2 8.6l7-.9z' },
	{ name: 'heart', color: '#ff8177', path: 'M12 21s-9.5-5.8-9.5-12.2A5.3 5.3 0 0 1 12 5.7a5.3 5.3 0 0 1 9.5 3.1C21.500 15.200 12 21 12 21z' },
	{ name: 'drop', color: '#58bdff', path: 'M12 2s7.500 8 7.500 13a7.500 7.500 0 0 1-15 0C4.500 10 12 2 12 2z' },
	{ name: 'leaf', color: '#4fd68f', path: 'M21 3S6 2.500 4 12c-.8 3.800 1 6.200 1 6.200L3 21l1.800.8 2.100-2.400s2.500 1.400 6 .2C22 16.600 21 3 21 3z' },
	{ name: 'moon', color: '#b79bff', path: 'M20.500 14.800A9 9 0 0 1 9.200 3.500a9 9 0 1 0 11.300 11.300z' },
	{ name: 'bolt', color: '#ff9f45', path: 'M13.500 1.500L4 13.800h6.500L9 22.500l10.500-13h-6.800z' },
	{ name: 'cloud', color: '#ff8fd0', path: 'M6.500 19.500a4.500 4.500 0 0 1-.7-8.900 6.200 6.200 0 0 1 12-.8 4.900 4.900 0 0 1-.3 9.700z' },
	{ name: 'flower', color: '#2fd3c9', path: 'M12 2a4 4 0 0 1 3.800 5.200A4 4 0 1 1 15.400 14 4 4 0 1 1 8.600 14 4 4 0 1 1 8.200 7.200 4 4 0 0 1 12 2z' }
];

/** An option's colour and shape, by its place in the question. */
export const markFor = (i: number): ToonMark => MARKS[((i % MARKS.length) + MARKS.length) % MARKS.length];

const maskUrl = (path: string) => `url("data:image/svg+xml,${encodeURIComponent(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24'><path d='${path}'/></svg>`)}")`;

/** Sets the marks as CSS variables on a toon page, for controls that can't draw them. */
export const toonVars = MARKS.map((m, i) => `--toon-${i + 1}:${m.color};--shape-${i + 1}:${maskUrl(m.path)}`).join(';');

/** How many tiles to a row on a presenter's screen, by how many options there are. */
export function tileColumns(options: number): number {
	if (options <= 4) return 2;
	if (options <= 9) return 3;
	if (options <= 16) return 4;
	return 5;
}

/** WCAG contrast of two #rrggbb colours. */
export function contrast(a: string, b: string): number {
	const lum = (hex: string) => {
		const [r, g, bl] = [1, 3, 5].map((i) => {
			const x = parseInt(hex.slice(i, i + 2), 16) / 255;
			return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
		});
		return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
	};
	const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
}
