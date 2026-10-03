import { readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { SETTINGS } from '../settingsMeta';
import { NAV, PAGES, neighbours, pageFiles, source } from './index';
import { docHref, render, slugify } from './render';

const repo = join(__dirname, '..', '..', '..', '..');

/** Every id attribute in rendered HTML. */
const ids = (html: string) => new Set([...html.matchAll(/ id="([^"]+)"/g)].map((m) => m[1]));

describe('docs registry', () => {
	it('lists every page file in the navigation and every nav entry has content', () => {
		const nav = new Set(NAV.flatMap((s) => s.pages.map(([slug]) => slug)));
		for (const f of pageFiles()) expect(nav, `pages/${f}.md is not in NAV`).toContain(f);
		for (const p of PAGES) expect(p.source.length, `${p.slug} has no content`).toBeGreaterThan(100);
	});

	it('includes the decision log from DECISIONS.md', () => {
		expect(source('decisions')).toContain('D-01');
	});

	it('gives each page one H1 and prev/next neighbours', () => {
		for (const p of PAGES) expect(render(p.source).title, p.slug).not.toBe('');
		expect(neighbours('overview').prev).toBeUndefined();
		expect(neighbours('overview').next?.slug).toBe(PAGES[1].slug);
	});
});

describe('links', () => {
	const rendered = new Map(PAGES.map((p) => [p.slug, render(p.source)]));
	it('resolves every internal doc link and anchor', () => {
		for (const [slug, r] of rendered) {
			for (const [, href] of r.html.matchAll(/href="([^"]+)"/g)) {
				const m = href.match(/^\/docs\/([a-z0-9-]+)(?:#(.+))?$/);
				if (m) {
					const target = rendered.get(m[1]);
					expect(target, `${slug}: link to missing page ${href}`).toBeDefined();
					if (m[2]) expect(ids(target!.html), `${slug}: missing anchor ${href}`).toContain(m[2]);
				} else if (href.startsWith('#')) {
					expect(ids(r.html), `${slug}: missing local anchor ${href}`).toContain(href.slice(1));
				}
				expect(href, `${slug}: unconverted .md link ${href}`).not.toMatch(/\.md(#|$)/);
			}
		}
	});
});

describe('renderer', () => {
	it('slugs headings, numbers duplicates and builds a TOC', () => {
		const r = render('# Title\n## Getting started!\n### Sub\n## Getting started!\n');
		expect(r.title).toBe('Title');
		expect(r.toc.map((t) => t.id)).toEqual(['getting-started', 'sub', 'getting-started-1']);
	});

	it('renders mermaid blocks for the lazy loader and escapes code', () => {
		const r = render('```mermaid\nflowchart LR\n A-->B\n```\n\n```go\nif a < b && c > d {}\n```\n');
		expect(r.html).toContain('<pre class="mermaid">flowchart LR\n A--&gt;B');
		expect(r.html).toContain('if a &lt; b &amp;&amp; c &gt; d');
		expect(r.html).not.toContain('<b');
	});

	it('rewrites page links and marks external links', () => {
		expect(docHref('marking.md#release')).toBe('/docs/marking#release');
		expect(docHref('https://example.com')).toBe('https://example.com');
		expect(render('[x](https://example.com)').html).toContain('rel="noopener noreferrer"');
		expect(slugify('HTTP API & `codes`')).toBe('http-api-codes');
	});
});

// Drift guards: the docs must keep up with the code.
describe('coverage of the codebase', () => {
	it('documents every backend module', () => {
		const modules = readdirSync(join(repo, 'backend', 'internal')).filter((d) => statSync(join(repo, 'backend', 'internal', d)).isDirectory());
		const page = source('backend-modules')!;
		for (const m of modules) expect(page, `backend-modules.md has no section for internal/${m}`).toMatch(new RegExp(`^## ${m}\\b`, 'm'));
	});

	it('documents every setting key', () => {
		const page = source('settings')!;
		for (const s of SETTINGS) expect(page, `settings.md does not mention ${s.key}`).toContain(s.key.replace(/^results_show_(answers|correct|feedback)$/, 'results_show_'));
	});

	it('documents every migration', () => {
		const page = source('data-model')!;
		for (const f of readdirSync(join(repo, 'backend', 'migrations')).filter((f) => f.endsWith('.sql'))) {
			expect(page, `data-model.md does not list ${f}`).toContain(f);
		}
	});
});
