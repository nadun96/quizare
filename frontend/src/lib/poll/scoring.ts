// Scored polls and leaderboards (V2-01, V2-02, D-42). The server marks
// answers; these helpers only shape keys in the editor and show results.
import type { Choice, Poll, PollBody, PollKey, PollQuestion, PollSettings, PollType } from './types';

export const SCORABLE: ReadonlySet<PollType> = new Set(['SINGLE', 'MULTI', 'MATCH', 'BLANK_OPT', 'BLANK_TEXT', 'DRAG', 'SHORT_TEXT', 'NUMBER', 'SLIDER', 'DATE', 'TIME']);

export const COMPETITION_DEFAULTS = { scoring: false, speed_bonus: false, leaderboard: 'presenter', show_answers: 'after_close', names: 'nickname' } as const;

export const DEFAULT_SETTINGS: PollSettings = { identity: 'anonymous', audience: 'anyone', pacing: 'self', show_results: 'after_answer', allow_edit: true, ...COMPETITION_DEFAULTS };

/** The editable settings of a poll. */
export function settingsOf(p: Poll): PollSettings {
	return {
		identity: p.identity, audience: p.audience, pacing: p.pacing, show_results: p.show_results, allow_edit: p.allow_edit,
		scoring: p.scoring ?? false, speed_bonus: p.speed_bonus ?? false, leaderboard: p.leaderboard ?? 'presenter',
		show_answers: p.show_answers ?? 'after_close', names: p.names ?? 'nickname'
	};
}

/** Drops parts of a key that no longer match the question (deleted options, blanks). */
export function cleanKey(type: PollType, body: PollBody, k: PollKey | null | undefined): PollKey | null {
	if (!k || !SCORABLE.has(type)) return null;
	const ids = (cs?: { id: string }[]) => new Set((cs ?? []).map((c) => c.id));
	const opts = ids(body.options);
	switch (type) {
		case 'SINGLE':
		case 'MULTI': {
			const correct = (k.correct ?? []).filter((id) => opts.has(id));
			return correct.length ? { correct: type === 'SINGLE' ? correct.slice(0, 1) : correct } : null;
		}
		case 'MATCH': {
			const left = ids(body.left), right = ids(body.right);
			const pairs = Object.fromEntries(Object.entries(k.pairs ?? {}).filter(([l, r]) => left.has(l) && right.has(r)));
			return Object.keys(pairs).length ? { pairs } : null;
		}
		case 'DRAG': {
			if (body.zones?.length) {
				const zones = ids(body.zones);
				const pairs = Object.fromEntries(Object.entries(k.pairs ?? {}).filter(([i, z]) => opts.has(i) && zones.has(z)));
				return Object.keys(pairs).length ? { pairs } : null;
			}
			const order = (k.order ?? []).filter((id) => opts.has(id));
			return order.length === opts.size ? { order } : null;
		}
		case 'BLANK_OPT':
		case 'BLANK_TEXT': {
			const blanks = new Set(body.blanks ?? []);
			const out: Record<string, string[]> = {};
			for (const [b, vs] of Object.entries(k.blanks ?? {})) {
				const keep = vs.map((v) => v.trim()).filter((v) => v && (type === 'BLANK_TEXT' || opts.has(v)));
				if (blanks.has(b) && keep.length) out[b] = keep;
			}
			return Object.keys(out).length ? { blanks: out, ...(type === 'BLANK_TEXT' && k.case_sensitive ? { case_sensitive: true } : {}) } : null;
		}
		case 'SHORT_TEXT':
		case 'DATE':
		case 'TIME': {
			const accepted = (k.accepted ?? []).map((v) => v.trim()).filter(Boolean);
			return accepted.length ? { accepted, ...(type === 'SHORT_TEXT' && k.case_sensitive ? { case_sensitive: true } : {}) } : null;
		}
		case 'NUMBER':
		case 'SLIDER':
			return typeof k.value === 'number' && Number.isFinite(k.value) ? { value: k.value, ...(k.tolerance ? { tolerance: Math.abs(k.tolerance) } : {}) } : null;
	}
	return null;
}

/** Seconds left on a timed question, given the server's clock offset; null if untimed. */
export function secondsLeft(startedAt: number | undefined, limitSec: number | null | undefined, now: number): number | null {
	if (!startedAt || !limitSec) return null;
	return Math.max(0, Math.ceil((startedAt + limitSec * 1000 - now) / 1000));
}

/** "1,250" or "87.5" points. */
export const fmtPoints = (n: number) => (Number.isInteger(n) ? n.toLocaleString() : n.toLocaleString(undefined, { maximumFractionDigits: 1 }));

/** "1st", "2nd", "23rd". */
export function ordinal(n: number): string {
	const teen = n % 100 >= 11 && n % 100 <= 13;
	return n + (teen ? 'th' : (['th', 'st', 'nd', 'rd'][n % 10] ?? 'th'));
}

/** The correct answer in words, for feedback after it is revealed. */
export function describeKey(q: PollQuestion, k: PollKey): string {
	const b = q.body;
	const name = (cs: Choice[] | undefined, id: string) => cs?.find((c) => c.id === id)?.text ?? id;
	switch (q.type) {
		case 'SINGLE':
		case 'MULTI':
			return (k.correct ?? []).map((id) => name(b.options, id)).join(', ');
		case 'MATCH':
			return Object.entries(k.pairs ?? {}).map(([l, r]) => `${name(b.left, l)} → ${name(b.right, r)}`).join('; ');
		case 'DRAG':
			if (k.order?.length) return k.order.map((id, i) => `${i + 1}. ${name(b.options, id)}`).join(', ');
			return Object.entries(k.pairs ?? {}).map(([i, z]) => `${name(b.options, i)} → ${name(b.zones, z)}`).join('; ');
		case 'BLANK_OPT':
			return Object.entries(k.blanks ?? {}).map(([bl, vs]) => `Blank ${bl}: ${name(b.options, vs[0])}`).join('; ');
		case 'BLANK_TEXT':
			return Object.entries(k.blanks ?? {}).map(([bl, vs]) => `Blank ${bl}: ${vs.join(' / ')}`).join('; ');
		case 'NUMBER':
		case 'SLIDER':
			return `${k.value}${k.tolerance ? ` ± ${k.tolerance}` : ''}${b.unit ? ' ' + b.unit : ''}`;
		default:
			return (k.accepted ?? []).join(' or ');
	}
}
