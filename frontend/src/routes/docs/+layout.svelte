<script lang="ts">
	import { page } from '$app/state';
	import { NAV, PAGES } from '$lib/docs';
	import { plainText } from '$lib/docs/render';

	let { children } = $props();
	let query = $state('');
	let menuOpen = $state(false);
	const index = PAGES.map((p) => ({ ...p, text: plainText(p.source) }));
	const results = $derived(
		query.trim().length < 2
			? []
			: index
					.map((p) => {
						const q = query.trim().toLowerCase();
						const at = p.text.indexOf(q);
						return { p, at, score: (p.title.toLowerCase().includes(q) ? 100 : 0) + (at >= 0 ? 1 : 0) };
					})
					.filter((r) => r.score > 0)
					.sort((a, b) => b.score - a.score)
					.slice(0, 8)
	);
	const current = $derived(page.params.slug ?? 'overview');
	$effect(() => {
		void current;
		menuOpen = false;
	});
	const snippet = (text: string, at: number) => (at < 0 ? '' : '…' + text.slice(Math.max(0, at - 40), at + 80) + '…');
</script>

<svelte:head><title>Developer docs · Classroom Quiz</title></svelte:head>

<div class="docs">
	<button class="menu small" onclick={() => (menuOpen = !menuOpen)} aria-expanded={menuOpen} aria-controls="docs-nav">☰ Contents</button>
	<aside id="docs-nav" class:open={menuOpen}>
		<a class="home" href="/docs/overview">Developer docs</a>
		<input type="search" placeholder="Search the docs" bind:value={query} aria-label="Search the docs" />
		{#if results.length}
			<ul class="results">
				{#each results as r (r.p.slug)}
					<li><a href={'/docs/' + r.p.slug} onclick={() => (query = '')}><strong>{r.p.title}</strong><span class="small muted">{snippet(r.p.text, r.at)}</span></a></li>
				{/each}
			</ul>
		{:else if query.trim().length >= 2}
			<p class="small muted">No matches.</p>
		{/if}
		<nav aria-label="Documentation">
			{#each NAV as s (s.section)}
				<p class="section">{s.section}</p>
				<ul>
					{#each s.pages as [slug, title] (slug)}
						<li><a href={'/docs/' + slug} class:active={current === slug} aria-current={current === slug ? 'page' : undefined}>{title}</a></li>
					{/each}
				</ul>
			{/each}
			<p class="section">Reference</p>
			<ul><li><a href="/api/docs" target="_blank" rel="noopener">Swagger UI ↗</a></li></ul>
		</nav>
	</aside>
	<div class="main">{@render children()}</div>
</div>

<style>
	.docs { display: grid; grid-template-columns: 260px minmax(0, 1fr); max-width: 1400px; margin: 0 auto; }
	aside { position: sticky; top: 0; align-self: start; height: 100dvh; overflow-y: auto; padding: 1rem; border-right: 1px solid var(--border); background: var(--surface); }
	.home { display: block; font-weight: 700; text-decoration: none; color: var(--primary); margin-bottom: 0.75rem; }
	.section { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--muted); margin: 1rem 0 0.25rem; font-weight: 700; }
	ul { list-style: none; padding: 0; margin: 0; }
	nav a { display: block; padding: 0.3rem 0.5rem; border-radius: 6px; text-decoration: none; color: var(--text); font-size: 0.95rem; }
	nav a:hover { background: var(--bg); }
	nav a.active { background: var(--bg); color: var(--primary); font-weight: 600; box-shadow: inset 3px 0 0 var(--primary); }
	.results { margin-top: 0.5rem; display: grid; gap: 0.25rem; }
	.results a { display: grid; padding: 0.4rem; border-radius: 6px; text-decoration: none; color: var(--text); border: 1px solid var(--border); }
	.main { min-width: 0; }
	.menu { display: none; }
	@media (max-width: 860px) {
		.docs { grid-template-columns: minmax(0, 1fr); }
		.menu { display: inline-flex; margin: 0.75rem 1rem 0; }
		aside { display: none; position: static; height: auto; border-right: none; border-bottom: 1px solid var(--border); }
		aside.open { display: block; }
	}
</style>
