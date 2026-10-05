// The light half of rich text rendering (D-38): plain text and helpers that
// every page can afford. Markdown rendering (marked + DOMPurify, ./render.ts)
// and KaTeX load only when a question actually uses them, so plain-text
// quizzes keep the student bundle as small as before (ADR-11).
import { BLANK } from './syntax';

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

/** Plain text keeps its line breaks and shows every character literally. */
export function plainHTML(text: string, mode: BlankMode): string {
	return '<p class="plain">' + text.split(/(\[\[\d{1,2}\]\])/).map((p) => {
		const m = BLANK.exec(p);
		return m && m[0] === p ? blankHTML(m[1], mode) : esc(p);
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
