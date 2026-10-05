// Markdown → HTML for the developer docs. Content comes only from files in
// this repository (src/lib/docs/pages/*.md and DECISIONS.md), so the
// output is trusted and rendered with {@html}.
import { Marked, type Tokens } from 'marked';

export type TocEntry = { depth: number; text: string; id: string };
export type Rendered = { html: string; toc: TocEntry[]; title: string };

export function slugify(text: string): string {
	return text
		.toLowerCase()
		.replace(/<[^>]+>/g, '')
		.replace(/[`*_~]/g, '')
		.replace(/[^\p{L}\p{N}\s-]/gu, '')
		.trim()
		.replace(/\s+/g, '-');
}

const escape = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** Rewrites `other-page.md#anchor` to `/docs/other-page#anchor`. */
export function docHref(href: string): string {
	const m = href.match(/^([a-z0-9-]+)\.md(#.*)?$/);
	return m ? `/docs/${m[1]}${m[2] ?? ''}` : href;
}

export function render(markdown: string): Rendered {
	const toc: TocEntry[] = [];
	const used = new Map<string, number>();
	let title = '';
	const md = new Marked({ gfm: true });
	md.use({
		renderer: {
			heading({ tokens, depth, text }: Tokens.Heading) {
				const inner = this.parser.parseInline(tokens);
				let id = slugify(text);
				const n = used.get(id) ?? 0;
				used.set(id, n + 1);
				if (n) id = `${id}-${n}`;
				if (depth === 1 && !title) title = text;
				if (depth === 2 || depth === 3) toc.push({ depth, text: inner.replace(/<[^>]+>/g, ''), id });
				return `<h${depth} id="${id}"><a class="anchor" href="#${id}" aria-hidden="true">#</a>${inner}</h${depth}>\n`;
			},
			code({ text, lang }: Tokens.Code) {
				if (lang === 'mermaid') return `<pre class="mermaid">${escape(text)}</pre>\n`;
				return `<pre class="code"><code${lang ? ` data-lang="${escape(lang)}"` : ''}>${escape(text)}</code></pre>\n`;
			},
			link({ href, title: t, tokens }: Tokens.Link) {
				const inner = this.parser.parseInline(tokens);
				const h = docHref(href);
				const external = /^https?:/.test(h);
				return `<a href="${escape(h)}"${t ? ` title="${escape(t)}"` : ''}${external ? ' target="_blank" rel="noopener noreferrer"' : ''}>${inner}</a>`;
			},
			table(token: Tokens.Table) {
				// Wrap tables so they scroll on phones instead of overflowing.
				const head = token.header.map((c) => `<th${c.align ? ` style="text-align:${c.align}"` : ''}>${this.parser.parseInline(c.tokens)}</th>`).join('');
				const rows = token.rows
					.map((r) => '<tr>' + r.map((c) => `<td${c.align ? ` style="text-align:${c.align}"` : ''}>${this.parser.parseInline(c.tokens)}</td>`).join('') + '</tr>')
					.join('');
				return `<div class="table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table></div>\n`;
			}
		}
	});
	const html = md.parse(markdown, { async: false }) as string;
	return { html, toc, title };
}

/** Lowercased plain text, for the docs search box. */
export function plainText(markdown: string): string {
	return markdown
		.replace(/```[\s\S]*?```/g, ' ')
		.replace(/[#>*_`|[\]()-]/g, ' ')
		.replace(/\s+/g, ' ')
		.toLowerCase();
}
