// The tutoring service's client (D-59). Tutoring is a separate service at its
// own address; the platform signs a 5-minute token naming the person
// (ADR-23), which every call carries as a bearer token (never a cookie, so
// there is nothing to forge across sites). Tokens are renewed a minute
// before they run out, so logging out of the platform ends tutoring access
// within minutes (TS-NFR-52).
import { api, ApiError } from '../api';

export type TutoringConfig = { enabled: boolean; url?: string };

let config: Promise<TutoringConfig> | null = null;

/** Whether tutoring is on, and where; asked once per page load. */
export function tutoringConfig(): Promise<TutoringConfig> {
	config ??= api.get<TutoringConfig>('/api/tutoring/config').catch(() => ({ enabled: false }));
	return config;
}

type Token = { token: string; expires_at: string; url: string };
type Clock = () => number;

/** Keeps a valid platform token and calls the tutoring service with it. */
export class TutorClient {
	private current: (Token & { exp: number }) | null = null;
	private pending: Promise<string> | null = null;

	constructor(
		private fetchToken: () => Promise<Token> = () => api.post<Token>('/api/tutoring/token'),
		private now: Clock = Date.now,
		private f: typeof fetch = (...a) => fetch(...a)
	) {}

	get base(): string {
		return this.current?.url ?? '';
	}

	/** A token valid for at least another minute. */
	async token(): Promise<string> {
		if (this.current && this.current.exp - this.now() > 60_000) return this.current.token;
		this.pending ??= this.fetchToken()
			.then((t) => {
				this.current = { ...t, exp: Date.parse(t.expires_at) };
				return t.token;
			})
			.finally(() => (this.pending = null));
		return this.pending;
	}

	async call<T>(method: string, path: string, body?: unknown): Promise<T> {
		const token = await this.token();
		let res: Response;
		try {
			res = await this.f(this.base + path, {
				method,
				headers: { Authorization: 'Bearer ' + token, ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
				body: body !== undefined ? JSON.stringify(body) : undefined
			});
		} catch {
			throw new ApiError(0, 'network', "Tutoring isn't reachable right now. We'll keep trying.");
		}
		if (res.status === 204) return undefined as T;
		const text = await res.text();
		let data: any;
		try {
			data = text ? JSON.parse(text) : undefined;
		} catch {
			data = text;
		}
		if (!res.ok) throw new ApiError(res.status, data?.code ?? 'error', data?.message ?? res.statusText, data?.fields ?? {});
		return data as T;
	}

	get = <T>(p: string) => this.call<T>('GET', p);
	post = <T>(p: string, b: unknown = {}) => this.call<T>('POST', p, b);
	put = <T>(p: string, b: unknown = {}) => this.call<T>('PUT', p, b);
	del = <T>(p: string) => this.call<T>('DELETE', p);

	/** Downloads a file (attendance CSV) with the token, then saves it. */
	async download(path: string, name: string) {
		const token = await this.token();
		const res = await this.f(this.base + path, { headers: { Authorization: 'Bearer ' + token } });
		if (!res.ok) throw new ApiError(res.status, 'error', 'Download failed');
		const url = URL.createObjectURL(await res.blob());
		const a = document.createElement('a');
		a.href = url;
		a.download = name;
		document.body.append(a);
		a.click();
		a.remove();
		setTimeout(() => URL.revokeObjectURL(url), 10_000);
	}
}

export type SocketEvent = { type: string; data?: any };

/** Reasons the service closes a socket for good; any other drop reconnects. */
export const FINAL = new Set(['removed', 'refused', 'ended', 'replaced']);

/**
 * The live connection to one session: state, chat and requests arrive
 * here; actions go through the REST API. It sends the token first (a
 * WebSocket can't carry headers), renews it every 4 minutes, and reconnects
 * after drops with a growing delay, up to 10 s (TS-NFR-10).
 */
export class TutorSocket {
	private ws: WebSocket | null = null;
	private renew: ReturnType<typeof setInterval> | undefined;
	private retry: ReturnType<typeof setTimeout> | undefined;
	private attempts = 0;
	private stopped = false;
	connected = false;

	constructor(
		private client: TutorClient,
		private sessionId: string,
		private on: (e: SocketEvent) => void,
		private onStatus: (s: 'open' | 'reconnecting' | 'closed', reason?: string) => void = () => {}
	) {}

	async open() {
		if (this.stopped) return;
		const token = await this.client.token();
		const url = this.client.base.replace(/^http/, 'ws') + '/ws/sessions/' + this.sessionId;
		const ws = new WebSocket(url);
		this.ws = ws;
		ws.onopen = () => {
			ws.send(JSON.stringify({ token }));
			this.attempts = 0;
			this.connected = true;
			this.onStatus('open');
			clearInterval(this.renew);
			this.renew = setInterval(async () => ws.readyState === WebSocket.OPEN && ws.send(JSON.stringify({ token: await this.client.token() })), 240_000);
		};
		ws.onmessage = (m) => {
			try {
				const e = JSON.parse(m.data) as SocketEvent;
				if (e.type === 'closed' && FINAL.has(e.data?.reason)) this.stopped = true;
				this.on(e);
			} catch {
				/* ignore */
			}
		};
		ws.onclose = (e) => {
			clearInterval(this.renew);
			this.connected = false;
			if (this.ws !== ws) return;
			if (this.stopped || FINAL.has(e.reason)) {
				this.stopped = true;
				this.onStatus('closed', e.reason);
				return;
			}
			this.onStatus('reconnecting');
			const delay = Math.min(10_000, 500 * 2 ** this.attempts++);
			this.retry = setTimeout(() => this.open().catch(() => this.onStatus('reconnecting')), delay);
		};
	}

	close() {
		this.stopped = true;
		clearInterval(this.renew);
		clearTimeout(this.retry);
		this.ws?.close();
	}
}

/** Phones and tablets can't join (PO-4, TS-FR-09): by capabilities and screen, not only the user agent. */
export function looksLikeComputer(w: { innerWidth: number; matchMedia?: (q: string) => { matches: boolean } }, ua = ''): boolean {
	const coarse = w.matchMedia?.('(pointer: coarse)').matches ?? false;
	const fine = w.matchMedia?.('(any-pointer: fine)').matches ?? true;
	const mobileUA = /Android|iPhone|iPad|iPod|Mobile/i.test(ua);
	if (w.innerWidth < 800) return false;
	if (coarse && !fine) return false;
	return !(mobileUA && coarse);
}
