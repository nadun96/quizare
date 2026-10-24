// Sign-in with OpenID Connect providers (D-70): what the pages share.

export type SSOOption = { id: string; name: string };

export type SSOProvider = {
	id?: string;
	name: string;
	enabled: boolean;
	issuer: string;
	client_id: string;
	client_secret?: string;
	client_secret_set?: boolean;
	clear_secret?: boolean;
	scopes: string[];
	email_claim: string;
	trust_email: boolean;
	allowed_domains: string[];
	new_accounts: 'none' | 'student' | 'teacher';
	teacher_claim: string;
	teacher_values: string[];
	link_by_email: boolean;
	updated_at?: string;
};

export type Identity = { provider_id: string; name: string; email: string; created_at: string; last_used_at: string };

/** Where a button sends the browser. `next` comes back after sign-in. */
export function startUrl(id: string, next?: string | null): string {
	return `/api/auth/oidc/${encodeURIComponent(id)}/start` + (next ? '?next=' + encodeURIComponent(next) : '');
}

/** Why a sign-in came back without signing in (the callback's sso_error codes). */
export const SSO_ERRORS: Record<string, string> = {
	sso_cancelled: 'Sign-in was cancelled.',
	sso_expired: 'That sign-in took too long or was started in another tab. Try again.',
	sso_no_account: 'There’s no account here for that sign-in. Ask your teacher or administrator to create one, or create an account below.',
	sso_email_unverified: 'The provider didn’t confirm your email address, so it can’t be matched to an account.',
	sso_conflict: 'An account with that email already exists. Log in with its password, then connect this sign-in from your account page.',
	sso_taken: 'That sign-in already belongs to another account.',
	sso_domain: 'That account’s email domain isn’t allowed here. Use your school account.',
	sso_unavailable: 'That sign-in option isn’t available right now. Try again shortly.',
	account_suspended: 'This account is suspended.',
	pending_approval: 'Your teacher account is waiting for an administrator’s approval.',
	sso_failed: 'Sign-in didn’t work. Try again, or ask your administrator.'
};

export function ssoError(code: string | null | undefined): string {
	if (!code) return '';
	return SSO_ERRORS[code] ?? SSO_ERRORS.sso_failed;
}

/** Starting points for the common providers; the admin fills in the rest. */
export const PRESETS: { id: string; label: string; hint: string; p: Partial<SSOProvider> }[] = [
	{ id: 'google', label: 'Google', hint: 'Google Workspace or Gmail. Limit it to your school’s domain below.',
		p: { name: 'Google', issuer: 'https://accounts.google.com', email_claim: 'email', trust_email: false, scopes: ['openid', 'email', 'profile'] } },
	{ id: 'microsoft', label: 'Microsoft', hint: 'Microsoft 365 / Entra ID. Replace TENANT-ID with your directory (tenant) ID.',
		p: { name: 'Microsoft', issuer: 'https://login.microsoftonline.com/TENANT-ID/v2.0', email_claim: 'preferred_username', trust_email: true, scopes: ['openid', 'email', 'profile'] } },
	{ id: 'keycloak', label: 'Keycloak', hint: 'Your Keycloak address and realm.',
		p: { name: 'School account', issuer: 'https://keycloak.example.edu/realms/school', email_claim: 'email', trust_email: false, scopes: ['openid', 'email', 'profile'] } },
	{ id: 'okta', label: 'Okta', hint: 'Your Okta domain.',
		p: { name: 'Okta', issuer: 'https://example.okta.com', email_claim: 'email', trust_email: false, scopes: ['openid', 'email', 'profile', 'groups'] } },
	{ id: 'other', label: 'Other', hint: 'Any OpenID Connect provider.', p: { name: '', issuer: '', email_claim: 'email', trust_email: false, scopes: ['openid', 'email', 'profile'] } }
];

export function blankProvider(preset = 'google'): SSOProvider {
	const p = PRESETS.find((x) => x.id === preset)?.p ?? {};
	return {
		name: '', enabled: false, issuer: '', client_id: '', client_secret: '', scopes: ['openid', 'email', 'profile'], email_claim: 'email',
		trust_email: false, allowed_domains: [], new_accounts: 'none', teacher_claim: '', teacher_values: [], link_by_email: true, ...p
	};
}

/** List fields edit as text: one item per line, or separated by commas or spaces. */
export function toList(text: string): string[] {
	return [...new Set(text.split(/[\s,]+/).map((v) => v.trim()).filter(Boolean))];
}

export function fromList(list: string[] | null | undefined, sep = '\n'): string {
	return (list ?? []).join(sep);
}

/** A sentence on the provider's card: who gets in, and as what. */
export function describe(p: SSOProvider): string {
	const domains = p.allowed_domains.length ? ` from ${p.allowed_domains.join(', ')}` : '';
	const who =
		p.new_accounts === 'none'
			? `People with an account here${domains}`
			: p.new_accounts === 'teacher'
				? `Anyone${domains}; new accounts are teachers`
				: `Anyone${domains}; new accounts are students${p.teacher_claim ? `, or teachers by ${p.teacher_claim}` : ''}`;
	return who + '.';
}
