import { describe as group, expect, it } from 'vitest';
import { blankProvider, describe, fromList, PRESETS, ssoError, SSO_ERRORS, startUrl, toList } from './sso';

group('sign-in providers', () => {
	it('builds start links that carry where to go next', () => {
		expect(startUrl('abc')).toBe('/api/auth/oidc/abc/start');
		expect(startUrl('abc', '/j/K7Q2?x=1')).toBe('/api/auth/oidc/abc/start?next=%2Fj%2FK7Q2%3Fx%3D1');
	});

	it('explains every callback error, and anything unknown as a failure', () => {
		for (const code of ['sso_cancelled', 'sso_expired', 'sso_no_account', 'sso_email_unverified', 'sso_conflict', 'sso_taken', 'sso_domain', 'sso_unavailable', 'account_suspended', 'pending_approval', 'sso_failed']) {
			expect(SSO_ERRORS[code]).toBeTruthy();
		}
		expect(ssoError('nonsense')).toBe(SSO_ERRORS.sso_failed);
		expect(ssoError(null)).toBe('');
	});

	it('edits lists as text', () => {
		expect(toList(' school.edu,\nstaff.school.edu  school.edu ')).toEqual(['school.edu', 'staff.school.edu']);
		expect(toList('')).toEqual([]);
		expect(fromList(['a', 'b'], ', ')).toBe('a, b');
		expect(fromList(undefined)).toBe('');
	});

	it('starts from a preset, never creating accounts by default', () => {
		const ms = blankProvider('microsoft');
		expect(ms.issuer).toContain('login.microsoftonline.com');
		expect(ms.email_claim).toBe('preferred_username');
		expect(ms.new_accounts).toBe('none');
		expect(ms.link_by_email).toBe(true);
		expect(PRESETS.map((p) => p.id)).toContain('other');
	});

	it('says who gets in', () => {
		const p = blankProvider('google');
		expect(describe(p)).toBe('People with an account here.');
		expect(describe({ ...p, new_accounts: 'student', allowed_domains: ['school.edu'], teacher_claim: 'groups' })).toBe(
			'Anyone from school.edu; new accounts are students, or teachers by groups.'
		);
		expect(describe({ ...p, new_accounts: 'teacher' })).toBe('Anyone; new accounts are teachers.');
	});
});
