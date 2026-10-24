<script lang="ts">
	// A tutorial page's content (D-74): text, pictures, videos, documents in
	// Google's viewer, and links. The server worked out each embed address,
	// so only YouTube's privacy-enhanced player and Google's viewers are framed.
	import RichText from '../richtext/RichText.svelte';
	import Icon from '../ui/Icon.svelte';
	import type { Block } from './tutorial';

	let { blocks }: { blocks: Block[] } = $props();
</script>

<div class="blocks">
	{#each blocks as b, i (i)}
		{#if b.type === 'text'}
			<div class="text"><RichText text={b.text ?? ''} format="markdown" /></div>
		{:else if b.type === 'image'}
			<figure>
				<img src={b.embed} alt={b.alt ?? ''} loading="lazy" referrerpolicy="no-referrer" />
				{#if b.text}<figcaption>{b.text}</figcaption>{/if}
			</figure>
		{:else if b.type === 'video' || b.type === 'document'}
			<figure>
				<div class="frame" class:doc={b.type === 'document'}>
					<iframe src={b.embed} title={b.text || (b.type === 'video' ? 'Video' : 'Document')} loading="lazy" referrerpolicy="strict-origin-when-cross-origin"
						allow="fullscreen; picture-in-picture; encrypted-media" allowfullscreen></iframe>
				</div>
				{#if b.text}<figcaption>{b.text}</figcaption>{/if}
			</figure>
		{:else if b.type === 'link'}
			<a class="link-card" href={b.url} target="_blank" rel="noopener noreferrer">
				<Icon name="link" size={18} />
				<span class="min-w-0"><strong class="block truncate">{b.text}</strong><span class="small muted block truncate">{b.url}</span></span>
				<span class="small muted ml-auto whitespace-nowrap newtab">Opens in a new tab</span>
			</a>
		{/if}
	{/each}
</div>

<style>
	.blocks { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1.25rem; }
	.blocks > * { min-width: 0; }
	.text :global(img) { max-width: 100%; }
	figure { margin: 0; display: grid; grid-template-columns: minmax(0, 1fr); gap: 0.4rem; }
	figure img { max-width: 100%; max-height: 70vh; justify-self: center; border-radius: 0.5rem; }
	figcaption { font-size: 0.9rem; color: var(--color-muted); text-align: center; }
	.frame { position: relative; width: 100%; aspect-ratio: 16 / 9; border-radius: 0.5rem; overflow: hidden; background: #000; }
	.frame.doc { aspect-ratio: 4 / 3; background: var(--color-base-200); }
	.frame iframe { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; }
	.link-card { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem; border: 1px solid var(--color-base-300); border-radius: 0.75rem; color: inherit; text-decoration: none; }
	.link-card:hover { background: var(--color-base-200); }
	.link-card :global(svg) { color: var(--color-primary); flex: none; }
	@media (max-width: 30rem) { .newtab { display: none; } }
</style>
