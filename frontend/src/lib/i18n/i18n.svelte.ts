// Languages (D-109): English, Sinhala and Tamil. The choice is kept on this
// device, starting from the browser's language; <html lang> follows it so
// screen readers and fonts switch too. t() reads the current language, so
// anything showing t(...) re-renders when it changes. A key missing from a
// language falls back to English (the tests make sure none is missing).
//
// tr() (D-157) is for everything else: the English is the key, so a phrase with
// no translation yet simply shows in English. Each language's phrases load on
// demand, from catalog/<locale>.json, so English users download none of them.
import { en, type Key } from './en';
import { si } from './si';
import { ta } from './ta';

export type Locale = 'en' | 'si' | 'ta';
export const LOCALES: { id: Locale; name: string; english: string; tag: string }[] = [
	{ id: 'en', name: 'English', english: 'English', tag: 'en-LK' },
	{ id: 'si', name: 'සිංහල', english: 'Sinhala', tag: 'si-LK' },
	{ id: 'ta', name: 'தமிழ்', english: 'Tamil', tag: 'ta-LK' }
];
export const DICTS: Record<Locale, Partial<Record<Key, string>>> = { en, si, ta };
const STORE = 'qp:lang';

function initial(): Locale {
	try {
		const saved = localStorage.getItem(STORE);
		if (saved === 'en' || saved === 'si' || saved === 'ta') return saved;
	} catch {
		/* private mode */
	}
	const nav = typeof navigator === 'undefined' ? '' : (navigator.languages?.[0] ?? navigator.language ?? '');
	return nav.startsWith('si') ? 'si' : nav.startsWith('ta') ? 'ta' : 'en';
}

/** Loads a language's phrases; a file that's missing means none yet. */
const CATALOGS = import.meta.glob<{ default: Record<string, string> }>('./catalog/*.json');

class I18n {
	locale = $state<Locale>(initial());
	/** The current language's phrases, by their English. */
	phrases = $state.raw<Record<string, string>>({});
	/** The BCP 47 tag for dates and numbers: en-LK, si-LK, ta-LK. */
	get tag() {
		return LOCALES.find((l) => l.id === this.locale)!.tag;
	}
	set(l: Locale) {
		this.locale = l;
		try {
			localStorage.setItem(STORE, l);
		} catch {
			/* private mode */
		}
		this.apply();
	}
	apply() {
		if (typeof document !== 'undefined') document.documentElement.lang = this.locale;
		return this.load();
	}
	async load() {
		const wanted = this.locale;
		if (wanted === 'en') {
			this.phrases = {};
			return;
		}
		const file = CATALOGS[`./catalog/${wanted}.json`];
		const phrases = file ? (await file()).default : {};
		if (this.locale === wanted) this.phrases = phrases;
	}
}

export const i18n = new I18n();

/** Fills {name} placeholders. */
export function fill(text: string, params?: Record<string, string | number>): string {
	return params ? text.replace(/\{(\w+)\}/g, (m, k: string) => (k in params ? String(params[k]) : m)) : text;
}

/** The text for key in the current language, with {placeholders} filled. */
export function t(key: Key, params?: Record<string, string | number>): string {
	return fill(DICTS[i18n.locale][key] ?? en[key], params);
}

/**
 * A phrase in the current language, or the English if it isn't translated yet.
 * {0}, {1}… are filled from params, wherever the translation puts them.
 */
export function tr(text: string | null | undefined, params?: unknown[]): string {
	if (text == null) return '';
	const s = i18n.phrases[text] ?? text;
	return params ? s.replace(/\{(\d+)\}/g, (m, i: string) => String(params[Number(i)] ?? '')) : s;
}

/** One of two keys by count: key_one for 1, key_other otherwise; {n} is the count. */
export function tn(key: string, n: number, params?: Record<string, string | number>): string {
	return t(`${key}_${n === 1 ? 'one' : 'other'}` as Key, { n, ...params });
}

/** The tag for formatting dates and numbers in the current language. */
export const localeTag = () => i18n.tag;

/** A date in the current language. */
export function date(d: string | Date, o: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short' }): string {
	return new Date(d).toLocaleDateString(i18n.tag, o);
}

export type { Key };
