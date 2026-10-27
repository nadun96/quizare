// A student's own progress (D-97): XP, levels, a weekly streak and badges,
// from /api/my/progress. Loaded once per page load and after a result is
// seen, shared by the nav's level ring and the My page.
import { api } from '../api';
import type { IconName } from '../ui/Icon.svelte';

export type Badge = { id: string; name: string; description: string; earned: boolean; earned_at?: string; have: number; need: number };
export type Activity = { kind: 'quiz' | 'tutorial'; title: string; at: string; xp: number };
export type Progress = {
	xp: number;
	level: number;
	level_xp: number;
	next_level_xp: number;
	streak_weeks: number;
	best_streak_weeks: number;
	active_this_week: boolean;
	quizzes: number;
	tutorials: number;
	badges: Badge[];
	recent: Activity[];
};

/** How far through the current level, 0 to 1. */
export function levelFraction(p: Pick<Progress, 'xp' | 'level_xp' | 'next_level_xp'>): number {
	const span = p.next_level_xp - p.level_xp;
	return span > 0 ? Math.min(1, Math.max(0, (p.xp - p.level_xp) / span)) : 0;
}

/** The same points the server gives a released result, for showing what a result earned. */
export function quizXP(pct: number, passed: boolean): number {
	return 10 + Math.round(Math.min(100, Math.max(0, pct))) + (passed ? 20 : 0);
}

/** Icons for the badges, from the app's icon set. */
export const BADGE_ICON: Record<string, IconName> = {
	first_quiz: 'flag', full_marks: 'star', on_a_roll: 'sparkles', comeback: 'arrow-up',
	ten_quizzes: 'trophy', tutorial: 'book', four_weeks: 'clock'
};

/** "1 week", "3 weeks". */
export const weeks = (n: number) => `${n} ${n === 1 ? 'week' : 'weeks'}`;

const SEEN = 'progress.seen';

/** What changed since the student last looked: a new level, newly earned badges. Remembers this look. */
export function news(p: Progress, store: Pick<Storage, 'getItem' | 'setItem'> | null = storage()): { levelUp: boolean; badges: Badge[] } {
	let seen: { level: number; badges: string[] } | null = null;
	try {
		seen = JSON.parse(store?.getItem(SEEN) ?? 'null');
	} catch {
		seen = null;
	}
	const earned = p.badges.filter((b) => b.earned);
	try {
		store?.setItem(SEEN, JSON.stringify({ level: p.level, badges: earned.map((b) => b.id) }));
	} catch {
		// Blocked storage: no celebrations, nothing else lost.
	}
	// The first look celebrates nothing: what was earned before isn't news.
	if (!seen || typeof seen.level !== 'number' || !Array.isArray(seen.badges)) return { levelUp: false, badges: [] };
	return { levelUp: p.level > seen.level, badges: earned.filter((b) => !seen!.badges.includes(b.id)) };
}

function storage(): Storage | null {
	try {
		return typeof localStorage === 'undefined' ? null : localStorage;
	} catch {
		return null;
	}
}

class Store {
	current = $state<Progress | null>(null);
	private pending: Promise<void> | null = null;
	/** Loads it (again, when asked). Failures leave it as it was. */
	load(again = false): Promise<void> {
		if (this.pending && !again) return this.pending;
		this.pending = api
			.get<Progress>('/api/my/progress')
			.then((p) => void (this.current = p))
			.catch(() => {});
		return this.pending;
	}
	clear() {
		this.current = null;
		this.pending = null;
	}
}
export const progress = new Store();
