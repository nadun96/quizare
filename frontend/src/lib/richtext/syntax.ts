// The Markdown dialect of the rich text editor (D-38), shared by the editor
// (Tiptap) and the renderer (marked) so both read the same text the same way.
//
//   [[1]]      a blank (BLANK_OPT / BLANK_TEXT), exactly as in the CSV format
//   ++text++   underline
//   $x^2$      inline math, Pandoc rule: no space just inside the dollars and
//              no digit right after the closing one, so "$5 or $10" stays text
//   $$ … $$    display math

/** Question text formats; '' is plain text (CSV import, older questions). */
export type TextFormat = '' | 'markdown';

export const BLANK = /^\[\[(\d{1,2})\]\]/;
export const UNDERLINE = /^\+\+(?=\S)([\s\S]*?\S)\+\+/;
export const INLINE_MATH = /^\$(?=[^\s$])((?:\\\$|[^$\n])*?[^\s$\\])\$(?!\d)/;
export const BLOCK_MATH = /^\$\$([\s\S]+?)\$\$/;

/** Marked-style `start` helpers: where a token could begin in `src`. */
export const startOf = (marker: string) => (src: string) => {
	const i = src.indexOf(marker);
	return i < 0 ? undefined : i;
};
