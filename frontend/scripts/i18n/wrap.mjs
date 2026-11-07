// @ts-nocheck
// Wraps the words in Svelte components in tr() (D-157), so that they can be
// translated: node scripts/i18n/wrap.mjs [file...]. Safe to run again: what's
// wrapped already is left alone. The English stays in the code, as the key.
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import MagicString from 'magic-string';
import { hasWords, parseSvelte, TEXT_ATTRS, walkFragment } from './wrap-lib.mjs';

const ROOT = 'src';
const SKIP_PATH = /(^|[\\/])(docs)[\\/]|\.test\./;
const ASSIGNED = new Set(['error', 'notice', 'problem', 'message', 'err', 'status', 'hintText', 'warning']);
const DIALOG_KEYS = new Set(['title', 'body', 'confirm', 'label', 'placeholder', 'message', 'cancel']);
const SHOWN_MEMBERS = new Set(['label', 'hint']);
const CALLS = new Set(['toast']);

function files(dir) {
	const out = [];
	for (const f of readdirSync(dir)) {
		const p = join(dir, f);
		if (statSync(p).isDirectory()) out.push(...files(p));
		else if (f.endsWith('.svelte') && !SKIP_PATH.test(p)) out.push(p);
	}
	return out;
}

const q = (s) => JSON.stringify(s);
const norm = (s) => s.replace(/\s+/g, ' ');

