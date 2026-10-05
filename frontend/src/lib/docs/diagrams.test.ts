import { beforeAll, describe, expect, it } from 'vitest';
import { PAGES } from './index';

// Parse every diagram with Mermaid itself so a syntax error fails CI
// instead of showing an error box on the docs page.
describe('mermaid diagrams', () => {
	const blocks = PAGES.flatMap((p) => [...p.source.matchAll(/```mermaid\r?\n([\s\S]*?)```/g)].map((m, i) => ({ page: p.slug, i, src: m[1] })));
	let mermaid: typeof import('mermaid').default;

	// Mermaid is large; load it once, with room for a slow first import when
	// the whole suite runs in parallel (it timed out at the 5 s default).
	beforeAll(async () => {
		mermaid = (await import('mermaid')).default;
	}, 60_000);

	it('finds diagrams to check', () => {
		expect(blocks.length).toBeGreaterThan(10);
	});

	it.each(blocks.map((b) => [`${b.page} #${b.i + 1}`, b.src]))('%s parses', async (_name, src) => {
		await expect(mermaid.parse(src as string)).resolves.toBeTruthy();
	}, 20_000);
});
