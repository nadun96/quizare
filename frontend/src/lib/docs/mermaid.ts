// Mermaid is large, so it is loaded only when a docs page has a diagram.
let loading: Promise<typeof import('mermaid').default> | null = null;

export async function renderDiagrams(root: HTMLElement) {
	const blocks = root.querySelectorAll<HTMLElement>('pre.mermaid:not([data-processed])');
	if (!blocks.length) return;
	loading ??= import('mermaid').then((m) => {
		const dark = matchMedia('(prefers-color-scheme: dark)').matches;
		m.default.initialize({ startOnLoad: false, theme: dark ? 'dark' : 'neutral', securityLevel: 'strict', fontFamily: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif' });
		return m.default;
	});
	const mermaid = await loading;
	await mermaid.run({ nodes: [...blocks] });
}