function wrapFile(path) {
	const src = readFileSync(path, 'utf8');
	let ast;
	try {
		ast = parseSvelte(src);
	} catch (e) {
		console.log('SKIP (does not parse)', path, e.message);
		return 0;
	}
	const ms = new MagicString(src);
	let count = 0;
	const slice = (n) => src.slice(n.start, n.end);

	const flatten = (n) => (n.type === 'BinaryExpression' && n.operator === '+' ? [...flatten(n.left), ...flatten(n.right)] : [n]);
	const isStr = (n) => n.type === 'Literal' && typeof n.value === 'string';

	/** Builds tr(...) from pieces of text and expressions; null if there are no words. */
	function message(parts) {
		let text = '';
		const params = [];
		for (const p of parts) {
			if (typeof p === 'string') text += p;
			else if (isStr(p)) text += p.value;
			else {
				text += `{${params.length}}`;
				params.push(gen(p));
			}
		}
		text = norm(text);
		if (!hasWords(text)) return null;
		count++;
		return params.length ? `tr(${q(text)}, [${params.join(', ')}])` : `tr(${q(text)})`;
	}

	/** An operand: a conditional needs its brackets back. */
	const inner = (n) => (n.type === 'ConditionalExpression' || n.type === 'LogicalExpression' ? `(${gen(n)})` : gen(n));

	/** The expression with its words wrapped. */
	function gen(n) {
		switch (n.type) {
			case 'Literal':
				if (typeof n.value === 'string' && hasWords(n.value) && (/\s/.test(n.value) || /^[A-Z]/.test(n.value))) return message([n]) ?? slice(n);
				return slice(n);
			case 'TemplateLiteral': {
				const parts = [];
				n.quasis.forEach((x, i) => {
					parts.push(x.value.cooked);
					if (n.expressions[i]) parts.push(n.expressions[i]);
				});
				return message(parts) ?? slice(n);
			}
			case 'BinaryExpression': {
				if (n.operator !== '+') return slice(n);
				const parts = flatten(n);
				return parts.some(isStr) ? (message(parts) ?? slice(n)) : slice(n);
			}
			case 'ConditionalExpression':
				return `${slice(n.test)} ? ${inner(n.consequent)} : ${n.alternate.type === 'ConditionalExpression' ? gen(n.alternate) : inner(n.alternate)}`;
			case 'LogicalExpression':
				return `${n.left.type === 'LogicalExpression' || n.left.type === 'Literal' ? gen(n.left) : slice(n.left)} ${n.operator} ${inner(n.right)}`;
			case 'MemberExpression':
				if (!n.computed && SHOWN_MEMBERS.has(n.property.name)) {
					count++;
					return `tr(${slice(n)})`;
				}
				return slice(n);
			default:
				return slice(n);
		}
	}
	const already = (n) => n.type === 'CallExpression' && n.callee.type === 'Identifier' && (n.callee.name === 'tr' || n.callee.name === 't' || n.callee.name === 'tn');

	walkFragment(ast.fragment, {
		fragment(nodes) {
			// Runs of text and {expressions} read as one sentence.
			let run = [];
			const flush = () => {
				if (run.some((n) => n.type === 'Text' && hasWords(n.data)) || run.some((n) => n.type === 'ExpressionTag' && isStr(n.expression) && hasWords(n.expression.value) && run.length > 1)) {
					const first = run[0];
					const last = run[run.length - 1];
					const parts = run.map((n) => (n.type === 'Text' ? n.data : n.expression));
					const lead = first.type === 'Text' ? (first.raw.match(/^\s*/)[0] ?? '') : '';
					const trail = last.type === 'Text' ? (last.raw.match(/\s*$/)[0] ?? '') : '';
					if (typeof parts[0] === 'string') parts[0] = parts[0].trimStart();
					if (typeof parts[parts.length - 1] === 'string') parts[parts.length - 1] = parts[parts.length - 1].trimEnd();
					const made = message(parts);
					if (made) ms.overwrite(first.start, last.end, `${lead}{${made}}${trail}`);
								} else {
					for (const n of run) {
						if (n.type !== 'ExpressionTag' || already(n.expression)) continue;
						const made = gen(n.expression);
						if (made !== slice(n.expression)) ms.overwrite(n.expression.start, n.expression.end, made);
					}
				}
				run = [];
			};
			for (const n of nodes) {
				if (n.type === 'Text' || n.type === 'ExpressionTag') run.push(n);
				else if (n.type !== 'Comment') flush();
			}
			flush();
		},
		attribute(a) {
			if (a.type !== 'Attribute' || !TEXT_ATTRS.has(a.name) || a.value === true) return;
			const values = Array.isArray(a.value) ? a.value : [a.value];
			if (values.length === 1 && values[0].type === 'ExpressionTag') {
				const e = values[0].expression;
				if (already(e)) return;
				const made = gen(e);
				if (made !== slice(e)) ms.overwrite(e.start, e.end, made);
				return;
			}
			const made = message(values.map((v) => (v.type === 'Text' ? v.data : v.expression)));
			if (made) ms.overwrite(a.start, a.end, `${a.name}={${made}}`);
		}
	});

	// In the scripts: messages shown to people.
	const walkScript = (n) => {
		if (!n || typeof n !== 'object') return;
		if (Array.isArray(n)) return n.forEach(walkScript);
		if (n.type === 'CallExpression' && n.callee.type === 'Identifier') {
			if (already(n)) return;
			if (CALLS.has(n.callee.name) && n.arguments[0]) {
				const made = gen(n.arguments[0]);
				if (made !== slice(n.arguments[0])) ms.overwrite(n.arguments[0].start, n.arguments[0].end, made);
				return;
			}
			if ((n.callee.name === 'confirmDialog' || n.callee.name === 'promptDialog') && n.arguments[0]?.type === 'ObjectExpression') {
				for (const p of n.arguments[0].properties) {
					if (p.type === 'Property' && !p.computed && DIALOG_KEYS.has(p.key.name ?? p.key.value) && p.value.type !== 'Identifier') {
						const made = gen(p.value);
						if (made !== slice(p.value)) ms.overwrite(p.value.start, p.value.end, made);
					}
				}
				return;
			}
		}
		if (n.type === 'AssignmentExpression' && n.left.type === 'Identifier' && ASSIGNED.has(n.left.name)) {
			const made = gen(n.right);
			if (made !== slice(n.right)) ms.overwrite(n.right.start, n.right.end, made);
			return;
		}
		for (const k of Object.keys(n)) {
			if (k === 'start' || k === 'end' || k === 'loc' || k === 'type') continue;
			if (typeof n[k] === 'object') walkScript(n[k]);
		}
	};
	if (ast.instance) walkScript(ast.instance.content);

	if (!count && !ms.hasChanged()) return 0;
	let out = ms.toString();
	if (!/\bimport\s*\{[^}]*\btr\b[^}]*\}/.test(out)) {
		const line = "import { tr } from '$lib/i18n/i18n.svelte';";
		const m = out.match(/<script\b(?![^>]*module)[^>]*>\r?\n?/);
		out = m ? out.replace(m[0], m[0] + '\t' + line + '\n') : `<script lang="ts">\n\t${line}\n</script>\n\n` + out;
	}
	writeFileSync(path, out);
	return count;
}

const list = process.argv.slice(2).length ? process.argv.slice(2) : files(ROOT);
let total = 0;
let changed = 0;
for (const f of list) {
	const n = wrapFile(f);
	if (n) changed++;
	total += n;
}
console.log(`${total} messages in ${changed} of ${list.length} files`);
