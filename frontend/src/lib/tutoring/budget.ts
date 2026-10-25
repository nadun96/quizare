// Asking the server before using more bandwidth (D-81, TS-FR-32). A refusal
// over the budget says what to do instead; when it ends "…anyway", the
// teacher may insist (up to the hard limit, which nothing goes above).
import { ApiError } from '../api';
import { confirmDialog } from '../ui/dialog.svelte';
import { insistable } from './types';

/**
 * Runs f, and once more with force when the budget refused it and the
 * teacher chooses to go ahead. False: refused, or the teacher didn't insist.
 */
export async function withBudget(f: (force: boolean) => Promise<unknown>, confirm = 'Go ahead'): Promise<boolean> {
	try {
		await f(false);
		return true;
	} catch (e) {
		if (!(e instanceof ApiError) || e.code !== 'over_budget' || !insistable(e.message)) throw e;
		const ok = await confirmDialog({ title: 'Over the server’s bandwidth budget', body: sentence(e.message) + ' Other sessions may get worse video while this runs.', confirm });
		if (!ok) return false;
		await f(true);
		return true;
	}
}

/** "it would use…: stop one, or send it anyway" → "It would use…: stop one, or send it anyway." */
export function sentence(s: string): string {
	return s ? s[0].toUpperCase() + s.slice(1) + (/[.!?]$/.test(s) ? '' : '.') : s;
}
