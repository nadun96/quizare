// Deleting a classroom, module or topic with everything inside (D-92): the
// teacher sees what goes, and types the name when students' results would.
import { api } from './api';
import { confirmDialog, promptDialog } from './ui/dialog.svelte';

export type DeleteContents = {
	name: string;
	modules: number;
	topics: number;
	quizzes: number;
	tutorials: number;
	presentations: number;
	sessions: number;
	attempts: number;
	tutorial_runs: number;
	enrolments: number;
	live_sessions: number;
	needs_confirm: boolean;
};

export type Level = 'classroom' | 'module' | 'topic';

const plural = (n: number, one: string, many = one + 's') => `${n} ${n === 1 ? one : many}`;

/** "2 topics, 3 quizzes and 1 presentation": what goes, leaving out nothing-counts. */
export function contentsList(c: DeleteContents): string {
	const parts = [
		c.modules && plural(c.modules, 'module'),
		c.topics && plural(c.topics, 'topic'),
		c.quizzes && plural(c.quizzes, 'quiz', 'quizzes'),
		c.tutorials && plural(c.tutorials, 'tutorial'),
		c.presentations && plural(c.presentations, 'presentation'),
		c.enrolments && plural(c.enrolments, 'enrolment')
	].filter(Boolean) as string[];
	if (parts.length < 2) return parts[0] ?? '';
	return parts.slice(0, -1).join(', ') + ' and ' + parts[parts.length - 1];
}

/** The results that would go, or "" when none would. */
export function resultsList(c: DeleteContents): string {
	const parts = [
		c.sessions && `${plural(c.sessions, 'quiz session')} with ${plural(c.attempts, "student's attempt", "students' attempts")}`,
		c.tutorial_runs && plural(c.tutorial_runs, "student's tutorial work", "students' tutorial work")
	].filter(Boolean) as string[];
	return parts.join(' and ');
}

const paths: Record<Level, string> = { classroom: 'classrooms', module: 'modules', topic: 'topics' };

/**
 * Asks, then deletes. Resolves to true when deleted, false when cancelled;
 * throws an ApiError when the server refuses.
 */
export async function deleteWithContents(level: Level, id: string): Promise<boolean> {
	const base = `/api/teacher/${paths[level]}/${id}`;
	const c = await api.get<DeleteContents>(base + '/delete-preview');
	const what = contentsList(c);
	if (c.live_sessions) {
		await confirmDialog({
			title: `"${c.name}" has a quiz session still open`,
			body: `End ${c.live_sessions === 1 ? 'it' : 'them'} first (Sessions), then delete.`,
			confirm: 'OK',
			cancel: 'Close'
		});
		return false;
	}
	const inside = what ? ` Everything inside goes too: ${what}.` : '';
	if (!c.needs_confirm) {
		const ok = await confirmDialog({ title: `Delete the ${level} "${c.name}"?`, body: inside.trim() || undefined, confirm: 'Delete', danger: true });
		if (!ok) return false;
		await api.del(base);
		return true;
	}
	const typed = await promptDialog({
		title: `Delete "${c.name}" and its results?`,
		body: `This also deletes ${resultsList(c)}. Nothing can bring them back; archiving keeps them instead.${inside} Type its name below to confirm.`,
		label: `Type ${c.name} to confirm`,
		placeholder: c.name,
		confirm: 'Delete for good',
		danger: true,
		maxlength: 200
	});
	if (typed === null) return false;
	if (typed.trim() !== c.name.trim()) {
		await confirmDialog({ title: "The name doesn't match", body: 'Nothing was deleted.', confirm: 'OK', cancel: 'Close' });
		return false;
	}
	await api.del(base + '?confirm=' + encodeURIComponent(typed));
	return true;
}

/** Asks for a quiz's or tutorial's title before deleting it with its results. */
export async function confirmTitle(kind: 'quiz' | 'tutorial', title: string, results: string): Promise<string | null> {
	const typed = await promptDialog({
		title: `Delete this ${kind} and its results?`,
		body: `This also deletes ${results}. Nothing can bring ${results.includes(' and ') ? 'them' : 'it'} back. Type its title below to confirm.`,
		label: `Type ${title} to confirm`,
		placeholder: title,
		confirm: 'Delete for good',
		danger: true,
		maxlength: 200
	});
	if (typed === null) return null;
	return typed.trim() === title.trim() ? typed : null;
}
