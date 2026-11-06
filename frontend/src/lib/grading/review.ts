// What the teacher lets a student see of their results, and until when (D-140).
export type Review = { detail?: string; until?: string; closed?: boolean };

const when = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });

/** The line above a student's results: why questions are missing, or when they will go. */
export function reviewNote(r: Review | null | undefined): string {
	if (!r) return '';
	if (r.closed) return r.until ? `The review time ended on ${when(r.until)}. Your score stays here.` : 'The review time has ended. Your score stays here.';
	const parts: string[] = [];
	if (r.detail === 'marks') parts.push('Your teacher is showing your score only.');
	else if (r.detail === 'correctness') parts.push('Your teacher is showing which answers were right, without the correct answers.');
	if (r.until) parts.push(`You can review the questions until ${when(r.until)}; your score stays after that.`);
	return parts.join(' ');
}
