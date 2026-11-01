// The light half of rich text rendering (D-38): plain text and helpers that
// every page can afford. Markdown rendering (marked + DOMPurify, ./render.ts)
// and KaTeX load only when a question actually uses them, so plain-text
// quizzes keep the student bundle as small as before (ADR-11).
import { ANY_MATH, BLANK } from './syntax';

/**
 * How [[n]] blanks are drawn: `slot` leaves an empty element for the caller
 * to put an input into, `chip` shows a visible marker, `line` a gap.
 */
export type BlankMode = 'slot' | 'chip' | 'line';

export const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;');

export function blankHTML(n: string, mode: BlankMode): string {
	if (mode === 'slot') return `<span class="blank-slot" data-blank="${n}"></span>`;
	if (mode === 'chip') return `<span class="blank-chip">[[${n}]]</span>`;
	return `<span class="blank-line" aria-label="blank ${n}">____</span>`;
}

/** A math placeholder KaTeX fills in (typeset). Display math is a block-level span, so it can sit anywhere. */
export function mathHTML(latex: string, display: boolean): string {
	// Text typed outside the formula editor reaches us with Markdown's escaping: "\\epsilon" for "\epsilon".
	// A LaTeX line break (\\) is never followed straight by a letter, so a doubled backslash before one is that escape.
	latex = latex.replace(/\\\\(?=[A-Za-z])/g, '\\');
	return `<span class="math${display ? ' math-display' : ''}" data-latex="${esc(latex.trim())}"${display ? ' data-display="1"' : ''}>${esc(latex.trim())}</span>`;
}

/** Text shown literally except $…$ and $$…$$, which become math (D-112): option labels, answers, captions, plain questions. */
export function mathTextHTML(text: string): string {
	let out = '';
	let at = 0;
	for (const m of text.matchAll(ANY_MATH)) {
		out += esc(text.slice(at, m.index));
		out += m[1] !== undefined ? mathHTML(m[1], true) : mathHTML(m[2].replace(/\\\$/g, '$'), false);
		at = m.index! + m[0].length;
	}
	return out + esc(text.slice(at));
}

/** True when text has a formula to typeset. */
export const textHasMath = (text: string) => text.includes('$') && mathTextHTML(text).includes('data-latex=');

/** Plain text keeps its line breaks and shows every character literally, formulas aside (D-112). */
export function plainHTML(text: string, mode: BlankMode): string {
	return '<p class="plain">' + text.split(/(\[\[\d{1,2}\]\])/).map((p) => {
		const m = BLANK.exec(p);
		return m && m[0] === p ? blankHTML(m[1], mode) : mathTextHTML(p);
	}).join('') + '</p>';
}

/** True when rendered HTML has math for KaTeX to typeset. */
export const hasMath = (html: string) => html.includes('data-latex=');

/** Typesets every math placeholder under `root` with KaTeX (lazy-loaded). */
export async function typeset(root: HTMLElement): Promise<void> {
	const els = root.querySelectorAll<HTMLElement>('.math[data-latex]');
	if (!els.length) return;
	const [{ default: katex }] = await Promise.all([import('katex'), import('katex/dist/katex.min.css')]);
	for (const el of els) {
		katex.render(el.dataset.latex ?? '', el, { displayMode: el.dataset.display === '1', throwOnError: false, trust: false, strict: 'ignore' });
	}
}

/** A formula's source without its dollar signs, for places KaTeX can't reach (a <select>'s options, a tooltip). */
export const mathPlain = (text: string) => text.replace(ANY_MATH, (_, d: string | undefined, i: string | undefined) => (d ?? i ?? '').trim());

/** A select's option label (D-112): "B · x^2" when the list has formulas (the typeset list sits beside it), the text otherwise. */
export function optionLabel(text: string, i: number, lettered: boolean): string {
	return lettered ? `${String.fromCharCode(65 + i)} · ${mathPlain(text)}` : text;
}
