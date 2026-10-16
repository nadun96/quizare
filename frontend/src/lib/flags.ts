// Questions a student flagged to come back to (D-61). The server keeps the
// flags with the attempt; these helpers only shape what the page shows.

export type FlaggedItem = { index: number; answered: boolean };

/** The flagged questions in quiz order, for the side panel. */
export function flaggedList(answered: boolean[] | null | undefined, flagged: boolean[] | null | undefined): FlaggedItem[] {
	if (!flagged) return [];
	return flagged.flatMap((f, i) => (f ? [{ index: i, answered: !!answered?.[i] }] : []));
}

const n = (k: number, one: string, many: string) => `${k} question${k === 1 ? ' ' + one : 's ' + many}`;

/** The submit confirmation: what is still unanswered or flagged. */
export function submitWarning(answered: boolean[] | null | undefined, flagged: boolean[] | null | undefined): string {
	const unanswered = answered?.filter((a) => !a).length ?? 0;
	const marked = flagged?.filter(Boolean).length ?? 0;
	const parts = [];
	if (unanswered) parts.push(n(unanswered, 'is', 'are') + ' still unanswered.');
	if (marked) parts.push(n(marked, 'is', 'are') + ' flagged for review.');
	return [...parts, "You can't change your answers afterwards."].join(' ');
}
