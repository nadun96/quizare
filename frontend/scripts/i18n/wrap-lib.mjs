// @ts-nocheck
// Finding the words in a Svelte component (D-157). Used by wrap.mjs, which wraps
// them in tr(), and by the test that keeps new ones from slipping through.
import { parse } from 'svelte/compiler';

/** Attributes and props that hold words people read. */
export const TEXT_ATTRS = new Set(['placeholder', 'title', 'aria-label', 'alt', 'label', 'hint', 'data-tip', 'confirm', 'legend', 'body', 'description', 'aria-description']);
/** Elements whose contents aren't words to translate. */
const SKIP_ELEMENTS = new Set(['code', 'pre', 'kbd', 'samp', 'style', 'script', 'textarea']);

/** Whether a piece of text has words in it, not just figures and punctuation. */
export const hasWords = (s) => /\p{L}{2,}/u.test(s.replace(/\{\d+\}/g, ''));

/** Calls every text-bearing place in a parsed fragment. */
export function walkFragment(fragment, visit) {
	if (!fragment) return;
	visit.fragment?.(fragment.nodes);
	for (const n of fragment.nodes) {
		switch (n.type) {
			case 'RegularElement':
			case 'SvelteElement':
			case 'TitleElement':
			case 'Component':
			case 'SvelteComponent':
			case 'SvelteSelf':
			case 'SvelteHead':
			case 'SvelteBody':
			case 'SvelteWindow':
			case 'SvelteBoundary':
			case 'SvelteFragment':
			case 'SlotElement':
				for (const a of n.attributes ?? []) visit.attribute?.(a, n);
				if (!SKIP_ELEMENTS.has(n.name)) walkFragment(n.fragment, visit);
				break;
			case 'IfBlock':
				walkFragment(n.consequent, visit);
				walkFragment(n.alternate, visit);
				break;
			case 'EachBlock':
				walkFragment(n.body, visit);
				walkFragment(n.fallback, visit);
				break;
			case 'AwaitBlock':
				walkFragment(n.pending, visit);
				walkFragment(n.then, visit);
				walkFragment(n.catch, visit);
				break;
			case 'KeyBlock':
				walkFragment(n.fragment, visit);
				break;
			case 'SnippetBlock':
				walkFragment(n.body, visit);
				break;
		}
	}
}

export const parseSvelte = (src) => parse(src, { modern: true });

/** Literal words still sitting in a component's markup. */
export function bareWords(src) {
	const found = [];
	walkFragment(parseSvelte(src).fragment, {
		fragment(nodes) {
			for (const n of nodes) if (n.type === 'Text' && hasWords(n.data)) found.push(n.data.trim().replace(/\s+/g, ' '));
		},
		attribute(a) {
			if (a.type !== 'Attribute' || !TEXT_ATTRS.has(a.name) || !Array.isArray(a.value)) return;
			for (const v of a.value) if (v.type === 'Text' && hasWords(v.data)) found.push(v.data.trim());
		}
	});
	return found;
}
