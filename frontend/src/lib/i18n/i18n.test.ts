import { describe, expect, it } from 'vitest';
import { en } from './en';
import { DICTS, LOCALES, fill, i18n, t, tn } from './i18n.svelte';

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

describe('languages (D-109)', () => {
	it('translates every key, keeping its placeholders', () => {
		for (const l of LOCALES) {
			for (const k of Object.keys(en) as (keyof typeof en)[]) {
				const s = DICTS[l.id][k];
				expect(s, `${l.id}: ${k} is missing`).toBeTruthy();
				expect(placeholders(s!), `${l.id}: ${k} placeholders`).toEqual(placeholders(en[k]));
			}
		}
	});

	it('switches language and fills placeholders', () => {
		i18n.set('si');
		expect(t('nav.dashboard')).toBe('උපකරණ පුවරුව');
		expect(document.documentElement.lang).toBe('si');
		i18n.set('ta');
		expect(t('pager.page', { page: 2, pages: 5 })).toBe('பக்கம் 2 / 5');
		i18n.set('en');
		expect(tn('ins.students', 1)).toBe('1 student');
		expect(tn('ins.students', 3)).toBe('3 students');
		expect(fill('{a} and {b}', { a: 1 })).toBe('1 and {b}');
		expect(i18n.tag).toBe('en-LK');
	});
});
