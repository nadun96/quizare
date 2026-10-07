// Display preferences (D-39, D-48): theme, main colour, button labels, text
// size and motion. Per device, kept in localStorage; applied as attributes
// and CSS variables on <html> that app.css reads.
import { semantic } from './colors.js';
import { DAISY_THEMES } from './themes.gen';

/** 'system' | 'light' | 'dark' are our own themes; any other value is a daisyUI theme name. */
export type Theme = string;
export type TextSize = 'md' | 'lg' | 'xl';
export type Motion = 'system' | 'reduce';
/** Icon buttons show their label on hover and focus, or always. */
export type Labels = 'hover' | 'always';

type Stored = { theme: Theme; text: TextSize; motion: Motion; timer: 'shown' | 'compact'; color: string; labels: Labels };
const KEY = 'qp:prefs';
const DEFAULTS: Stored = { theme: 'system', text: 'md', motion: 'system', timer: 'shown', color: '', labels: 'hover' };
const OWN = new Set(['system', 'light', 'dark']);
/** The preference value for a daisyUI theme: its name, except daisyUI's own
 * "light" and "dark", which would clash with ours ("daisy-light", "daisy-dark"). */
export const themeValue = (name: string) => (OWN.has(name) ? 'daisy-' + name : name);
/** The daisyUI theme a preference value selects, if any. */
export const daisyTheme = (v: string) => (OWN.has(v) ? undefined : DAISY_THEMES.find((x) => themeValue(x.name) === v));
const isTheme = (t: string) => OWN.has(t) || !!daisyTheme(t);
const isColor = (c: string) => /^#[0-9a-f]{6}$/i.test(c);

function read(): Stored {
	try {
		const s = { ...DEFAULTS, ...JSON.parse(localStorage.getItem(KEY) ?? '{}') };
		if (!isTheme(s.theme)) s.theme = 'system';
		if (s.color && !isColor(s.color)) s.color = '';
		if (s.labels !== 'always') s.labels = 'hover';
		return s;
	} catch {
		return { ...DEFAULTS };
	}
}

class Prefs {
	theme = $state<Theme>('system');
	text = $state<TextSize>('md');
	motion = $state<Motion>('system');
	/** Quiz timer shown in full or as a compact pill (it expands by itself near the end). */
	timer = $state<'shown' | 'compact'>('shown');
	/** Main (primary) colour chosen by the user; '' = the theme's own. */
	color = $state('');
	labels = $state<Labels>('hover');
	/** The custom colour had to be darkened or lightened to stay readable. */
	colorAdjusted = $state(false);

	private watching = false;

	load() {
		Object.assign(this, read());
		this.apply();
		if (!this.watching && typeof matchMedia === 'function') {
			this.watching = true;
			// "System" follows the OS; a custom colour is re-checked against the new page colours.
			matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change', () => this.apply());
		}
	}
	set<K extends keyof Stored>(k: K, v: Stored[K]) {
		(this as unknown as Stored)[k] = v;
		try {
			const s: Stored = { theme: this.theme, text: this.text, motion: this.motion, timer: this.timer, color: this.color, labels: this.labels };
			localStorage.setItem(KEY, JSON.stringify(s));
		} catch {
			/* private mode: the choice lasts until reload */
		}
		this.apply();
	}
	reset() {
		for (const [k, v] of Object.entries(DEFAULTS)) this.set(k as keyof Stored, v as never);
	}
	apply() {
		if (typeof document === 'undefined') return;
		const el = document.documentElement;
		const daisy = daisyTheme(this.theme);
		// daisyUI themes are separate files, loaded only when chosen.
		let link = document.getElementById('qp-theme') as HTMLLinkElement | null;
		if (daisy) {
			const href = `/themes/${daisy.name}.css`;
			if (!link) {
				link = document.createElement('link');
				link.id = 'qp-theme';
				link.rel = 'stylesheet';
				document.head.append(link);
			}
			if (link.getAttribute('href') !== href) {
				link.onload = () => this.applyColor();
				link.setAttribute('href', href);
			}
			el.setAttribute('data-theme', daisy.name);
		} else {
			link?.remove();
			if (this.theme === 'system') el.removeAttribute('data-theme');
			else el.setAttribute('data-theme', this.theme === 'dark' ? 'quiz-dark' : 'quiz');
		}
		if (this.text === 'md') el.removeAttribute('data-text');
		else el.setAttribute('data-text', this.text);
		if (this.motion === 'reduce') el.setAttribute('data-motion', 'reduce');
		else el.removeAttribute('data-motion');
		el.setAttribute('data-labels', this.labels);
		const meta = document.querySelector('meta[name="theme-color"]');
		const dark = daisy ? daisy.scheme === 'dark' : this.theme === 'dark' || (this.theme === 'system' && typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches);
		meta?.setAttribute('content', daisy ? daisy.base : dark ? '#0f151d' : '#f3f5f8');
		this.applyColor();
	}
	/** Sets the user's main colour, tuned to stay readable on the current theme. */
	applyColor() {
		if (typeof document === 'undefined') return;
		const st = document.documentElement.style;
		if (!this.color) {
			st.removeProperty('--color-primary');
			st.removeProperty('--color-primary-content');
			this.colorAdjusted = false;
			return;
		}
		const cs = getComputedStyle(document.documentElement);
		const b100 = cs.getPropertyValue('--color-base-100').trim() || '#ffffff';
		const b200 = cs.getPropertyValue('--color-base-200').trim() || b100;
		try {
			const s = semantic(this.color, b100, b200);
			st.setProperty('--color-primary', s.color);
			st.setProperty('--color-primary-content', s.content);
			this.colorAdjusted = s.adjusted;
		} catch {
			st.removeProperty('--color-primary');
			st.removeProperty('--color-primary-content');
		}
	}
}

export const prefs = new Prefs();
