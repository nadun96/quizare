// Thin fetch wrapper for the Go API. Cookies are HttpOnly (__Host-sid), so
// no token is ever stored in JavaScript (ADR-13: no tokens in localStorage).

export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string,
		public fields: Record<string, string> = {}
	) {
		super(message);
	}
}

export type Fetch = typeof fetch;

async function request<T>(method: string, path: string, body?: unknown, f: Fetch = fetch): Promise<T> {
	const headers: Record<string, string> = { 'X-Requested-With': 'fetch' }; // CSRF header (ADR-13)
	let payload: BodyInit | undefined;
	if (body instanceof FormData || typeof body === 'string') {
		payload = body;
		if (typeof body === 'string') headers['Content-Type'] = 'text/csv';
	} else if (body !== undefined) {
		headers['Content-Type'] = 'application/json';
		payload = JSON.stringify(body);
	}
	let res: Response;
	try {
		res = await f(path, { method, headers, body: payload, credentials: 'same-origin' });
	} catch {
		throw new ApiError(0, 'network', 'You appear to be offline. We will retry.');
	}
	if (res.status === 204 || res.status === 202) return undefined as T;
	const text = await res.text();
	let data: any = undefined;
	try {
		data = text ? JSON.parse(text) : undefined;
	} catch {
		data = text;
	}
	if (!res.ok) {
		if (res.status === 503) {
			throw new ApiError(503, data?.code ?? 'busy', data?.message ?? 'The server is busy, please retry.');
		}
		throw new ApiError(res.status, data?.code ?? 'error', data?.message ?? res.statusText, data?.fields ?? {});
	}
	return data as T;
}

export const api = {
	get: <T>(p: string) => request<T>('GET', p),
	post: <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {}),
	put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b ?? {}),
	patch: <T>(p: string, b?: unknown) => request<T>('PATCH', p, b ?? {}),
	del: <T>(p: string, b?: unknown) => request<T>('DELETE', p, b),
	raw: request
};

/** Retry while the login queue is saturated (Argon2 pool returns 503 + Retry-After). */
export async function withBusyRetry<T>(fn: () => Promise<T>, attempts = 5, wait = (ms: number) => new Promise((r) => setTimeout(r, ms))): Promise<T> {
	for (let i = 0; ; i++) {
		try {
			return await fn();
		} catch (e) {
			if (!(e instanceof ApiError) || e.status !== 503 || i >= attempts - 1) throw e;
			await wait(1500 + Math.random() * 1500);
		}
	}
}

/** Only allow same-site relative redirects after login (avoids open redirects). */
export function safeNext(next: string | null | undefined, fallback = '/'): string {
	if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return fallback;
	return next;
}
