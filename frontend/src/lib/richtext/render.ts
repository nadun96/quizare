// Question text and feedback → safe HTML (D-38).
//
// Teachers write the text and students see it, so nothing here is trusted:
// raw HTML is escaped, links are limited to http(s) and mailto, images are not
// rendered (pictures are question resources, BR-15), and the result goes
// through DOMPurify with a short allowlist. Math is emitted as placeholders
// that KaTeX fills in later, loaded only when a question actually has math.
import DOMPurify from 'dompurify';
import { Marked, type Tokens, type TokenizerAndRendererExtension } from 'marked';
import { BLANK, BLOCK_MATH, INLINE_MATH, UNDERLINE, startOf, type TextFormat } from './syntax';
import { blankHTML, esc, plainHTML, type BlankMode } from './plain';

export { hasMath, typeset, type BlankMode } from './plain';

function mathHTML(latex: string, display: boolean): string {
	const tag = display ? 'div' : 'span';
	return `<${tag} class="math" data-latex="${esc(latex.trim())}"${display ? ' data-display="1"' : ''}>${esc(latex.trim())}</${tag}>`;
}

const SAFE_HREF = /^(https?:|mailto:)/i;

function markdownParser(mode: BlankMode): Marked {
	const ext = (name: string, level: 'block' | 'inline', start: string, re: RegExp, html: (m: RegExpExecArray) => string): TokenizerAndRendererExtension => ({
		name,
		level,
		start: startOf(start),
		tokenizer(src) {
			const m = re.exec(src);
			if (m) return { type: name, raw: m[0], m };
		},
		renderer: (t) => html((t as unknown as { m: RegExpExecArray }).m)
	});
	const md = new Marked({ gfm: true, breaks: true });
	md.use({
		extensions: [
			ext('blank', 'inline', '[[', BLANK, (m) => blankHTML(m[1], mode)),
			ext('blockMath', 'block', '$$', BLOCK_MATH, (m) => mathHTML(m[1], true) + '\n'),
			ext('inlineMath', 'inline', '$', INLINE_MATH, (m) => mathHTML(m[1].replace(/\\\$/g, '$'), false)),
			{
				name: 'underline',
				level: 'inline',
				start: startOf('++'),
				tokenizer(src) {
					const m = UNDERLINE.exec(src);
					if (m) return { type: 'underline', raw: m[0], tokens: this.lexer.inlineTokens(m[1]) };
				},
				renderer(t) {
					return `<u>${this.parser.parseInline((t as Tokens.Generic).tokens ?? [])}</u>`;
				}
			}
		],
		renderer: {
			html: ({ text }: Tokens.HTML | Tokens.Tag) => esc(text),
			image: ({ text }: Tokens.Image) => esc(text),
			link({ href, tokens }: Tokens.Link) {
				const inner = this.parser.parseInline(tokens);
				if (!SAFE_HREF.test(href)) return inner;
				return `<a href="${esc(href)}" target="_blank" rel="noopener noreferrer nofollow">${inner}</a>`;
			},
			table(token: Tokens.Table) {
				const cell = (c: Tokens.TableCell, tag: string) => `<${tag}${c.align ? ` class="align-${c.align}"` : ''}>${this.parser.parseInline(c.tokens)}</${tag}>`;
				const head = token.header.map((c) => cell(c, 'th')).join('');
				const rows = token.rows.map((r) => '<tr>' + r.map((c) => cell(c, 'td')).join('') + '</tr>').join('');
				return `<div class="table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table></div>\n`;
			}
		}
	});
	return md;
}

const parsers = new Map<BlankMode, Marked>();

const ALLOWED_TAGS = ['p', 'br', 'strong', 'em', 'u', 's', 'del', 'code', 'pre', 'blockquote', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'a', 'div', 'span', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'input'];
const ALLOWED_ATTR = ['href', 'target', 'rel', 'class', 'start', 'data-blank', 'data-latex', 'data-display', 'aria-label', 'type', 'checked', 'disabled'];

/** Renders question text or feedback to sanitised HTML. */
export function renderRich(text: string, format: TextFormat | undefined, mode: BlankMode = 'line'): string {
	let html: string;
	if (format === 'markdown') {
		let md = parsers.get(mode);
		if (!md) parsers.set(mode, (md = markdownParser(mode)));
		html = md.parse(text, { async: false }) as string;
	} else {
		html = plainHTML(text, mode);
	}
	const clean = DOMPurify.sanitize(html, { ALLOWED_TAGS, ALLOWED_ATTR, ALLOW_DATA_ATTR: false });
	// Task-list checkboxes (GFM) are display only.
	return clean.replace(/<input /g, '<input disabled ');
}

/** One-line plain text for tables and previews (analytics, public results). */
export function toPlain(text: string, format: TextFormat | undefined): string {
	if (format !== 'markdown') return text.replace(/\s+/g, ' ').trim();
	const doc = new DOMParser().parseFromString(renderRich(text, format, 'chip'), 'text/html');
	for (const el of doc.body.querySelectorAll('br, p, li, td, th, div')) el.append(' ');
	return (doc.body.textContent ?? '').replace(/\s+/g, ' ').trim();
}
