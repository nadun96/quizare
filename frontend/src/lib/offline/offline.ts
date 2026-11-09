// Offline assignments (D-159): work done away from the platform, marked by
// the teacher by hand or through a downloaded sheet. In a group assignment
// each member gets the group's mark unless they have their own.

export type Mode = 'individual' | 'group';

export type Offline = {
	id: string;
	classroom_id: string;
	topic_id: string;
	title: string;
	instructions: string;
	max_marks: number;
	mode: Mode;
	due_at: string | null;
	released_at: string | null;
	created_at: string;
	marked: number;
	students: number;
};

export type OfflineStudent = {
	user_id: string;
	name: string;
	student_number: string | null;
	groups: string[];
	group_id?: string;
	group_name?: string;
	group_score?: number;
	own: number | null;
	own_comment: string;
	score: number | null;
	comment: string;
};

export type OfflineGroup = { id: string; name: string; color: number; members: string[]; score: number | null; comment: string };

export type OfflineSheet = Offline & { classroom: string; topic: string; module: string; students: OfflineStudent[]; groups: OfflineGroup[] };

export type Upload = {
	students: { user_id: string; name: string; score: number; comment: string; was: number | null }[];
	groups: { group_id: string; name: string; score: number; comment: string; was: number | null }[];
	problems: { sheet: string; row: number; message: string }[];
	same: number;
};

export type MyOffline = {
	id: string;
	title: string;
	instructions: string;
	classroom: string;
	classroom_id: string;
	topic: string;
	max_marks: number;
	mode: Mode;
	due_at: string | null;
	released: boolean;
	group?: string;
	score?: number;
	pct?: number;
	comment?: string;
};

/** A mark typed in: '' is none; a comma may be the decimal point. */
export function parseMark(text: string, max: number): { value: number | null; error: string } {
	const t = text.trim().replace(',', '.');
	if (!t) return { value: null, error: '' };
	const n = Number(t);
	if (!Number.isFinite(n)) return { value: null, error: 'Not a number' };
	if (n < 0 || n > max) return { value: null, error: `0 to ${max}` };
	if (Math.round(n * 100) / 100 !== n) return { value: null, error: 'At most 2 decimal places' };
	return { value: n, error: '' };
}

export const markText = (n: number | null | undefined) => (n == null ? '' : String(n));

/** A row being edited: its mark as typed, and its comment. */
export type Draft = { mark: string; comment: string };

/** The rows whose mark or comment differ from what's saved. */
export function changed<T extends { id: string; score: number | null; comment: string }>(saved: T[], drafts: Record<string, Draft>, max: number) {
	const out: { id: string; score: number | null; comment: string }[] = [];
	const errors: Record<string, string> = {};
	for (const s of saved) {
		const d = drafts[s.id];
		if (!d) continue;
		const { value, error } = parseMark(d.mark, max);
		if (error) {
			errors[s.id] = error;
			continue;
		}
		const comment = d.comment.trim();
		if (value !== s.score || (value != null && comment !== s.comment.trim())) out.push({ id: s.id, score: value, comment: value == null ? '' : comment });
	}
	return { changes: out, errors };
}

/** "12 of 30 marked". */
export const progress = (o: Pick<Offline, 'marked' | 'students'>) => `${o.marked} of ${o.students} marked`;

/** The file to download. */
export const sheetUrl = (id: string, format: 'xlsx' | 'csv', sheet?: 'students' | 'groups') =>
	`/api/teacher/offline-assignments/${id}/sheet?format=${format}${sheet ? `&sheet=${sheet}` : ''}`;

/** Whether a file looks like a mark sheet we can read. */
export const readable = (name: string) => /\.(xlsx|csv)$/i.test(name);
