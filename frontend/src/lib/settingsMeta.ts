// Labels and levels for the hierarchical settings (BA §7, backend/internal/settings).
// Each option has an icon and a short label; its hint is shown only while it
// is selected, so a teacher reads one sentence, not one per option (D-63).
import type { IconName } from './ui/Icon.svelte';

export type Level = 'teacher' | 'classroom' | 'module' | 'topic' | 'quiz' | 'question' | 'session' | 'student' | 'platform';
export type Option = { v: string; label: string; icon: IconName; hint?: string };
export type Meta = {
	key: string;
	label: string;
	/** Its name in a section's summary line ("Countdown: 60 s"). */
	short: string;
	/** Shown only while this holds, e.g. the warning count only for "Warn first". */
	when?: (get: (key: string) => unknown) => boolean;
	kind: 'int' | 'bool' | 'enum' | 'text';
	levels: Level[];
	section: SectionId;
	options?: Option[];
	/** For bool: what "on" and "off" mean, in a few words. */
	on?: string;
	off?: string;
	unit?: string;
	/** int: 0 means "no limit". */
	zeroIsNone?: boolean;
	/** int: what 0 means instead, in a summary ("all"). */
	zeroLabel?: string;
	help?: string;
};

export type SectionId = 'start' | 'timing' | 'integrity' | 'questions' | 'marking' | 'results' | 'teams' | 'enrolment';
export const SECTIONS: { id: SectionId; title: string; icon: IconName }[] = [
	{ id: 'start', title: 'Starting', icon: 'play' },
	{ id: 'timing', title: 'Timing', icon: 'clock' },
	{ id: 'integrity', title: 'Leaving the quiz', icon: 'flag' },
	{ id: 'questions', title: 'Questions', icon: 'list-ordered' },
	{ id: 'marking', title: 'Marking and feedback', icon: 'check-circle' },
	{ id: 'results', title: 'Results', icon: 'chart' },
	{ id: 'teams', title: 'Teams', icon: 'users' },
	{ id: 'enrolment', title: 'Enrolment', icon: 'user' }
];

const ORDER: Option[] = [
	{ v: 'fixed', label: 'Fixed', icon: 'list-ordered', hint: 'Everyone gets the same order.' },
	{ v: 'shuffled', label: 'Shuffled', icon: 'shuffle', hint: 'Each student gets their own order, which makes copying harder.' }
];

