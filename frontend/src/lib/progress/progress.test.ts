import { describe, expect, it } from 'vitest';
import { levelFraction, news, quizXP, weeks, type Progress } from './progress.svelte';

const base: Progress = { xp: 150, level: 2, level_xp: 100, next_level_xp: 300, streak_weeks: 1, best_streak_weeks: 1, active_this_week: true, quizzes: 1, tutorials: 0, badges: [], recent: [] };
const memory = () => {
	const m = new Map<string, string>();
	return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
};

describe('student progress', () => {
	it('works out how far through the level, and what a result earns, as the server does', () => {
		expect(levelFraction(base)).toBe(0.25);
		expect(levelFraction({ xp: 900, level_xp: 600, next_level_xp: 600 })).toBe(0);
		expect(quizXP(85, true)).toBe(115);
		expect(quizXP(40, false)).toBe(50);
		expect(weeks(1)).toBe('1 week');
	});

	it('celebrates only what is new since the last look, never on the first', () => {
		const s = memory();
		const badge = (id: string, earned = true) => ({ id, name: id, description: '', earned, have: 1, need: 1 });
		expect(news({ ...base, badges: [badge('first_quiz')] }, s)).toEqual({ levelUp: false, badges: [] });
		const n = news({ ...base, level: 3, badges: [badge('first_quiz'), badge('full_marks'), badge('ten_quizzes', false)] }, s);
		expect(n.levelUp).toBe(true);
		expect(n.badges.map((b) => b.id)).toEqual(['full_marks']);
		expect(news({ ...base, level: 3, badges: [badge('first_quiz'), badge('full_marks')] }, s)).toEqual({ levelUp: false, badges: [] });
		expect(news(base, null)).toEqual({ levelUp: false, badges: [] });
	});
});
