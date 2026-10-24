// Interactive tutorials (D-74): what the editor and the student's player share.
import type { IconName } from '../ui/Icon.svelte';
import type { Key, Question, Response, StudentQuestion } from '../types';

export type BlockType = 'text' | 'image' | 'video' | 'document' | 'link';
export type Block = { type: BlockType; text?: string; url?: string; embed?: string; alt?: string };
export type Checkpoint = 'none' | 'answered' | 'correct';
export type FeedbackTiming = 'page' | 'end' | 'release';
export type Leaving = 'allowed' | 'recorded' | 'blocked';

export type Tutorial = {
	id: string;
	topic_id: string;
	quiz_id: string;
	title: string;
	description: string;
	status: 'draft' | 'published';
	allow_back: boolean;
	lock_answers: boolean;
	feedback: FeedbackTiming;
	released_at?: string;
	opens_at?: string;
	due_at?: string;
	published_at?: string;
	created_at: string;
	updated_at: string;
	page_count: number;
};

export type Page = {
	id: string;
	position: number;
	title: string;
	blocks: Block[];
	question_ids: string[];
	checkpoint: Checkpoint;
	max_tries: number;
	leaving: Leaving;
	lock_after: number;
};
export type Editor = { tutorial: Tutorial; pages: Page[]; questions: Question[] };

export type Mark = { score: number; max: number; correct: boolean; pending: boolean; marking: boolean; feedback?: string; comment?: string; key?: Key };
export type QuestionView = StudentQuestion & { response?: Response; checked: boolean; locked: boolean; tries: number; mark?: Mark };
export type PageView = {
	index: number;
	title: string;
	blocks: Block[];
	questions: QuestionView[];
	checkpoint: Checkpoint;
	max_tries: number;
	passed: boolean;
	leaving: Leaving;
	lock_after: number;
	leaves: number; // counted toward the lock
	locked: boolean;
};
export type RunView = {
	tutorial: { id: string; title: string; description: string; allow_back: boolean; feedback: FeedbackTiming; due_at?: string };
	current: number;
	reached: number;
	finished: boolean;
	read_only: boolean;
	past_due: boolean;
	pages: { title: string; questions: number; done: boolean; open: boolean }[];
	page?: PageView;
	score?: { score: number; max: number; pending: number };
};

export type QuestionStats = { id: string; code: string; text: string; type: string; marks: number; answered: number; correct: number; pending: number; score_pct?: number; avg_tries: number };
export type PageStats = {
	index: number;
	title: string;
	reading: boolean;
	checkpoint: Checkpoint;
	leaving: Leaving;
	reached: number;
	stopped_here: number;
	median_seconds?: number;
	leaves: number;
	left_students: number;
	locked: number;
	out_of_tries: number;
	questions: QuestionStats[];
	then?: { median_seconds: number; longer_n: number; longer_pct?: number; shorter_n: number; shorter_pct?: number; questions: number };
};
export type Analytics = {
	started: number;
	finished: number;
	pages: PageStats[];
	students: { run_id: string; seconds: number; reading_seconds: number; score_pct?: number; finished: boolean }[];
};

export type MyTutorial = { id: string; title: string; classroom: string; page_count: number; due_at?: string; opens_at?: string; started: boolean; reached: number; finished: boolean };

export const BLOCKS: { type: BlockType; label: string; icon: IconName; hint: string }[] = [
	{ type: 'text', label: 'Text', icon: 'type', hint: 'Reading, with headings, lists, tables and maths.' },
	{ type: 'image', label: 'Picture', icon: 'eye', hint: 'A picture or diagram from a public link or Google Drive.' },
	{ type: 'video', label: 'Video', icon: 'video', hint: 'A YouTube link, or a video in Google Drive.' },
	{ type: 'document', label: 'Document', icon: 'book', hint: 'A PDF in Google Drive, or a Google Doc, Slides or Sheet.' },
	{ type: 'link', label: 'Link', icon: 'link', hint: 'An outside page students open in a new tab.' }
];

export const CHECKPOINTS: { v: Checkpoint; label: string; icon: IconName; hint: string }[] = [
	{ v: 'none', label: 'Go on freely', icon: 'arrow-right', hint: 'Students can go on without answering.' },
	{ v: 'answered', label: 'Answer first', icon: 'check', hint: 'Every question on the page must be answered before going on.' },
	{ v: 'correct', label: 'Get it right', icon: 'check-circle', hint: 'Answers must be right before going on; after the tries you set, they go on anyway.' }
];

