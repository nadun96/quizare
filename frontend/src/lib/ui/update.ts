// A new version on the server (D-137). Kit checks every few minutes; a page
// opened before an update keeps running the old app until it's reloaded.

type Nav = { willUnload: boolean; to: { url: { href: string } } | null };

/** Where to go by a full page load, because a newer version is out; null to navigate as usual. */
export function reloadTarget(updated: boolean, nav: Nav): string | null {
	return updated && !nav.willUnload && nav.to ? nav.to.url.href : null;
}
