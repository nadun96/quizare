// Pictures in rich text (D-113): uploaded as they are (the server redraws
// them), shown only from this site's own picture store.
import { ApiError } from '../api';

export const IMAGE_SRC = /^\/api\/images\/[0-9a-f-]{36}$/;
export const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'];
export const MAX_IMAGE = 8 << 20;

export type Uploaded = { id: string; url: string; width: number; height: number };

/** Sends a picture to the server and returns where it's served. */
export async function uploadImage(file: File, f: typeof fetch = fetch): Promise<Uploaded> {
	if (!IMAGE_TYPES.includes(file.type)) throw new ApiError(422, 'validation_failed', 'Pictures can be PNG, JPEG, GIF or WebP.');
	if (file.size > MAX_IMAGE) throw new ApiError(413, 'too_large', 'Pictures can be at most 8 MB.');
	const res = await f('/api/teacher/images', { method: 'POST', headers: { 'X-Requested-With': 'fetch', 'Content-Type': file.type }, body: file, credentials: 'same-origin' });
	const data = await res.json().catch(() => ({}));
	if (!res.ok) throw new ApiError(res.status, data.code ?? 'error', Object.values(data.fields ?? {})[0] as string ?? data.message ?? 'Upload failed');
	return data as Uploaded;
}

/** The pictures among files a teacher dropped or pasted. */
export const imageFiles = (files: FileList | File[] | null | undefined) => [...(files ?? [])].filter((f) => IMAGE_TYPES.includes(f.type));
