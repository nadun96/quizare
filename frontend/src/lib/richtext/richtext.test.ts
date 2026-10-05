// @vitest-environment jsdom
import { afterEach, describe, expect, test } from 'vitest';
import type { Editor } from '@tiptap/core';
import { renderRich, toPlain, hasMath } from './render';
import { createEditor, markdownOf } from './editor';

describe('renderRich: safety', () => {
	const attacks = [
		'<script>alert(1)</script>',
		'<img src=x onerror=alert(1)>',
		'[click](javascript:alert(1))',
		'[x](data:text/html,<script>alert(1)</script>)',
		'![pic](https://evil.example/x.png)',
		'<a href="https://x" onclick="alert(1)">x</a>',
		'**bold <iframe src="https://evil.example"></iframe>**',
		'$x" onmouseover="alert(1)$'
	];
	for (const a of attacks) {
		test(a, () => {
			const doc = new DOMParser().parseFromString(renderRich(a, 'markdown'), 'text/html');
			expect(doc.querySelectorAll('script, img, iframe, object, embed, style').length).toBe(0);
			for (const el of doc.body.querySelectorAll('*')) {
				for (const at of el.attributes) expect(at.name).not.toMatch(/^on/i);
				const href = el.getAttribute('href');
				if (href !== null) expect(href).toMatch(/^(https?:|mailto:)/);
			}
		});
	}
	test('links open safely in a new tab', () => {
		const html = renderRich('[docs](https://example.edu/a)', 'markdown');
		expect(html).toContain('href="https://example.edu/a"');
		expect(html).toContain('rel="noopener noreferrer nofollow"');
		expect(html).toContain('target="_blank"');
	});
	test('plain text is shown literally', () => {
		const html = renderRich('5*3*2 = <b>30</b>\nnext line', '');
		expect(html).toContain('5*3*2 = &lt;b&gt;30&lt;/b&gt;');
		expect(html).not.toContain('<em>');
	});
});

describe('renderRich: formatting', () => {
	test('marks, lists, quote, code, table', () => {
		const html = renderRich('**b** *i* ++u++ ~~s~~ `c`\n\n- one\n- two\n\n1. first\n\n> quote\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n```\ncode\n```', 'markdown');
		for (const s of ['<strong>b</strong>', '<em>i</em>', '<u>u</u>', '<del>s</del>', '<code>c</code>', '<ul>', '<ol>', '<blockquote>', 'class="table-wrap"', '<td>1</td>', '<pre><code>code']) expect(html).toContain(s);
	});
	test('blanks become slots, chips or gaps', () => {
		expect(renderRich('**Water** boils at [[1]] °C', 'markdown', 'slot')).toContain('<span class="blank-slot" data-blank="1"></span>');
		expect(renderRich('at [[2]]', '', 'slot')).toContain('data-blank="2"');
		expect(renderRich('at [[2]]', 'markdown', 'chip')).toContain('[[2]]');
		expect(renderRich('at [[2]]', 'markdown', 'line')).toContain('____');
	});
	test('math, but not money', () => {
		const html = renderRich('Solve $x^2 = 4$ for $5 or $10.\n\n$$\n\\frac{a}{b}\n$$', 'markdown');
		expect(html).toContain('data-latex="x^2 = 4"');
		expect(html).toContain('data-latex="\\frac{a}{b}" data-display="1"');
		expect(html).toContain('for $5 or $10.');
		expect(hasMath(html)).toBe(true);
		expect(hasMath(renderRich('costs $5 and $6', 'markdown'))).toBe(false);
	});
	test('toPlain', () => {
		expect(toPlain('**Water** boils at [[1]]\n\n- at *sea* level', 'markdown')).toBe('Water boils at [[1]] at sea level');
		expect(toPlain('a  *b*\nc', '')).toBe('a *b* c');
	});
});

describe('editor', () => {
	let editor: Editor | undefined;
	afterEach(() => editor?.destroy());
	const open = (value: string, format: '' | 'markdown') => {
		let out = '';
		editor = createEditor({ element: document.createElement('div'), value, format, onchange: (m) => (out = m), onstate: () => {}, onmath: () => {} });
		return { md: () => markdownOf(editor!), out: () => out };
	};

	test('plain text converts without changing what students see', () => {
		const plain = 'What is 5*3*2? It costs $5 or $10.\nWater boils at [[1]] °C and _x_';
		const e = open(plain, '');
		const md = e.md();
		expect(md).toContain('[[1]]');
		expect(toPlain(md, 'markdown')).toBe(toPlain(plain, ''));
		expect(hasMath(renderRich(md, 'markdown'))).toBe(false);
	});

	test('markdown round-trips', () => {
		const src = '## Heading\n\n**Bold** *it* ++under++ ~~gone~~ `code` [[1]] and [[2]]\n\n- a\n- b\n\n> quote\n\nInline $x^2$ math\n\n$$\n\\frac{a}{b}\n$$';
		const once = open(src, 'markdown').md();
		editor!.destroy();
		const twice = open(once, 'markdown').md();
		expect(twice).toBe(once);
		for (const s of ['## Heading', '**Bold**', '++under++', '~~gone~~', '`code`', '[[1]]', '[[2]]', '- a', '> quote', '$x^2$', '$$']) expect(once).toContain(s);
		const html = renderRich(once, 'markdown', 'slot');
		expect(html.match(/blank-slot/g)?.length).toBe(2);
		expect(html).toContain('data-latex="\\frac{a}{b}"');
	});

	test('typing [[3]] makes a blank; edits report Markdown', () => {
		const e = open('', 'markdown');
		editor!.commands.insertContent({ type: 'blank', attrs: { n: '3' } });
		expect(e.out()).toBe('[[3]]');
		editor!.commands.setContent('');
		expect(markdownOf(editor!)).toBe('');
	});

	test('literal markup characters typed by the teacher stay literal', () => {
		const e = open('', 'markdown');
		editor!.commands.insertContent('2 * 3 * 4 and [x](y) <b>');
		expect(toPlain(e.out(), 'markdown')).toBe('2 * 3 * 4 and [x](y) <b>');
	});
});
