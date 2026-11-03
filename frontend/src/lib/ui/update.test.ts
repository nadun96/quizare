import { describe, expect, it } from 'vitest';
import { reloadTarget } from './update';

describe('a new version on the server (D-137)', () => {
	const to = { url: { href: 'https://x/t/bank' } };
	it('loads the next page in full once a new version is out', () => {
		expect(reloadTarget(true, { willUnload: false, to })).toBe('https://x/t/bank');
		expect(reloadTarget(false, { willUnload: false, to })).toBeNull();
	});
	it('leaves alone a page already unloading, or leaving the site', () => {
		expect(reloadTarget(true, { willUnload: true, to })).toBeNull();
		expect(reloadTarget(true, { willUnload: false, to: null })).toBeNull();
	});
});
