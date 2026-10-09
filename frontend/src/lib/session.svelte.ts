import { api, ApiError } from './api';

import type { Feature } from './staff';

export type Role = 'student' | 'teacher' | 'manager' | 'admin';
/** manager is set for managers, teacher-managers included (D-56). */
export type User = { id: string; email: string; name: string; role: Role; status: string; email_verified: boolean; avatar?: string; manager?: { features: Feature[] } };

class Auth {
	user = $state<User | null>(null);
	loaded = $state(false);

	async load() {
		try {
			this.user = await api.get<User>('/api/auth/me');
		} catch (e) {
			if (!(e instanceof ApiError) || e.status !== 401) console.warn(e);
			this.user = null;
		}
		this.loaded = true;
		return this.user;
	}

	async logout() {
		await api.post('/api/auth/logout');
		this.user = null;
	}

	home(): string {
		switch (this.user?.role) {
			case 'teacher':
				return '/t';
			case 'admin':
			case 'manager':
				return '/admin';
			case 'student':
				return '/my';
			default:
				return '/login';
		}
	}
}

export const auth = new Auth();

/** Redirect target for pages that need a login, preserving where to return (FR-ACC-06). */
export function loginUrl(next: string) {
	return '/login?next=' + encodeURIComponent(next);
}
