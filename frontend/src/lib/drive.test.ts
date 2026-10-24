import { describe, expect, it, vi } from 'vitest';
import { connectUrl, DRIVE_ERRORS, driveError, KINDS, loadPicker, picked, SHARING } from './drive';

describe('Google Drive', () => {
	it('explains every connect error, anything unknown as a failure', () => {
		for (const c of ['drive_expired', 'drive_cancelled', 'drive_scope', 'drive_off', 'drive_failed']) expect(DRIVE_ERRORS[c]).toBeTruthy();
		expect(driveError('weird')).toBe(DRIVE_ERRORS.drive_failed);
		expect(driveError(undefined)).toBe('');
	});

	it('connects and comes back to the page', () => {
		expect(connectUrl('/t/classrooms/1?tab=files')).toBe('/api/teacher/drive/connect?next=%2Ft%2Fclassrooms%2F1%3Ftab%3Dfiles');
	});

	it('reads the picker’s answer', () => {
		expect(picked({ action: 'loaded' })).toBeNull();
		expect(picked({ action: 'cancel' })).toEqual([]);
		expect(picked({ action: 'picked', docs: [{ id: 'a', name: 'Notes', mimeType: 'application/pdf', extra: 1 } as never] })).toEqual([
			{ id: 'a', name: 'Notes', mimeType: 'application/pdf' }
		]);
	});

	it('limits pickers to what each place takes', () => {
		expect(KINDS.questions).toContain('application/vnd.google-apps.spreadsheet');
		expect(KINDS.images.split(',').every((m) => m.startsWith('image/'))).toBe(true);
		expect(Object.keys(SHARING)).toEqual(['students', 'link', 'none']);
	});

	it('loads Google’s picker script once', async () => {
		const appended: HTMLScriptElement[] = [];
		const doc = { createElement: () => ({}) as HTMLScriptElement, head: { appendChild: (s: HTMLScriptElement) => appended.push(s) } } as unknown as Document;
		const win = {} as Window & { gapi?: { load: (l: string, cb: { callback: () => void }) => void }; google?: unknown };
		const p = loadPicker(doc, win);
		expect(loadPicker(doc, win)).toBe(p);
		expect(appended).toHaveLength(1);
		expect(appended[0].src).toBe('https://apis.google.com/js/api.js');
		const load = vi.fn((_l: string, cb: { callback: () => void }) => cb.callback());
		win.gapi = { load };
		win.google = { picker: {} };
		appended[0].onload!(new Event('load'));
		await expect(p).resolves.toEqual({ picker: {} });
		expect(load).toHaveBeenCalledWith('picker', expect.anything());
	});
});
