import { goto } from '$app/navigation';
import { page } from '$app/state';
import { auth, loginUrl, type Role } from './session.svelte';
import { isStaff } from './staff';

/** Call inside a component: redirects unless the user has the role. Returns a reactive "ready" check. */
export function requireRole(role: Role) {
	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) goto(loginUrl(page.url.pathname + page.url.search), { replaceState: true });
		else if (auth.user.role !== role) goto(auth.home(), { replaceState: true });
	});
	return () => auth.loaded && auth.user?.role === role;
}

/** Like requireRole, for the admin console: admins and managers (D-56). */
export function requireStaff() {
	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) goto(loginUrl(page.url.pathname + page.url.search), { replaceState: true });
		else if (!isStaff(auth.user)) goto(auth.home(), { replaceState: true });
	});
	return () => auth.loaded && isStaff(auth.user);
}
