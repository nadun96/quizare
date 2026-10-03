<script lang="ts">
	import { tick } from 'svelte';
	import { page } from '$app/state';
	import { neighbours, PAGES, source } from '$lib/docs';
	import { renderDiagrams } from '$lib/docs/mermaid';
	import { render } from '$lib/docs/render';
	import './docs.css';

	const slug = $derived(page.params.slug ?? 'overview');
	const md = $derived(source(slug));
	const doc = $derived(md ? render(md) : null);
	const meta = $derived(PAGES.find((p) => p.slug === slug));
	const nb = $derived(neighbours(slug));
	let article = $state<HTMLElement>();

	$effect(() => {
		void doc;
		tick().then(() => {
			if (!article) return;
			renderDiagrams(article).catch((e) => console.warn('diagram', e));
			if (location.hash) document.getElementById(decodeURIComponent(location.hash.slice(1)))?.scrollIntoView();
			else window.scrollTo(0, 0);
		});
	});
</script>

<svelte:head><title>{doc?.title ?? 'Not found'} · Developer docs</title></svelte:head>

<div class="doc-wrap">
	{#if doc}
		<article class="doc" bind:this={article}>
			<p class="crumb small muted">{meta?.section}</p>
			{@html doc.html}
			<nav class="pager" aria-label="Previous and next page">
				{#if nb.prev}<a href={'/docs/' + nb.prev.slug} class="card"><span class="small muted">← Previous</span><strong>{nb.prev.title}</strong></a>{:else}<span></span>{/if}
				{#if nb.next}<a href={'/docs/' + nb.next.slug} class="card" style="text-align:right"><span class="small muted">Next →</span><strong>{nb.next.title}</strong></a>{/if}
			</nav>
		</article>
		{#if doc.toc.length > 2}
			<aside class="toc" aria-label="On this page">
				<p class="small muted"><strong>On this page</strong></p>
				{#each doc.toc as t (t.id)}
					<a href={'#' + t.id} class:sub={t.depth === 3}>{t.text}</a>
				{/each}
			</aside>
		{/if}
	{:else}
		<article class="doc"><h1>Page not found</h1><p><a href="/docs/overview">Back to the overview</a></p></article>
	{/if}
</div>
