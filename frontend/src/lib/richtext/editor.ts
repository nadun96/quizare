// Tiptap set-up for the rich text editor (D-38). Imported lazily by
// RichTextEditor.svelte, so Tiptap and KaTeX stay out of the student bundle.
import { Editor, InputRule, Node, mergeAttributes, type JSONContent } from '@tiptap/core';
import StarterKit from '@tiptap/starter-kit';
import { Markdown } from '@tiptap/markdown';
import { TableKit } from '@tiptap/extension-table';
import { BlockMath, InlineMath } from '@tiptap/extension-mathematics';
import { Placeholder } from '@tiptap/extensions';
import 'katex/dist/katex.min.css';
import { BLANK, BLOCK_MATH, INLINE_MATH, type TextFormat } from './syntax';

/** A [[n]] blank, kept as one atom so the marker can't be escaped or split. */
export const Blank = Node.create({
	name: 'blank',
	group: 'inline',
	inline: true,
	atom: true,
	selectable: true,
	addAttributes() {
		return { n: { default: '1', parseHTML: (el) => el.getAttribute('data-blank') ?? '1' } };
	},
	parseHTML() {
		return [{ tag: 'span[data-blank]' }];
	},
	renderHTML({ node, HTMLAttributes }) {
		return ['span', mergeAttributes(HTMLAttributes, { 'data-blank': node.attrs.n, class: 'blank-chip', contenteditable: 'false' }), `[[${node.attrs.n}]]`];
	},
	renderText: ({ node }) => `[[${node.attrs.n}]]`,
	renderMarkdown: (node: JSONContent) => `[[${node.attrs?.n ?? '1'}]]`,
	parseMarkdown: (token) => ({ type: 'blank', attrs: { n: String(token.n ?? '1') } }),
	markdownTokenizer: {
		name: 'blank',
		level: 'inline',
		start: (src: string) => src.indexOf('[['),
		tokenize: (src: string) => {
			const m = BLANK.exec(src);
			if (m) return { type: 'blank', raw: m[0], n: m[1] };
		}
	},
	addInputRules() {
		// Typing [[3]] turns into a blank, like the toolbar button.
		return [
			new InputRule({
				find: /\[\[(\d{1,2})\]\]$/,
				handler: ({ state, range, match }) => {
					state.tr.replaceWith(range.from, range.to, this.type.create({ n: match[1] }));
				}
			})
		];
	}
});

// Same Pandoc-style rule as the renderer, so prices like "$5 or $10" stay text.
const PandocInlineMath = InlineMath.extend({
	markdownTokenizer: {
		name: 'inlineMath',
		level: 'inline',
		start: (src: string) => src.indexOf('$'),
		tokenize: (src: string) => {
			const m = INLINE_MATH.exec(src);
			if (m) return { type: 'inlineMath', raw: m[0], latex: m[1].trim() };
		}
	}
});

const StrictBlockMath = BlockMath.extend({
	markdownTokenizer: {
		name: 'blockMath',
		level: 'block',
		start: (src: string) => src.indexOf('$$'),
		tokenize: (src: string) => {
			const m = BLOCK_MATH.exec(src);
			if (m) return { type: 'blockMath', raw: m[0], latex: m[1].trim() };
		}
	}
});

/** Plain text (CSV import, older questions) as a document: every character literal. */
export function plainToDoc(text: string): JSONContent {
	const paragraph = (block: string): JSONContent => {
		const content: JSONContent[] = [];
		block.split('\n').forEach((line, i) => {
			if (i) content.push({ type: 'hardBreak' });
			for (const part of line.split(/(\[\[\d{1,2}\]\])/)) {
				if (!part) continue;
				const m = BLANK.exec(part);
				content.push(m && m[0] === part ? { type: 'blank', attrs: { n: m[1] } } : { type: 'text', text: part });
			}
		});
		return { type: 'paragraph', content };
	};
	const blocks = text.replace(/\r\n/g, '\n').split(/\n{2,}/).filter((b) => b.trim());
	return { type: 'doc', content: blocks.length ? blocks.map(paragraph) : [{ type: 'paragraph' }] };
}

export type MathEdit = { latex: string; pos: number; display: boolean };

export type EditorOptions = {
	element: HTMLElement;
	value: string;
	format: TextFormat;
	placeholder?: string;
	onchange: (markdown: string) => void;
	onstate: () => void;
	onmath: (m: MathEdit) => void;
};

export function createEditor(o: EditorOptions): Editor {
	const katexOptions = { throwOnError: false, trust: false, strict: 'ignore' as const };
	const editor: Editor = new Editor({
		element: o.element,
		extensions: [
			StarterKit.configure({
				heading: { levels: [2, 3, 4] },
				link: { openOnClick: false, autolink: true, protocols: ['mailto'], isAllowedUri: (url) => /^(https?:|mailto:)/i.test(url) },
				// Pictures are question resources (BR-15), not inline images.
				dropcursor: { width: 2 }
			}),
			Markdown,
			TableKit.configure({ table: { resizable: false } }),
			PandocInlineMath.configure({ katexOptions, onClick: (node, pos) => o.onmath({ latex: node.attrs.latex, pos, display: false }) }),
			StrictBlockMath.configure({ katexOptions, onClick: (node, pos) => o.onmath({ latex: node.attrs.latex, pos, display: true }) }),
			Blank,
			Placeholder.configure({ placeholder: o.placeholder ?? '' })
		],
		content: o.format === 'markdown' ? o.value : plainToDoc(o.value),
		contentType: o.format === 'markdown' ? 'markdown' : 'json',
		onUpdate: () => o.onchange(markdownOf(editor)),
		onSelectionUpdate: o.onstate,
		onTransaction: o.onstate
	});
	return editor;
}

/** The editor's Markdown, with an empty document as "". */
export function markdownOf(editor: Editor): string {
	return editor.isEmpty ? '' : editor.getMarkdown().trim();
}
