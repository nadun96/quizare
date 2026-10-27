// Quick reactions (TS-FR-26): what students can send, and how the teacher
// sees them: counts of the last minute.

export type ReactionKind = 'thumbs_up' | 'clap' | 'slower' | 'confused';
export type ReactionCounts = { counts: Record<string, number>; window_sec: number };

export const REACTIONS: { kind: ReactionKind; emoji: string; label: string }[] = [
	{ kind: 'thumbs_up', emoji: '👍', label: 'Thumbs up' },
	{ kind: 'clap', emoji: '👏', label: 'Clap' },
	{ kind: 'slower', emoji: '🐢', label: 'Slower please' },
	{ kind: 'confused', emoji: '❓', label: "I'm confused" }
];

/** The reactions with any count, most first; for the teacher's bar. */
export function shown(c: ReactionCounts | null): { kind: ReactionKind; emoji: string; label: string; n: number }[] {
	if (!c) return [];
	return REACTIONS.map((r) => ({ ...r, n: c.counts[r.kind] ?? 0 }))
		.filter((r) => r.n > 0)
		.sort((a, b) => b.n - a.n);
}

/** "3 people: Slower please" for screen readers and the title. */
export const describe = (r: { label: string; n: number }) => `${r.n} ${r.n === 1 ? 'person' : 'people'}: ${r.label}`;