export const SETTINGS: Meta[] = [
	{ key: 'countdown_seconds', short: 'Countdown', label: 'Countdown before the first question', kind: 'int', unit: 's', section: 'start', levels: ['teacher', 'classroom', 'quiz', 'session'] },
	{ key: 'admission_mode', short: 'Admission', label: 'Letting students in', kind: 'enum', section: 'start', levels: ['classroom', 'session'], options: [
		{ v: 'manual', label: 'I admit them', icon: 'user', hint: 'Students wait until you admit them from the dashboard.' },
		{ v: 'auto', label: 'Automatically', icon: 'check-circle', hint: 'Students go straight in when they join.' }
	] },
	{ key: 'start_mode', short: 'Start', label: 'Starting the quiz', kind: 'enum', section: 'start', levels: ['teacher', 'classroom', 'quiz', 'session'], options: [
		{ v: 'countdown', label: 'On admission', icon: 'clock', hint: 'Each student’s countdown starts as soon as they’re admitted.' },
		{ v: 'teacher', label: 'I press Start', icon: 'play', hint: 'Admitted students wait until you press Start.' }
	] },
	{ key: 'question_time_limit_sec', short: 'Per question', label: 'Time per question', kind: 'int', unit: 's', zeroIsNone: true, section: 'timing', levels: ['quiz', 'question'], help: '0 for no limit. Ignored when students can go back.' },
	{ key: 'quiz_time_limit_sec', short: 'Whole quiz', label: 'Time for the whole quiz', kind: 'int', unit: 's', zeroIsNone: true, section: 'timing', levels: ['topic', 'quiz', 'session', 'student'], help: '0 for no limit.' },
	{ key: 'violation_policy', short: 'On leaving', label: 'When a student leaves the quiz', kind: 'enum', section: 'integrity', levels: ['classroom', 'quiz', 'session'], options: [
		{ v: 'invalidate', label: 'End the attempt', icon: 'x', hint: 'Switching tabs or apps ends the attempt; you can reinstate it.' },
		{ v: 'warn_then_invalidate', label: 'Warn first', icon: 'alert', hint: 'Students get warnings first, then the attempt ends.' },
		{ v: 'log_only', label: 'Only record it', icon: 'eye', hint: 'Nothing happens to the attempt; you see it in the alerts.' }
	] },
	{ key: 'allowed_warnings', short: 'Warnings', when: (get) => get('violation_policy') === 'warn_then_invalidate', label: 'Warnings before the attempt ends', kind: 'int', section: 'integrity', levels: ['classroom', 'quiz', 'session'] },
	{ key: 'blur_grace_ms', short: 'Ignore under', label: 'Ignore leaving for less than', kind: 'int', unit: 'ms', section: 'integrity', levels: ['classroom', 'quiz', 'session'], help: 'Covers notifications and accidental taps.' },
	{ key: 'disconnect_grace_sec', short: 'Reconnect', label: 'Time to reconnect', kind: 'int', unit: 's', section: 'integrity', levels: ['classroom', 'quiz', 'session'], help: 'A lost connection counts as leaving after this long.' },
	{ key: 'question_order', short: 'Questions', label: 'Question order', kind: 'enum', section: 'questions', levels: ['quiz', 'session'], options: ORDER },
	{ key: 'question_draw', short: 'Per student', label: 'Questions per student', kind: 'int', zeroLabel: 'all', section: 'questions', levels: ['quiz', 'session'], help: 'Each student gets this many, drawn at random from the quiz, so neighbours see different questions. 0 for all of them.' },
	{ key: 'option_order', short: 'Options', label: 'Answer option order', kind: 'enum', section: 'questions', levels: ['quiz', 'question'], options: ORDER },
	{ key: 'one_way_navigation', short: 'Going back', label: 'Going back to earlier questions', kind: 'bool', section: 'questions', levels: ['quiz'], on: 'Not allowed', off: 'Allowed', help: 'When allowed, students can also flag questions to come back to.' },
	{ key: 'evaluation_method', short: 'Marking', label: 'Marking', kind: 'enum', section: 'marking', levels: ['teacher', 'quiz', 'question'], options: [
		{ v: 'key', label: 'Answer key', icon: 'check', hint: 'Marked instantly against the correct answers.' },
		{ v: 'llm', label: 'AI', icon: 'sparkles', hint: 'Marked by your AI key; you can review every mark.' },
		{ v: 'manual', label: 'By me', icon: 'pencil', hint: 'You set each mark in Results.' }
	] },
	{ key: 'feedback_mode', short: 'Feedback', label: 'Feedback', kind: 'enum', section: 'marking', levels: ['teacher', 'quiz', 'question'], options: [
		{ v: 'none', label: 'None', icon: 'x' },
		{ v: 'predefined', label: 'Written', icon: 'list', hint: 'The feedback you wrote for each question.' },
		{ v: 'ai', label: 'AI', icon: 'sparkles', hint: 'Written by your AI key for each answer.' },
		{ v: 'both', label: 'Both', icon: 'plus', hint: 'Your feedback, then the AI’s.' }
	] },
	{ key: 'llm_model', short: 'Model', label: 'AI model', kind: 'text', section: 'marking', levels: ['teacher', 'quiz', 'question'], help: 'Leave empty for the provider’s default.' },
	{ key: 'pass_mark_pct', short: 'Pass mark', label: 'Pass mark', kind: 'int', unit: '%', section: 'marking', levels: ['classroom', 'quiz'] },
	{ key: 'results_release', short: 'Released', label: 'Students see results', kind: 'enum', section: 'results', levels: ['quiz', 'session'], options: [
		{ v: 'immediate', label: 'Right away', icon: 'send', hint: 'As soon as their attempt is marked.' },
		{ v: 'on_session_end', label: 'When it ends', icon: 'clock', hint: 'When you end the session.' },
		{ v: 'manual', label: 'When I release', icon: 'user', hint: 'Only after you press Release in Results.' }
	] },
	{ key: 'results_show_answers', short: 'Their answers', label: 'Their own answers', kind: 'bool', section: 'results', levels: ['quiz', 'session'], on: 'Shown', off: 'Hidden' },
	{ key: 'results_show_correct', short: 'Correct answers', label: 'The correct answers', kind: 'bool', section: 'results', levels: ['quiz', 'session'], on: 'Shown', off: 'Hidden' },
	{ key: 'results_show_feedback', short: 'Feedback', label: 'Feedback', kind: 'bool', section: 'results', levels: ['quiz', 'session'], on: 'Shown', off: 'Hidden' },
	{ key: 'results_visibility', short: 'Public page', label: 'Public results page', kind: 'enum', section: 'results', levels: ['quiz', 'session'], options: [
		{ v: 'private', label: 'Off', icon: 'eye-off', hint: 'Only you and each student see results.' },
		{ v: 'public', label: 'On', icon: 'share', hint: 'You can share a link to a results page.' }
	] },
	{ key: 'results_view', short: 'Page shows', when: (get) => get('results_visibility') === 'public', label: 'The public page shows', kind: 'enum', section: 'results', levels: ['quiz', 'session'], options: [
		{ v: 'individual', label: 'Each student', icon: 'user' },
		{ v: 'question_pct', label: '% per question', icon: 'chart' },
		{ v: 'pass_rate', label: 'Pass rate', icon: 'users' }
	] },
	{ key: 'team_mode', short: 'Teams', label: 'Teams', kind: 'enum', section: 'teams', levels: ['quiz', 'session'], options: [
		{ v: 'off', label: 'None', icon: 'x' },
		{ v: 'manual', label: 'I choose', icon: 'pencil', hint: 'You put students in teams on the dashboard.' },
		{ v: 'random', label: 'Random', icon: 'shuffle', hint: 'Students go to a team at random as they join.' },
		{ v: 'categories', label: 'Categories', icon: 'list', hint: 'One team per classroom category.' },
		{ v: 'self', label: 'Students choose', icon: 'users', hint: 'Students pick a team when they join.' }
	] },
	{ key: 'team_acceptance', short: 'Counting', when: (get) => (get('team_mode') ?? 'off') !== 'off', label: 'Which marks count', kind: 'enum', section: 'teams', levels: ['quiz', 'session'], options: [
		{ v: 'all', label: 'Everyone’s', icon: 'users', hint: 'Every member answers; their marks are combined.' },
		{ v: 'first', label: 'First answer', icon: 'skip-forward', hint: 'The team’s first answer to each question counts.' },
		{ v: 'captain', label: 'Captain’s', icon: 'star', hint: 'Only the captain’s answers count.' },
		{ v: 'best', label: 'Best', icon: 'trophy', hint: 'The best member’s mark on each question counts.' }
	] },
	{ key: 'team_calc', short: 'Combined by', when: (get) => (get('team_mode') ?? 'off') !== 'off' && (get('team_acceptance') ?? 'all') === 'all', label: 'Combine marks by', kind: 'enum', section: 'teams', levels: ['quiz', 'session'], options: [
		{ v: 'sum', label: 'Total', icon: 'plus' },
		{ v: 'average', label: 'Average', icon: 'minus', hint: 'Members who skip a question count as 0.' },
		{ v: 'max', label: 'Highest', icon: 'arrow-up' },
		{ v: 'min', label: 'Lowest', icon: 'arrow-down' }
	] },
	{ key: 'student_id_required', short: 'Student ID', label: 'Student ID', kind: 'bool', section: 'enrolment', levels: ['classroom'], on: 'Required', off: 'Optional', help: 'Students give their classroom ID when they join.' },
	{ key: 'enrolment_approval', short: 'New students', label: 'New students', kind: 'bool', section: 'enrolment', levels: ['classroom'], on: 'I approve them', off: 'Join directly' },
	{ key: 'auto_enrol_on_join', short: 'Joining a session', label: 'Joining a session', kind: 'bool', section: 'enrolment', levels: ['classroom'], on: 'Enrols the student', off: 'Doesn’t enrol' }
];

