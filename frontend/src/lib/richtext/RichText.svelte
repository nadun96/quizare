<script lang="ts">
	// Shows question text or feedback (D-38). Plain text renders exactly as it
	// always did; Markdown from the rich text editor is sanitised in renderRich,
	// which (with marked and DOMPurify) loads only when a question needs it.
	// With a `blank` snippet, each [[n]] gets that snippet (an input or select)
	// in place, so blanks can sit inside lists, tables or bold text.
	import type { Snippet } from 'svelte';
	import { hasMath, plainHTML, typeset, type BlankMode } from './plain';
	import type { TextFormat } from './syntax';

	let {
		text,
		format = '',
		blanks = 'line',
		blank,
		class: cls = ''
	}: { text: string; format?: TextFormat; blanks?: BlankMode; blank?: Snippet<[string]>; class?: string } = $props();

	const mode = $derived<BlankMode>(blank ? 'slot' : blanks);
	let markdownHTML = $state<string | null>(null);
	$effect(() => {
		if (format !== 'markdown') return;
		const [t, m] = [text, mode];
		let current = true;
		import('./render').then((r) => current && (markdownHTML = r.renderRich(t, 'markdown', m)));
		return () => {
			current = false;
			markdownHTML = null;
		};
	});
	const html = $derived(format === 'markdown' ? (markdownHTML ?? '') : plainHTML(text, mode));
	const ids = $derived(blank ? [...new Set([...text.matchAll(/\[\[(\d{1,2})\]\]/g)].map((m) => m[1]))] : []);

	let root = $state<HTMLElement>();
	let holder = $state<HTMLElement>();

	$effect(() => {
		void html;
		if (!root || !html) return;
		// Move each rendered blank control into its slot; Svelte keeps its
		// references, so the controls stay reactive where they land.
		if (holder) {
			for (const el of [...holder.children] as HTMLElement[]) {
				const slot = root.querySelector<HTMLElement>(`.blank-slot[data-blank="${el.dataset.for}"]:empty`);
				if (slot) slot.append(el);
			}
		}
		if (hasMath(html)) void typeset(root);
	});
</script>

{#key html}
	<div class="rich {cls}" bind:this={root}>{@html html}</div>
	{#if blank}
		<div hidden bind:this={holder}>
			{#each ids as id (id)}<span class="blank-control" data-for={id}>{@render blank(id)}</span>{/each}
		</div>
	{/if}
{/key}

<style>
	.rich { overflow-wrap: anywhere; }
	.rich :global(p) { margin: 0 0 0.6em; }
	.rich :global(p:last-child) { margin-bottom: 0; }
	.rich :global(.plain) { white-space: pre-wrap; }
	.rich :global(h2), .rich :global(h3), .rich :global(h4) { margin: 0.6em 0 0.4em; line-height: 1.25; }
	.rich :global(h2) { font-size: 1.25em; }
	.rich :global(h3) { font-size: 1.12em; }
	.rich :global(h4) { font-size: 1em; }
	.rich :global(ul), .rich :global(ol) { margin: 0 0 0.6em; padding-left: 1.4em; }
	.rich :global(blockquote) { margin: 0 0 0.6em; padding: 0.2em 0.9em; border-left: 3px solid var(--color-base-300); color: var(--muted); }
	.rich :global(code) { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 0.92em; background: var(--bg); border: 1px solid var(--color-base-300); border-radius: 4px; padding: 0.05em 0.3em; }
	.rich :global(pre) { margin: 0 0 0.6em; padding: 0.75em; overflow-x: auto; background: var(--bg); border: 1px solid var(--color-base-300); border-radius: 8px; }
	.rich :global(pre code) { border: 0; padding: 0; background: none; }
	.rich :global(.table-wrap) { overflow-x: auto; margin: 0 0 0.6em; }
	.rich :global(table) { border-collapse: collapse; font-size: 0.95em; }
	.rich :global(th), .rich :global(td) { border: 1px solid var(--color-base-300); padding: 0.35em 0.6em; text-align: left; vertical-align: top; }
	.rich :global(th) { background: var(--bg); }
	.rich :global(.align-center) { text-align: center; }
	.rich :global(.align-right) { text-align: right; }
	.rich :global(hr) { border: 0; border-top: 1px solid var(--color-base-300); margin: 0.8em 0; }
	.rich :global(a) { color: var(--accent); }
	.rich :global(.math[data-display]) { overflow-x: auto; margin: 0 0 0.6em; text-align: center; }
	.rich :global(.blank-chip) { font-family: ui-monospace, Consolas, monospace; font-size: 0.85em; padding: 0 0.3em; border-radius: 4px; background: var(--warn-bg); color: var(--warn); }
	.rich :global(.blank-line) { letter-spacing: 0.05em; }
	.rich :global(.blank-slot) { display: inline-block; }
</style>
