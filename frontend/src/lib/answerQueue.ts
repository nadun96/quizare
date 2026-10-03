// Offline-tolerant answer saving (UC-03 8a, NFR-11): every change is queued
// locally with an increasing sequence number and flushed to the server. If
// the network drops, saves wait and retry; the server ignores older seqs.

export type Save = { questionId: string; response: unknown; seq: number };
export type Sender = (s: Save) => Promise<'ok' | 'retry' | 'reject'>;

export interface Storage {
	getItem(k: string): string | null;
	setItem(k: string, v: string): void;
	removeItem(k: string): void;
}

export class AnswerQueue {
	private pending = new Map<string, Save>(); // latest save per question
	private seq: number;
	private flushing = false;
	onChange: (pending: number) => void = () => {};

	constructor(
		private key: string,
		private send: Sender,
		private store: Storage | null = typeof localStorage !== 'undefined' ? localStorage : null
	) {
		this.seq = Date.now(); // monotonic across reloads of the same attempt
		try {
			const raw = this.store?.getItem(key);
			if (raw) for (const s of JSON.parse(raw) as Save[]) this.pending.set(s.questionId, s);
		} catch {
			/* storage may be unavailable (private mode); keep working in memory */
		}
	}

	get size() {
		return this.pending.size;
	}

	private persist() {
		try {
			if (this.pending.size === 0) this.store?.removeItem(this.key);
			else this.store?.setItem(this.key, JSON.stringify([...this.pending.values()]));
		} catch {
			/* ignore */
		}
		this.onChange(this.pending.size);
	}

	enqueue(questionId: string, response: unknown) {
		this.seq = Math.max(this.seq + 1, Date.now());
		this.pending.set(questionId, { questionId, response, seq: this.seq });
		this.persist();
		void this.flush();
	}

	/** Send everything pending; returns true when the queue is empty. */
	async flush(): Promise<boolean> {
		if (this.flushing) return this.pending.size === 0;
		this.flushing = true;
		try {
			for (const s of [...this.pending.values()]) {
				const result = await this.send(s);
				if (result === 'retry') return false; // offline: keep everything for later
				// ok, or rejected (e.g. time is up): either way stop retrying this save,
				// unless a newer change for the same question arrived meanwhile.
				if (this.pending.get(s.questionId)?.seq === s.seq) this.pending.delete(s.questionId);
				this.persist();
			}
			return this.pending.size === 0;
		} finally {
			this.flushing = false;
		}
	}

	clear() {
		this.pending.clear();
		this.persist();
	}
}
