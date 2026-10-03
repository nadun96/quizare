import { describe, expect, it, vi } from 'vitest';
import { ApiError, api, safeNext, withBusyRetry } from './api';
import { ServerClock, formatDuration } from './clock';
import { AnswerQueue, type Save } from './answerQueue';
import { Proctor, type ViolationKind } from './proctor';
import { LiveSocket } from './socket';

describe('api', () => {
	it('sends the CSRF header and JSON, and maps errors', async () => {
		const f = vi.fn(async (_p: string, init: RequestInit) => {
			expect((init.headers as Record<string, string>)['X-Requested-With']).toBe('fetch');
			expect(init.credentials).toBe('same-origin');
			return new Response(JSON.stringify({ code: 'validation_failed', message: 'bad', fields: { email: 'x' } }), { status: 422 });
		});
		const err: any = await api.raw('POST', '/api/x', { a: 1 }, f as unknown as typeof fetch).catch((e: any) => e);
		expect(err).toBeInstanceOf(ApiError);
		expect(err.status).toBe(422);
		expect(err.fields.email).toBe('x');
	});

	it('treats network failures as status 0', async () => {
		const f = vi.fn(async () => {
			throw new TypeError('offline');
		});
		const err: any = await api.raw('GET', '/api/x', undefined, f as unknown as typeof fetch).catch((e: any) => e);
		expect(err.status).toBe(0);
	});

	it('retries 503 busy responses from the login queue', async () => {
		let n = 0;
		const res = await withBusyRetry(async () => {
			if (n++ < 2) throw new ApiError(503, 'busy', 'busy');
			return 'ok';
		}, 5, async () => {});
		expect(res).toBe('ok');
		expect(n).toBe(3);
	});

	it('only allows relative same-site redirects', () => {
		expect(safeNext('/j/ABC123')).toBe('/j/ABC123');
		expect(safeNext('https://evil.example')).toBe('/');
		expect(safeNext('//evil.example')).toBe('/');
		expect(safeNext('/\\evil')).toBe('/');
		expect(safeNext(null, '/my')).toBe('/my');
	});
});

describe('ServerClock', () => {
	it('derives offset from the fastest round trip', () => {
		let now = 1000;
		const c = new ServerClock(() => now);
		c.sample(1000, 6050, 1100); // rtt 100 → offset 5000
		expect(c.serverNow()).toBe(6000);
		c.sample(2000, 7010, 2020); // rtt 20 → offset 5000
		c.sample(3000, 99999, 3900); // slow, noisy sample ignored
		now = 4000;
		expect(c.serverNow()).toBe(9000);
		expect(c.remaining(9500)).toBe(500);
		expect(c.remaining(8000)).toBe(0);
		expect(c.remaining(null)).toBeNull();
	});

	it('formats durations', () => {
		expect(formatDuration(59_001)).toBe('1:00');
		expect(formatDuration(5_000)).toBe('0:05');
		expect(formatDuration(3_600_000 + 61_000)).toBe('1:01:01');
		expect(formatDuration(null)).toBe('');
	});
});

describe('AnswerQueue', () => {
	function memStore() {
		const m = new Map<string, string>();
		return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), m };
	}

	it('keeps saves while offline and flushes the latest per question', async () => {
		let online = false;
		const sent: Save[] = [];
		const store = memStore();
		const q = new AnswerQueue('a1', async (s) => {
			if (!online) return 'retry';
			sent.push(s);
			return 'ok';
		}, store);
		q.enqueue('q1', { text: 'a' });
		q.enqueue('q1', { text: 'ab' });
		q.enqueue('q2', { selected: ['o1'] });
		await q.flush();
		expect(q.size).toBe(2);
		expect(store.m.get('a1')).toContain('ab');

		// A reload restores the queue from storage.
		const q2 = new AnswerQueue('a1', async (s) => {
			sent.push(s);
			return 'ok';
		}, store);
		expect(q2.size).toBe(2);
		online = true;
		expect(await q2.flush()).toBe(true);
		expect(sent.map((s) => s.questionId).sort()).toEqual(['q1', 'q2']);
		expect(sent.find((s) => s.questionId === 'q1')!.response).toEqual({ text: 'ab' });
		expect(store.m.has('a1')).toBe(false);
	});

	it('uses increasing sequence numbers', () => {
		const seqs: number[] = [];
		const q = new AnswerQueue('a2', async (s) => {
			seqs.push(s.seq);
			return 'ok';
		}, memStore());
		q.enqueue('q1', 1);
		q.enqueue('q1', 2);
		expect(q['pending'].get('q1')!.seq).toBeGreaterThan(seqs[0] ?? 0);
	});
});

describe('Proctor', () => {
	function setup(active = true) {
		const reported: string[] = [];
		const beacons: string[] = [];
		const p = new Proctor({
			report: (k: ViolationKind) => reported.push(k),
			beacon: (k: ViolationKind) => beacons.push(k),
			blurGraceMs: () => 2000,
			active: () => active
		});
		p.start();
		return { p, reported, beacons };
	}

	it('reports a hidden tab through the beacon', () => {
		const { p, beacons } = setup();
		Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
		document.dispatchEvent(new Event('visibilitychange'));
		expect(beacons).toEqual(['tab_hidden']);
		Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
		p.stop();
	});

	it('ignores short blurs within the grace period (R-01)', () => {
		vi.useFakeTimers();
		const { p, reported } = setup();
		window.dispatchEvent(new Event('blur'));
		vi.advanceTimersByTime(1000);
		window.dispatchEvent(new Event('focus'));
		vi.advanceTimersByTime(5000);
		expect(reported).toEqual([]);
		window.dispatchEvent(new Event('blur'));
		vi.advanceTimersByTime(2100);
		expect(reported).toEqual(['window_blur']);
		p.stop();
		vi.useRealTimers();
	});

	it('does nothing when the attempt is not running, and blocks paste while running', () => {
		const idle = setup(false);
		window.dispatchEvent(new Event('pagehide'));
		expect(idle.beacons).toEqual([]);
		idle.p.stop();
		const live = setup(true);
		const e = new Event('paste', { cancelable: true });
		document.dispatchEvent(e);
		expect(e.defaultPrevented).toBe(true);
		live.p.stop();
	});
});

describe('LiveSocket', () => {
	it('backs off exponentially with a cap', () => {
		expect(LiveSocket.backoff(0, () => 0.5)).toBe(500);
		expect(LiveSocket.backoff(3, () => 0.5)).toBe(4000);
		expect(LiveSocket.backoff(20, () => 0.5)).toBe(10_000);
	});

	it('measures clock offset from pongs and forwards other messages', () => {
		const fake: any = { readyState: 1, send: vi.fn(), close: vi.fn() };
		const s = new LiveSocket('/ws/x', undefined, () => fake);
		const got: string[] = [];
		s.onMessage = (m) => got.push(m.type);
		s.open();
		fake.onopen();
		const t = JSON.parse(fake.send.mock.calls[0][0]).t;
		fake.onmessage({ data: JSON.stringify({ type: 'pong', t, server_time: t + 60_000 }) });
		fake.onmessage({ data: JSON.stringify({ type: 'state', server_time: 1 }) });
		expect(got).toEqual(['state']);
		expect(Math.abs(s.clock.serverNow() - Date.now() - 60_000)).toBeLessThan(1000);
		s.close();
	});
});
