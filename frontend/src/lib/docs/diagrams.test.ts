import { describe, expect, it } from 'vitest';
import { PAGES } from './index';

// Parse every diagram with Mermaid itself so a syntax error fails CI
// instead of showing an error box on the docs page.
describe('mermaid diagrams', () => {
	const blocks = PAGES.flatMap((p) => [...p.source.matchAll(/```mermaid\r?\n([\s\S]*?)```/g)].map((m, i) => ({ page: p.slug, i, src: m[1] })));

	it('finds diagrams to check', () => {
		expect(blocks.length).toBeGreaterThan(10);
	});

	it.each(blocks.map((b) => [`${b.page} #${b.i + 1}`, b.src]))('%s parses', async (_name, src) => {
		const mermaid = (await import('mermaid')).default;
		await expect(mermaid.parse(src as string)).resolves.toBeTruthy();
	});
});
