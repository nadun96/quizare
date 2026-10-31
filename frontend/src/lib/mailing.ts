// Email to groups (D-102): the placeholders a template may hold, filled in
// the way the server fills them, for the live preview while writing.

export const PLACEHOLDERS: { key: string; label: string; hint: string }[] = [
	{ key: 'first_name', label: 'First name', hint: 'Their first name, after any title (Ms, Dr…)' },
	{ key: 'name', label: 'Full name', hint: 'Their name as on their account' },
	{ key: 'email', label: 'Email', hint: 'Their email address' },
	{ key: 'role', label: 'Role', hint: 'student, teacher, manager or admin' },
	{ key: 'classroom', label: 'Classroom', hint: "The classroom's name, when sending to a classroom" },
	{ key: 'site_url', label: 'Site address', hint: "This site's address" }
];

const TOKEN = /\{\{\s*([A-Za-z_]+)\s*\}\}/g;
const TITLES = new Set(['mr', 'mrs', 'ms', 'miss', 'mx', 'dr', 'prof', 'rev', 'sir']);

export type Recipient = { name: string; email: string; role: string; classroom?: string };

/** "Ms Ann Perera" → "Ann", as the server does. */
export function firstName(name: string): string {
	const parts = name.trim().split(/\s+/);
	for (let i = 0; i < parts.length; i++) {
		if (TITLES.has(parts[i].toLowerCase().replace(/\.$/, '')) && i < parts.length - 1) continue;
		return parts[i];
	}
	return name;
}

/** Fills the placeholders in for one recipient. */
export function render(text: string, r: Recipient, site: string): string {
	return text.replace(TOKEN, (m, key: string) => {
		switch (key.toLowerCase()) {
			case 'name': return r.name;
			case 'first_name': return firstName(r.name);
			case 'email': return r.email;
			case 'role': return r.role;
			case 'classroom': return r.classroom ?? '';
			case 'site_url': return site;
		}
		return m;
	});
}

/** Placeholders in the text that don't exist, each once. */
export function unknownPlaceholders(text: string): string[] {
	const known = new Set(PLACEHOLDERS.map((p) => p.key));
	const out: string[] = [];
	for (const [, key] of text.matchAll(TOKEN)) if (!known.has(key.toLowerCase()) && !out.includes(key)) out.push(key);
	return out;
}

/** Puts a placeholder where the caret is (or replaces the selection); returns the text and the caret after it. */
export function insertAt(text: string, start: number, end: number, key: string): { text: string; caret: number } {
	const token = `{{${key}}}`;
	return { text: text.slice(0, start) + token + text.slice(end), caret: start + token.length };
}

/** Addresses from pasted text: commas, semicolons, spaces or lines between, "<…>" stripped, each once. */
export function parseEmails(text: string): string[] {
	const out: string[] = [];
	for (const raw of text.split(/[\s,;]+/)) {
		const e = raw.replace(/^<|>$/g, '').toLowerCase();
		if (e && !out.includes(e)) out.push(e);
	}
	return out;
}
