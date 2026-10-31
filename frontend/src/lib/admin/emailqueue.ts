// The email queue's statuses (D-104), and a send's progress in words.

export type Progress = Partial<Record<'waiting' | 'sent' | 'retrying' | 'failed', number>>;
export type Delivery = { id: number; to: string; subject: string; status: string; attempts: number; max_attempts: number; error?: string; send_id?: string; created_at: string; at?: string | null };

export const STATUS = [
	{ id: 'waiting', label: 'Waiting', word: 'waiting', badge: 'badge-ghost' },
	{ id: 'sent', label: 'Sent', word: 'sent', badge: 'badge-success' },
	{ id: 'retrying', label: 'Retrying', word: 'retrying', badge: 'badge-warning' },
	{ id: 'failed', label: 'Failed', word: 'failed', badge: 'badge-error' }
] as const;

/** "118 sent · 1 retrying · 1 failed", or "" once the queue no longer has them. */
export function progressText(p: Progress | undefined): string {
	if (!p) return '';
	return STATUS.filter((s) => (p[s.id] ?? 0) > 0)
		.map((s) => `${p[s.id]} ${s.word}`)
		.join(' · ');
}

/** Whether a send still has emails on their way. */
export const inFlight = (p: Progress | undefined) => (p?.waiting ?? 0) + (p?.retrying ?? 0) > 0;
