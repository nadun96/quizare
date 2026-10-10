import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api';
import { looksLikeComputer, TutorClient } from './client';
import { audienceLabel, handQueue, linkParts, type ChatMessage, type TutoringParticipant } from './types';

// Tutoring in the browser (D-59).

const token = (n: number, inMs: number, now: number) => ({ token: 't' + n, expires_at: new Date(now + inMs).toISOString(), url: 'https://tutor.example.edu' });

describe('TutorClient', () => {
	it('reuses a token until a minute before it runs out, and asks once at a time', async () => {
		let now = 1_000_000;
		let n = 0;
		const fetchToken = vi.fn(async () => token(++n, 5 * 60_000, now));
		const c = new TutorClient(fetchToken, () => now);
		const [a, b] = await Promise.all([c.token(), c.token()]);
		expect(a).toBe('t1');
		expect(b).toBe('t1');
		expect(fetchToken).toHaveBeenCalledTimes(1);
		now += 3 * 60_000;
		expect(await c.token()).toBe('t1');
		now += 61_000; // under a minute left
		expect(await c.token()).toBe('t2');
		expect(c.base).toBe('https://tutor.example.edu');
	});

	it('sends the token as a bearer and passes the service’s errors through', async () => {
		const now = Date.now();
		const f = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
			expect((init!.headers as Record<string, string>).Authorization).toBe('Bearer t1');
			return new Response(JSON.stringify({ code: 'session_locked', message: 'this session is locked' }), { status: 403 });
		});
		const c = new TutorClient(async () => token(1, 300_000, now), Date.now, f as typeof fetch);
		await expect(c.post('/api/join/ABC')).rejects.toMatchObject({ status: 403, code: 'session_locked' });
		expect(f.mock.calls[0][0]).toBe('https://tutor.example.edu/api/join/ABC');
		const offline = new TutorClient(async () => token(1, 300_000, now), Date.now, (async () => {
			throw new TypeError('failed');
		}) as typeof fetch);
		await expect(offline.get('/api/sessions')).rejects.toBeInstanceOf(ApiError);
	});
});

describe('chat text', () => {
	it('splits links out of plain text, never HTML (TS-FR-43)', () => {
		expect(linkParts('<b>see</b> https://example.com/a?b=1. ok')).toEqual([
			{ text: '<b>see</b> ' },
			{ text: 'https://example.com/a?b=1', href: 'https://example.com/a?b=1' },
			{ text: '. ok' }
		]);
		expect(linkParts('javascript:alert(1)')).toEqual([{ text: 'javascript:alert(1)' }]);
		expect(linkParts('no links')).toEqual([{ text: 'no links' }]);
	});

	it('labels private messages', () => {
		const m = (x: Partial<ChatMessage>) => ({ id: 1, user_id: 'a', name: 'A', from_teacher: false, audience: 'everyone', text: 'x', created_at: '', ...x }) as ChatMessage;
		expect(audienceLabel(m({ audience: 'teachers' }), 'a')).toBe('to the teacher');
		expect(audienceLabel(m({ audience: 'teachers' }), 't')).toBe('question to teachers');
		expect(audienceLabel(m({ audience: 'one', to_user: 'b' }), 'b')).toBe('private, to you');
		expect(audienceLabel(m({}), 'a')).toBe('');
	});
});

describe('raised hands', () => {
	it('queue in the order hands went up (TS-FR-24)', () => {
		const p = (name: string, hand?: string, state = 'admitted') => ({ user_id: name, name, role: 'student', state, hand_at: hand }) as TutoringParticipant;
		const q = handQueue([p('Cara', '2026-10-10T10:00:03Z'), p('Ann', '2026-10-10T10:00:01Z'), p('Bob'), p('Dan', '2026-10-10T10:00:00Z', 'removed')]);
		expect(q.map((x) => x.name)).toEqual(['Ann', 'Cara']);
	});
});

describe('computers only (TS-FR-09)', () => {
	const w = (width: number, coarse: boolean, fine: boolean) => ({
		innerWidth: width,
		matchMedia: (q: string) => ({ matches: q.includes('any-pointer: fine') ? fine : q.includes('pointer: coarse') ? coarse : false })
	});
	it('lets laptops in and keeps phones and tablets out', () => {
		expect(looksLikeComputer(w(1440, false, true), 'Mozilla/5.0 (Windows NT 10.0)')).toBe(true);
		expect(looksLikeComputer(w(390, true, false), 'iPhone')).toBe(false);
		expect(looksLikeComputer(w(1024, true, false), 'Mozilla/5.0 (iPad)')).toBe(false);
		// A touch laptop has a fine pointer too.
		expect(looksLikeComputer(w(1366, true, true), 'Mozilla/5.0 (Windows NT 10.0)')).toBe(true);
	});
});
