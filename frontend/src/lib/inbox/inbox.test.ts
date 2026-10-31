import { describe, expect, it } from 'vitest';
import { badge, when } from './inbox.svelte';
import { firstName, insertAt, parseEmails, render, unknownPlaceholders } from '../mailing';

describe('inbox', () => {
	const now = new Date(2026, 9, 31, 15, 0);
	it('says when, shorter the closer it is', () => {
		expect(when(new Date(2026, 9, 31, 14, 59, 30).toISOString(), now)).toBe('now');
		expect(when(new Date(2026, 9, 31, 14, 40).toISOString(), now)).toBe('20 min');
		expect(when(new Date(2025, 9, 1).toISOString(), now)).toMatch(/2025/);
	});
	it('keeps the badge small', () => {
		expect(badge(3)).toBe('3');
		expect(badge(12)).toBe('9+');
	});
});

describe('templated email (D-102)', () => {
	const r = { name: 'Ms Ann Perera', email: 'ann@example.com', role: 'teacher', classroom: 'Grade 8' };
	it('fills placeholders in as the server does', () => {
		expect(render('Hi {{first_name}} ({{ name }}, {{EMAIL}}) of {{classroom}}, {{site_url}}', r, 'https://x.test')).toBe('Hi Ann (Ms Ann Perera, ann@example.com) of Grade 8, https://x.test');
		expect(firstName('Dr')).toBe('Dr');
		expect(firstName('Kamal')).toBe('Kamal');
	});
	it('names placeholders that do not exist', () => {
		expect(unknownPlaceholders('{{frist_name}} {{name}} {{frist_name}} {{x}}')).toEqual(['frist_name', 'x']);
	});
	it('inserts a placeholder at the caret', () => {
		expect(insertAt('Hi !', 3, 3, 'first_name')).toEqual({ text: 'Hi {{first_name}}!', caret: 17 });
		expect(insertAt('Hi you!', 3, 6, 'name').text).toBe('Hi {{name}}!');
	});
	it('reads pasted addresses', () => {
		expect(parseEmails('a@x.lk, B@x.lk;\n<c@x.lk> a@x.lk')).toEqual(['a@x.lk', 'b@x.lk', 'c@x.lk']);
	});
});
