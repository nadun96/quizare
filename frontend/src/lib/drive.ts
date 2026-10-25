// A teacher's Google Drive (D-71): status, messages, and Google's file
// picker. The platform only reaches files picked here or made by it
// (drive.file), so every file a teacher uses passes through the picker.
import { api } from './api';

export type DriveStatus = {
	available: boolean;
	connected: boolean;
	email?: string;
	reconnect?: boolean;
	auto_backup: boolean;
	folder_link?: string;
	connected_at?: string;
};

export type DriveExport = {
	id: string;
	kind: 'session' | 'poll';
	target_id: string;
	name: string;
	web_link?: string;
	automatic: boolean;
	status: 'done' | 'failed';
	error?: string;
	created_at: string;
};

export type ClassroomFile = {
	id: string;
	file_id?: string;
	name: string;
	mime_type: string;
	web_link: string;
	icon_link?: string;
	sharing?: 'students' | 'link' | 'none';
	shared_with?: number;
	synced_at?: string;
	sync_error?: string;
	created_at: string;
};

export type Picked = { id: string; name: string; mimeType: string };

/** Why connecting came back without connecting (the callback's drive_error codes). */
export const DRIVE_ERRORS: Record<string, string> = {
	drive_expired: 'Connecting took too long or was started in another tab. Try again.',
	drive_cancelled: 'You didn’t allow access, so Drive isn’t connected.',
	drive_scope: 'Google Drive was unticked on Google’s page. Connect again and leave it ticked.',
	drive_off: 'Google Drive isn’t set up on this platform. Ask your administrator.',
	drive_failed: 'Connecting to Google Drive didn’t work. Try again shortly.'
};

export function driveError(code: string | null | undefined): string {
	if (!code) return '';
	return DRIVE_ERRORS[code] ?? DRIVE_ERRORS.drive_failed;
}

export const connectUrl = (next: string) => '/api/teacher/drive/connect?next=' + encodeURIComponent(next);

/** The mime types each picker shows. */
export const KINDS = {
	any: '',
	images: 'image/png,image/jpeg,image/gif,image/webp,image/svg+xml',
	questions: 'text/csv,application/vnd.google-apps.spreadsheet',
	// Presentations (D-78): Google Slides, PDFs, PowerPoint, OpenDocument.
	slides: 'application/vnd.google-apps.presentation,application/pdf,application/vnd.openxmlformats-officedocument.presentationml.presentation,application/vnd.ms-powerpoint,application/vnd.oasis.opendocument.presentation'
} as const;
export type PickKind = keyof typeof KINDS;

// Asked once per page: whether to show Drive buttons at all.
let statusOnce: Promise<DriveStatus> | null = null;
export function driveStatus(fresh = false): Promise<DriveStatus> {
	if (fresh || !statusOnce) {
		statusOnce = api.get<DriveStatus>('/api/teacher/drive').catch(() => ({ available: false, connected: false, auto_backup: false }));
	}
	return statusOnce;
}

/** Turns the picker's answer into the files picked; cancelling picks none. */
export function picked(data: { action?: string; docs?: { id: string; name: string; mimeType: string }[] }): Picked[] | null {
	if (data.action === 'picked') return (data.docs ?? []).map((d) => ({ id: d.id, name: d.name, mimeType: d.mimeType }));
	if (data.action === 'cancel') return [];
	return null; // loaded, and other steps on the way
}

type Gapi = { load: (lib: string, cb: { callback: () => void; onerror: () => void }) => void };
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Google = any;

let loading: Promise<Google> | null = null;

/** Loads Google's picker library once (only on pages that use it). */
export function loadPicker(doc: Document = document, win: Window & { gapi?: Gapi; google?: Google } = window): Promise<Google> {
	if (loading) return loading;
	loading = new Promise<Google>((resolve, reject) => {
		const ready = () => win.gapi!.load('picker', { callback: () => resolve(win.google), onerror: () => reject(new Error('Google’s file picker didn’t load')) });
		if (win.gapi) return ready();
		const s = doc.createElement('script');
		s.src = 'https://apis.google.com/js/api.js';
		s.async = true;
		s.onload = ready;
		s.onerror = () => {
			loading = null;
			reject(new Error('Google’s file picker didn’t load. Check your connection, or whether something blocks apis.google.com.'));
		};
		doc.head.appendChild(s);
	});
	return loading;
}

/** Opens Google's picker; resolves to what was picked ([] if cancelled). */
export async function pickFiles(o: { kind: PickKind; multiple?: boolean; title: string }): Promise<Picked[]> {
	const [tok, google] = await Promise.all([
		api.post<{ access_token: string; api_key: string; app_id: string }>('/api/teacher/drive/picker'),
		loadPicker()
	]);
	return new Promise((resolve) => {
		const view = new google.picker.DocsView(google.picker.ViewId.DOCS).setIncludeFolders(true).setSelectFolderEnabled(false).setMode(google.picker.DocsViewMode.LIST);
		if (KINDS[o.kind]) view.setMimeTypes(KINDS[o.kind]);
		let b = new google.picker.PickerBuilder()
			.setOAuthToken(tok.access_token)
			.setDeveloperKey(tok.api_key)
			.setAppId(tok.app_id)
			.setOrigin(location.origin)
			.setTitle(o.title)
			.addView(view)
			.setCallback((data: Parameters<typeof picked>[0]) => {
				const files = picked(data);
				if (files) resolve(files);
			});
		if (o.multiple) b = b.enableFeature(google.picker.Feature.MULTISELECT_ENABLED);
		b.build().setVisible(true);
	});
}

/** A shared-by-link file, for a question image or a link in question text. */
export type Shared = { id: string; name: string; mime_type: string; web_link: string; image: boolean; image_url?: string };

export function shareByLink(fileId: string): Promise<Shared> {
	return api.post<Shared>('/api/teacher/drive/share', { file_id: fileId });
}

export const SHARING: Record<'students' | 'link' | 'none', { label: string; hint: string }> = {
	students: { label: 'Students in this class', hint: 'Shared with each enrolled student’s email; students who join or leave are kept in step.' },
	link: { label: 'Anyone with the link', hint: 'Students open it without signing in to Google; anyone they pass the link to can too.' },
	none: { label: 'As it is in Drive', hint: 'The platform doesn’t change who can open it; share it yourself in Drive.' }
};
