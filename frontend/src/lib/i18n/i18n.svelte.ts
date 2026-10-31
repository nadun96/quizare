// Languages (D-109): English, Sinhala and Tamil. The choice is kept on this
// device, starting from the browser's language; <html lang> follows it so
// screen readers and fonts switch too. t() reads the current language, so
// anything showing t(...) re-renders when it changes. A key missing from a
// language falls back to English (the tests make sure none is missing).
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

class I18n {
	locale = $state<Locale>(initial());
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

/** One of two keys by count: key_one for 1, key_other otherwise; {n} is the count. */
export function tn(key: string, n: number, params?: Record<string, string | number>): string {
	return t(`${key}_${n === 1 ? 'one' : 'other'}` as Key, { n, ...params });
}

/** A date in the current language. */
export function date(d: string | Date, o: Intl.DateTimeFormatOptions = { day: 'numeric', month: 'short' }): string {
	return new Date(d).toLocaleDateString(i18n.tag, o);
}

export type { Key };
