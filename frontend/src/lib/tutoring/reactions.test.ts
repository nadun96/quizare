import { describe as group, expect, it } from 'vitest';
import { describe, shown } from './reactions';

group('quick reactions', () => {
	it('shows only what someone sent, most first', () => {
		expect(shown(null)).toEqual([]);
		const s = shown({ counts: { thumbs_up: 1, clap: 0, slower: 4, confused: 2 }, window_sec: 60 });
		expect(s.map((r) => [r.kind, r.n])).toEqual([['slower', 4], ['confused', 2], ['thumbs_up', 1]]);
		expect(describe(s[0])).toBe('4 people: Slower please');
		expect(describe(s[2])).toBe('1 person: Thumbs up');
	});
});
