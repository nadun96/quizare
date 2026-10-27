// The teacher's grid of what students share backstage (TS-FR-35): pages of
// 25 tiles, one of them enlarged. The teacher's browser receives only the
// videos of the visible page and the enlarged one, never all of them.

export const PAGE_SIZE = 25;

export type StageEntry = { key: string; participant: string; name: string; label: string; source: 'camera' | 'screen_share' };

export const pages = (n: number, size = PAGE_SIZE) => Math.max(1, Math.ceil(n / size));

/** The entries on a page (counted from 0), the page clamped to those there are. */
export function pageOf<T>(list: T[], page: number, size = PAGE_SIZE): T[] {
	const p = Math.min(Math.max(0, page), pages(list.length, size) - 1);
	return list.slice(p * size, p * size + size);
}

/** The videos to receive: the visible page and the enlarged one. */
export function wanted(list: { key: string }[], page: number, enlarged: string | null, size = PAGE_SIZE): Set<string> {
	const keys = new Set(pageOf(list, page, size).map((e) => e.key));
	if (enlarged && list.some((e) => e.key === enlarged)) keys.add(enlarged);
	return keys;
}

/** Steady order: by name, then screens before cameras, so tiles don't jump as people come and go. */
export function ordered<T extends StageEntry>(list: T[]): T[] {
	return [...list].sort((a, b) => a.name.localeCompare(b.name) || (a.source === b.source ? a.label.localeCompare(b.label) : a.source === 'screen_share' ? -1 : 1));
}
