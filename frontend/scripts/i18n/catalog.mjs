// @ts-nocheck
// The list of everything to translate (D-157): every tr() in the app, labels and
// hints in code, and what the server says. node scripts/i18n/catalog.mjs writes it
// to src/lib/i18n/catalog/en.json; `export` and `import <file>` go to and from a
// spreadsheet, so that translators and teachers never need to touch code.
import { existsSync, mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const FRONT = join(here, '..', '..');
const SRC = join(FRONT, 'src');
const CATALOG = join(SRC, 'lib', 'i18n', 'catalog');
export const LOCALES = ['si', 'ta', 'hi', 'ar', 'fr', 'es'];

function walk(dir, test) {
	const out = [];
	for (const f of readdirSync(dir)) {
		if (f === 'node_modules' || f === '.git') continue;
		const p = join(dir, f);
		if (statSync(p).isDirectory()) out.push(...walk(p, test));
		else if (test(p)) out.push(p);
	}
	return out;
}

const decode = (body, quote) => {
	if (quote === '"') {
		try {
			return JSON.parse(`"${body}"`);
		} catch {
			/* fall through */
		}
	}
	return body.replace(/\\(['"`\\])/g, '$1').replace(/\\n/g, '\n');
};
const STR = String.raw`"((?:[^"\\\n]|\\.)*)"|'((?:[^'\\\n]|\\.)*)'`;

/** The messages in the app's source. */
export function appMessages() {
	const found = new Set();
	const files = walk(SRC, (p) => /\.(svelte|ts)$/.test(p) && !/\.test\.ts$/.test(p) && !/[\\/]i18n[\\/]/.test(p) && !/[\\/]docs[\\/]/.test(p));
	const tr = new RegExp(String.raw`\btr\(\s*(?:${STR})`, 'g');
	const prop = new RegExp(String.raw`\b(?:label|hint)\s*:\s*(?:${STR})`, 'g');
	for (const f of files) {
		const s = readFileSync(f, 'utf8');
		for (const m of s.matchAll(tr)) found.add(decode(m[1] ?? m[2], m[1] != null ? '"' : "'"));
		for (const m of s.matchAll(prop)) {
			const v = decode(m[1] ?? m[2], m[1] != null ? '"' : "'");
			if (/\p{L}{2,}/u.test(v)) found.add(v);
		}
	}
	return found;
}

/** What the platform and tutoring servers say to people. */
export function serverMessages() {
	const found = new Set();
	const root = join(FRONT, '..');
	const files = [join(root, 'backend', 'internal'), join(root, 'tutoring', 'internal')].flatMap((d) => walk(d, (p) => p.endsWith('.go') && !p.endsWith('_test.go')));
	const patterns = [
		/\b(?:httpx|web)\.(?:Conflict|BadRequest)\("((?:[^"\\]|\\.)*)"/g,
		/\b(?:httpx|web)\.NewError\([^,]+,\s*"[^"]*",\s*(?:fmt\.Sprintf\()?"((?:[^"\\]|\\.)*)"/g,
		/\b(?:bad|f|fields|errs)\["[\w.]+"\]\s*=\s*(?:fmt\.Sprintf\()?"((?:[^"\\]|\\.)*)"/g,
		/Invalid\(map\[string\]string\{\s*"[\w.]+":\s*"((?:[^"\\]|\\.)*)"/g
	];
	for (const f of files) {
		const s = readFileSync(f, 'utf8');
		for (const re of patterns) for (const m of s.matchAll(re)) if (/\p{L}{2,}/u.test(m[1])) found.add(JSON.parse(`"${m[1]}"`));
	}
	return found;
}

export function allMessages() {
	return [...new Set([...appMessages(), ...serverMessages()])].sort((a, b) => a.localeCompare(b));
}

const read = (loc) => (existsSync(join(CATALOG, `${loc}.json`)) ? JSON.parse(readFileSync(join(CATALOG, `${loc}.json`), 'utf8')) : {});
const sorted = (o) => Object.fromEntries(Object.entries(o).sort(([a], [b]) => a.localeCompare(b)));
const save = (loc, o) => writeFileSync(join(CATALOG, `${loc}.json`), JSON.stringify(sorted(o), null, '\t') + '\n');

function csvCell(v) {
	const s = String(v ?? '');
	return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}
function parseCSV(text) {
	const rows = [];
	let row = [];
	let cell = '';
	let inQ = false;
	for (let i = 0; i < text.length; i++) {
		const c = text[i];
		if (inQ) {
			if (c === '"' && text[i + 1] === '"') (cell += '"'), i++;
			else if (c === '"') inQ = false;
			else cell += c;
		} else if (c === '"') inQ = true;
		else if (c === ',') row.push(cell), (cell = '');
		else if (c === '\n') row.push(cell.replace(/\r$/, '')), rows.push(row), (row = []), (cell = '');
		else cell += c;
	}
	if (cell || row.length) row.push(cell), rows.push(row);
	return rows;
}

function run() {
	mkdirSync(CATALOG, { recursive: true });
	const [cmd, arg] = process.argv.slice(2);
	const all = allMessages();
	if (!cmd) {
		writeFileSync(join(CATALOG, 'en.json'), JSON.stringify(all, null, '\t') + '\n');
		const words = all.reduce((n, s) => n + s.split(/\s+/).length, 0);
		console.log(`${all.length} messages, ${words} words`);
		for (const l of LOCALES) {
			const d = read(l);
			console.log(l, all.filter((m) => d[m]).length);
		}
		return;
	}
	if (cmd === 'export') {
		const dicts = LOCALES.map(read);
		const lines = [['English', ...LOCALES].map(csvCell).join(',')];
		for (const m of all) lines.push([m, ...dicts.map((d) => d[m] ?? '')].map(csvCell).join(','));
		writeFileSync(arg ?? 'translations.csv', '﻿' + lines.join('\r\n') + '\r\n');
		console.log(`${all.length} rows in ${arg ?? 'translations.csv'}`);
		return;
	}
	if (cmd === 'import') {
		const rows = parseCSV(readFileSync(arg, 'utf8').replace(/^﻿/, ''));
		const head = rows.shift();
		const known = new Set(all);
		const bad = [];
		const places = (s) => new Set(s.match(/\{\d+\}|%[sdv]/g) ?? []);
		// A placeholder right after a letter only adds an English plural ending, so it may go.
		const needed = (s) => [...places(s)].filter((p) => !new RegExp('[A-Za-z]' + p.replace(/[{}]/g, '\\$&')).test(s));
		for (let c = 1; c < head.length; c++) {
			const loc = head[c].trim();
			if (!LOCALES.includes(loc)) continue;
			const d = read(loc);
			for (const r of rows) {
				const [en, v] = [r[0], r[c]];
				if (!en || !v || !known.has(en)) continue;
				const has = places(v);
				if (needed(en).some((p) => !has.has(p)) || [...has].some((p) => !places(en).has(p))) {
					bad.push(`${loc}: "${en}" must keep ${needed(en).join(' ')}`);
					continue;
				}
				d[en] = v;
			}
			save(loc, d);
		}
		console.log(bad.length ? `Not taken:\n${bad.join('\n')}` : 'All taken');
		return;
	}
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) run();
