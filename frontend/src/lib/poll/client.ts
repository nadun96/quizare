// Participant side of the poll API (D-40). Anonymous participants keep a
// device token per poll; it can only answer as that participant in that
// poll, so it is kept in localStorage (login sessions stay HttpOnly cookies).
import { ApiError } from '../api';
import type { BoardView, NewStroke, Stroke } from '../board/strokes.svelte';
import type { FileRef, PollAnswer, PollKey, PollScore, PublicPoll } from './types';

const KEY = (code: string) => 'qp:poll:' + code.toUpperCase();

export function getToken(code: string): string {
	try {
		return localStorage.getItem(KEY(code)) ?? '';
	} catch {
		return memory.get(code) ?? '';
	}
}
const memory = new Map<string, string>();
function setToken(code: string, token: string) {
	memory.set(code, token);
	try {
		localStorage.setItem(KEY(code), token);
	} catch {
		/* private mode: the token lasts for this page only */
	}
}

async function call<T>(code: string, method: string, path: string, body?: BodyInit, headers: Record<string, string> = {}): Promise<T> {
	const h: Record<string, string> = { 'X-Requested-With': 'fetch', ...headers };
	const tok = getToken(code);
	if (tok) h['X-Poll-Token'] = tok;
	let res: Response;
	try {
		res = await fetch('/api/polls/' + encodeURIComponent(code) + path, { method, headers: h, body, credentials: 'same-origin' });
	} catch {
		throw new ApiError(0, 'network', 'You appear to be offline. Try again.');
	}
	const text = await res.text();
	let data: any;
	try {
		data = text ? JSON.parse(text) : undefined;
	} catch {
		data = undefined;
	}
	if (!res.ok) throw new ApiError(res.status, data?.code ?? 'error', data?.message ?? res.statusText, data?.fields ?? {});
	return data as T;
}

const json = { 'Content-Type': 'application/json' };

export const pollClient = {
	view: (code: string) => call<PublicPoll>(code, 'GET', ''),
	// Whiteboard (D-47).
	board: (code: string) => call<BoardView>(code, 'GET', '/board'),
	boardAdd: async (code: string, strokes: NewStroke[]) => (await call<{ strokes: Stroke[] }>(code, 'POST', '/board/strokes', JSON.stringify({ strokes }), json)).strokes,
	boardErase: async (code: string, ids: number[], gesture = '') => (await call<{ removed: number[] }>(code, 'POST', '/board/erase', JSON.stringify({ ids, gesture }), json)).removed ?? [],
	async join(code: string, identify: boolean, nickname = '', groupId = '') {
		const r = await call<{ participant_id: string; identified: boolean; token?: string }>(code, 'POST', '/join', JSON.stringify({ identify, nickname, group_id: groupId }), { 'Content-Type': 'application/json' });
		if (r.token) setToken(code, r.token);
		return r;
	},
	answer: (code: string, questionId: string, value: PollAnswer) =>
		call<{ value: PollAnswer; key?: PollKey; score?: PollScore }>(code, 'PUT', '/answers/' + questionId, JSON.stringify({ value }), { 'Content-Type': 'application/json' }),
	/** Uploads a file answer; onProgress gets 0..1 where the browser reports it. */
	upload(code: string, questionId: string, file: Blob, name: string, onProgress?: (p: number) => void): Promise<{ value: { file: FileRef } }> {
		return new Promise((resolve, reject) => {
			const x = new XMLHttpRequest();
			x.open('POST', '/api/polls/' + encodeURIComponent(code) + '/files/' + questionId);
			x.setRequestHeader('X-Requested-With', 'fetch');
			x.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
			x.setRequestHeader('X-File-Name', encodeURIComponent(name));
			const tok = getToken(code);
			if (tok) x.setRequestHeader('X-Poll-Token', tok);
			x.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
			x.onload = () => {
				let data: any;
				try {
					data = JSON.parse(x.responseText);
				} catch {
					data = undefined;
				}
				if (x.status >= 200 && x.status < 300) resolve(data);
				else reject(new ApiError(x.status, data?.code ?? 'error', data?.fields?.file ?? data?.message ?? 'Upload failed', data?.fields ?? {}));
			};
			x.onerror = () => reject(new ApiError(0, 'network', 'Upload failed: check your connection'));
			x.send(file);
		});
	}
};
