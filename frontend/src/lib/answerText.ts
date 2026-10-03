import type { Body, Key, QType, Response } from './types';

const names = (b: Body) => Object.fromEntries([...(b.options ?? []), ...(b.left ?? []), ...(b.right ?? []), ...(b.zones ?? [])].map((c) => [c.id, c.text]));

/** Human-readable form of a student's response. */
export function describeResponse(type: QType, body: Body, r: Response | null | undefined): string {
	if (!r) return '—';
	const n = names(body);
	switch (type) {
		case 'SINGLE':
		case 'MULTI':
			return (r.selected ?? []).map((id) => n[id]).join(', ') || '—';
		case 'MATCH':
			return Object.entries(r.pairs ?? {}).map(([l, rt]) => `${n[l]} → ${n[rt]}`).join('; ') || '—';
		case 'BLANK_OPT':
		case 'BLANK_TEXT':
			return (body.blanks ?? []).map((b) => `[${b}] ${n[r.blanks?.[b] ?? ''] ?? r.blanks?.[b] ?? '—'}`).join('  ');
		case 'DRAG':
			if (body.zones?.length) return Object.entries(r.pairs ?? {}).map(([i, z]) => `${n[i]} → ${n[z]}`).join('; ') || '—';
			return (r.order ?? []).map((id) => n[id]).join(' → ') || '—';
		case 'ESSAY':
			return r.text || '—';
	}
}

/** Human-readable form of the answer key. */
export function describeKey(type: QType, body: Body, k: Key | null | undefined): string {
	if (!k) return '';
	const n = names(body);
	switch (type) {
		case 'SINGLE':
		case 'MULTI':
			return (k.correct ?? []).map((id) => n[id]).join(', ');
		case 'MATCH':
			return Object.entries(k.pairs ?? {}).map(([l, r]) => `${n[l]} → ${n[r]}`).join('; ');
		case 'BLANK_OPT':
			return (body.blanks ?? []).map((b) => `[${b}] ${n[k.blanks?.[b]?.[0] ?? '']}`).join('  ');
		case 'BLANK_TEXT':
			return (body.blanks ?? []).map((b) => `[${b}] ${(k.blanks?.[b] ?? []).join(' / ')}`).join('  ');
		case 'DRAG':
			if (body.zones?.length) return Object.entries(k.pairs ?? {}).map(([i, z]) => `${n[i]} → ${n[z]}`).join('; ');
			return (k.order ?? []).map((id) => n[id]).join(' → ');
		case 'ESSAY':
			return k.model_answer ?? '';
	}
}
