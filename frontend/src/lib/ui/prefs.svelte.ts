// Display preferences (D-39): theme, text size and motion. Per device, kept in
// localStorage; applied as attributes on <html> that app.css reads.
export type Theme = 'system' | 'light' | 'dark';
export type TextSize = 'md' | 'lg' | 'xl';
export type Motion = 'system' | 'reduce';

const KEY = 'qp:prefs';

function read(): { theme: Theme; text: TextSize; motion: Motion; timer: 'shown' | 'compact' } {
	const d = { theme: 'system' as Theme, text: 'md' as TextSize, motion: 'system' as Motion, timer: 'shown' as 'shown' | 'compact' };
	try {
		return { ...d, ...JSON.parse(localStorage.getItem(KEY) ?? '{}') };
	} catch {
		return d;
	}
}

class Prefs {
	theme = $state<Theme>('system');
	text = $state<TextSize>('md');
	motion = $state<Motion>('system');
	/** Quiz timer shown in full or as a compact pill (it expands by itself near the end). */
	timer = $state<'shown' | 'compact'>('shown');

	load() {
		Object.assign(this, read());
		this.apply();
	}
	set<K extends 'theme' | 'text' | 'motion' | 'timer'>(k: K, v: Prefs[K]) {
		(this as Prefs)[k] = v;
		try {
			localStorage.setItem(KEY, JSON.stringify({ theme: this.theme, text: this.text, motion: this.motion, timer: this.timer }));
		} catch {
			/* private mode: the choice lasts until reload */
		}
		this.apply();
	}
	apply() {
		if (typeof document === 'undefined') return;
		const el = document.documentElement;
		if (this.theme === 'system') el.removeAttribute('data-theme');
		else el.setAttribute('data-theme', this.theme === 'dark' ? 'quiz-dark' : 'quiz');
		if (this.text === 'md') el.removeAttribute('data-text');
		else el.setAttribute('data-text', this.text);
		if (this.motion === 'reduce') el.setAttribute('data-motion', 'reduce');
		else el.removeAttribute('data-motion');
		const meta = document.querySelector('meta[name="theme-color"]');
		const dark = this.theme === 'dark' || (this.theme === 'system' && typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches);
		meta?.setAttribute('content', dark ? '#0f151d' : '#f3f5f8');
	}
}

export const prefs = new Prefs();
