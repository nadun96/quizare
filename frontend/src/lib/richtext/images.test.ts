// @vitest-environment jsdom
import { afterEach, describe, expect, test } from 'vitest';
import type { Editor } from '@tiptap/core';
import { renderRich } from './render';
import { createEditor, markdownOf } from './editor';
import { imageFiles, IMAGE_SRC } from './images';

const ID = '3f2c9a10-1b2c-4d5e-8f90-123456789abc';

describe('pictures in rich text (D-113)', () => {
	test('shows pictures from our own store, with their description', () => {
		const html = renderRich(`Look:\n\n![A plant cell](/api/images/${ID})`, 'markdown');
		expect(html).toContain(`<img class="rt-img" src="/api/images/${ID}" alt="A plant cell" loading="lazy" decoding="async">`);
	});
	test('never shows a picture from anywhere else', () => {
		for (const src of ['https://tracker.example/p.png', 'javascript:alert(1)', 'data:image/png;base64,AAAA', '/api/images/../auth/me', `/api/images/${ID}?x=1`]) {
			const html = renderRich(`![Described](${src})`, 'markdown');
			expect(html, src).not.toContain('<img');
			expect(html, src).toContain('Described');
		}
		expect(renderRich('<img src=x onerror=alert(1)>', 'markdown')).not.toContain('<img');
	});
	test('knows our addresses and picture files', () => {
		expect(IMAGE_SRC.test(`/api/images/${ID}`)).toBe(true);
		expect(IMAGE_SRC.test('/api/images/x')).toBe(false);
		const files = [new File(['a'], 'a.png', { type: 'image/png' }), new File(['b'], 'b.svg', { type: 'image/svg+xml' }), new File(['c'], 'c.txt', { type: 'text/plain' })];
		expect(imageFiles(files).map((f) => f.name)).toEqual(['a.png']);
	});
});

describe('the editor keeps pictures in place (D-113)', () => {
	let editor: Editor | undefined;
	afterEach(() => editor?.destroy());
	test('a picture survives a round trip through Markdown', () => {
		editor = createEditor({ element: document.createElement('div'), value: `Look at ![A cell](/api/images/${ID}) closely.`, format: 'markdown', onchange: () => {}, onstate: () => {}, onmath: () => {} });
		expect(markdownOf(editor)).toContain(`![A cell](/api/images/${ID})`);
		editor.chain().focus().insertContent({ type: 'image', attrs: { src: `/api/images/${ID}`, alt: 'Second' } }).run();
		expect(markdownOf(editor)).toContain(`![Second](/api/images/${ID})`);
	});
});
