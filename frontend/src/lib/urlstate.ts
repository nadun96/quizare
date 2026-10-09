// The address's query string for Paged lists (TS-FR-94), through SvelteKit's
// shallow routing so the router stays in charge of history.
import { replaceState } from '$app/navigation';
import { page } from '$app/state';
import type { UrlState } from './paged.svelte';

export const urlState: UrlState = {
	read: () => new URLSearchParams(typeof location === 'undefined' ? '' : location.search),
	write(params) {
		const qs = params.toString();
		const next = location.pathname + (qs ? '?' + qs : '') + location.hash;
		if (next === location.pathname + location.search + location.hash) return;
		try {
			replaceState(next, page.state);
		} catch {
			/* before the router starts (or outside SvelteKit, in tests): leave the address as it is */
		}
	}
};
