// The inbox (D-103): unread announcements and messages for the bar's badge,
// refreshed after each navigation (at most every 30 seconds) and every
// minute while the page is visible.
import { api } from '../api';

export type Summary = { announcements: number; messages: number; email: boolean };
export type Person = { id: string; name: string; avatar?: string; classrooms?: string };
export type Announcement = {
	id: string;
	classroom_id: string;
	classroom: string;
	author: string;
	author_id: string | null;
	avatar?: string;
	title: string;
	body: string;
	emailed: boolean;
	created_at: string;
	updated_at: string;
	read: boolean;
	reads: number;
	students: number;
};
export type Conversation = { id: string; with: Person; last: string; last_mine: boolean; last_at: string; unread: number; can_reply: boolean };
export type Message = { id: string; mine: boolean; sender: string; body: string; created_at: string };

const REFRESH = 30_000;

class Inbox {
	current = $state<Summary | null>(null);
	private at = 0;
	private timer: ReturnType<typeof setInterval> | undefined;

	get unread() {
		return (this.current?.announcements ?? 0) + (this.current?.messages ?? 0);
	}

	async load(force = false) {
		if (!force && Date.now() - this.at < REFRESH) return;
		this.at = Date.now();
		try {
			this.current = await api.get<Summary>('/api/my/inbox');
		} catch {
			// The badge waits for the next try.
		}
	}

	start() {
		this.load(true);
		this.timer ??= setInterval(() => {
			if (typeof document === 'undefined' || document.visibilityState === 'visible') this.load(true);
		}, 60_000);
	}

	clear() {
		clearInterval(this.timer);
		this.timer = undefined;
		this.current = null;
		this.at = 0;
	}
}

export const inbox = new Inbox();

/** "now", "5 min", "3:41 PM" today, "Mon" this week, "12 Oct" this year, then "12 Oct 2025". */
export function when(iso: string, now = new Date()): string {
	const d = new Date(iso);
	const s = (now.getTime() - d.getTime()) / 1000;
	if (s < 60) return 'now';
	if (s < 3600) return `${Math.floor(s / 60)} min`;
	if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
	if (s < 6 * 86400) return d.toLocaleDateString(undefined, { weekday: 'short' });
	if (d.getFullYear() === now.getFullYear()) return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
	return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
}

/** "9+" past nine, so the badge stays small. */
export const badge = (n: number) => (n > 9 ? '9+' : String(n));