export const FEEDBACK: { v: FeedbackTiming; label: string; icon: IconName; hint: string }[] = [
	{ v: 'page', label: 'As they go', icon: 'check', hint: 'Marks and feedback right after each page’s answers are checked. Checked answers can’t change.' },
	{ v: 'end', label: 'At the end', icon: 'flag', hint: 'Marks and feedback once the student finishes the tutorial.' },
	{ v: 'release', label: 'When I release', icon: 'user', hint: 'Nothing until you press Release, for example after marking written answers.' }
];

export const LEAVING: { v: Leaving; label: string; icon: IconName; hint: string }[] = [
	{ v: 'allowed', label: 'Allowed', icon: 'logout', hint: 'Students may leave the app, for example to look something up.' },
	{ v: 'recorded', label: 'Recorded', icon: 'eye', hint: 'Students may leave, but you see each time they did. They’re told it’s recorded.' },
	{ v: 'blocked', label: 'Not allowed', icon: 'alert', hint: 'Students are warned first; leaving is recorded, and the page can lock until you unlock it.' }
];

/** What the page says about leaving the app, before they do. Links on the page always count as allowed. */
export function leavingNote(p: Pick<PageView, 'leaving' | 'lock_after' | 'leaves' | 'locked'>): string {
	if (p.locked) return 'This page is locked because you left the app too often. Ask your teacher to unlock it.';
	if (p.leaving === 'recorded') return 'Leaving the app on this page is recorded for your teacher. Links on the page are fine.';
	if (p.leaving !== 'blocked') return '';
	const left = p.lock_after ? p.lock_after - p.leaves : 0;
	const lock = p.lock_after ? ` After ${p.lock_after} ${p.lock_after === 1 ? 'time' : 'times'}, the page locks${p.leaves ? ` (${left} left)` : ''}.` : '';
	return `Stay in the app on this page: your teacher sees each time you leave.${lock} Links on the page are fine.`;
}

/** "45 s", "3 min 05 s", "1 h 20 min". */
export function duration(seconds: number | null | undefined): string {
	if (seconds == null) return '—';
	const s = Math.max(0, Math.round(seconds));
	if (s < 60) return `${s} s`;
	if (s < 3600) return `${Math.floor(s / 60)} min ${String(s % 60).padStart(2, '0')} s`;
	return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} min`;
}

/** A block with nothing in it yet, for the editor. */
export function emptyBlock(type: BlockType): Block {
	return type === 'text' ? { type, text: '' } : { type, url: '', text: '', alt: '' };
}

/** Moves item i by d places; out-of-range moves leave the list as it is. */
export function move<T>(list: T[], i: number, d: number): T[] {
	const j = i + d;
	if (i < 0 || i >= list.length || j < 0 || j >= list.length) return list;
	const out = [...list];
	[out[i], out[j]] = [out[j], out[i]];
	return out;
}

/** What the page's checkpoint asks, in a line for the student. */
export function checkpointNote(p: Pick<PageView, 'checkpoint' | 'max_tries' | 'passed' | 'questions'>): string {
	if (p.passed || !p.questions.length) return '';
	if (p.checkpoint === 'answered') return 'Answer every question on this page to go on.';
	if (p.checkpoint === 'correct') return p.max_tries ? `Get the answers right to go on (${p.max_tries} ${p.max_tries === 1 ? 'try' : 'tries'}).` : 'Get the answers right to go on.';
	return '';
}

/** "3 of 8 pages" for the student's list. */
export function progressText(t: MyTutorial): string {
	if (t.finished) return 'Finished';
	if (!t.started) return t.page_count === 1 ? '1 page' : `${t.page_count} pages`;
	return `Page ${Math.min(t.reached + 1, t.page_count)} of ${t.page_count}`;
}

/** Which questions changed since they were last checked, to send for checking. */
export function changed(questions: QuestionView[], drafts: Record<string, Response | undefined>): Record<string, Response> {
	const out: Record<string, Response> = {};
	for (const q of questions) {
		const d = drafts[q.id];
		if (!d || q.locked) continue;
		if (!q.checked || JSON.stringify(d) !== JSON.stringify(q.response ?? {})) out[q.id] = d;
	}
	return out;
}

/** A datetime-local input's value for an ISO time, in the browser's zone. */
export function toLocalInput(iso: string | undefined | null): string {
	if (!iso) return '';
	const d = new Date(iso);
	const p = (n: number) => String(n).padStart(2, '0');
	return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

export function fromLocalInput(v: string): string | undefined {
	return v ? new Date(v).toISOString() : undefined;
}