export const forLevel = (level: Level) => SETTINGS.filter((m) => level === 'platform' || m.levels.includes(level));

type Val = string | number | boolean | undefined;
// "Answer key" → "answer key" inside a sentence, but "I press Start" and "AI" stay.
const lower = (t: string) => (/^[A-Z][a-z]/.test(t) ? t[0].toLowerCase() + t.slice(1) : t);
function amount(n: number, unit?: string): string {
	if (unit === 's' && n >= 60 && n % 60 === 0) return `${n / 60} min`;
	if (unit === 'ms' && n >= 1000 && n % 1000 === 0) return `${n / 1000} s`;
	return unit === '%' ? `${n}%` : unit ? `${n} ${unit}` : String(n);
}
/** The current value in a few words, for a section's summary line. */
export function describe(m: Meta, v: Val): string {
	if (v === undefined || v === '') return '';
	if (m.kind === 'enum') return lower(m.options?.find((o) => o.v === v)?.label ?? String(v));
	if (m.kind === 'bool') return lower((v ? m.on : m.off) ?? (v ? 'On' : 'Off'));
	if (m.kind === 'int' && Number(v) === 0 && (m.zeroIsNone || m.zeroLabel)) return m.zeroLabel ?? 'no limit';
	if (m.kind === 'int') return amount(Number(v), m.unit);
	return String(v);
}
