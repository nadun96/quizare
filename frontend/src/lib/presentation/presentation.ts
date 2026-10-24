// Presentations (D-76): what the teacher's editor and the student's viewer
// share, and turning a PDF into slides in the teacher's browser. pdf.js is
// loaded only when a teacher uploads, so it costs students nothing.
import { api, ApiError } from '../api';

export type Presentation = {
	id: string;
	topic_id: string;
	title: string;
	description: string;
	status: 'draft' | 'published';
	allow_jump: boolean;
	allow_download: boolean;
	version: number;
	slide_count: number;
	file_name?: string;
	file_bytes: number;
	opens_at?: string;
	due_at?: string;
	published_at?: string;
	created_at: string;
	updated_at: string;
};

export type Slide = { n: number; width: number; height: number; text: string };

export type View = {
	presentation: { id: string; title: string; description: string; allow_jump: boolean; allow_download: boolean; version: number; due_at?: string };
	slides: Slide[];
	current: number;
	reached: number;
	finished: boolean;
};

export type MyPresentation = { id: string; title: string; classroom: string; slide_count: number; opens_at?: string; due_at?: string; started: boolean; reached: number; finished: boolean };

export const MAX_PDF_BYTES = 50 * 1024 * 1024;
export const MAX_SLIDES = 300;
const SLIDE_PX = 1600; // the longer side of a slide image

/** Alt text for a slide: its number and the start of its own text (PR-NFR-05). */
export function slideAlt(s: Pick<Slide, 'n' | 'text'>, total: number): string {
	const text = s.text.trim();
	const short = text.length > 240 ? text.slice(0, 240).replace(/\s+\S*$/, '') + '…' : text;
	return `Slide ${s.n} of ${total}${short ? ': ' + short : ''}`;
}

/** Whether a slide can be opened now: any, or in order up to the next unseen one. */
export function canOpen(v: Pick<View, 'presentation' | 'reached' | 'slides'>, n: number): boolean {
	return n >= 1 && n <= v.slides.length && (v.presentation.allow_jump || n <= v.reached + 1);
}

/** "Slide 3 of 12" for the student's list. */
export function progressText(p: MyPresentation): string {
	if (p.finished) return 'Seen to the end';
	if (!p.started) return p.slide_count === 1 ? '1 slide' : `${p.slide_count} slides`;
	return `Slide ${Math.min(p.reached, p.slide_count)} of ${p.slide_count}`;
}

/** The scale that makes a page's longer side SLIDE_PX, within reason. */
export function slideScale(width: number, height: number): number {
	return Math.min(SLIDE_PX / Math.max(width, height, 1), 4);
}

type PdfDoc = { numPages: number; getPage(n: number): Promise<any>; destroy(): Promise<void> };

async function openPdf(data: ArrayBuffer): Promise<PdfDoc> {
	const pdfjs = await import('pdfjs-dist');
	pdfjs.GlobalWorkerOptions.workerSrc = (await import('pdfjs-dist/build/pdf.worker.min.mjs?url')).default;
	// No eval and no scripts from the file: it's rendered, nothing more.
	return pdfjs.getDocument({ data, isEvalSupported: false, enableXfa: false }).promise as Promise<PdfDoc>;
}

async function renderSlide(doc: PdfDoc, n: number): Promise<{ image: Blob; text: string }> {
	const page = await doc.getPage(n);
	const base = page.getViewport({ scale: 1 });
	const viewport = page.getViewport({ scale: slideScale(base.width, base.height) });
	const canvas = document.createElement('canvas');
	canvas.width = Math.round(viewport.width);
	canvas.height = Math.round(viewport.height);
	const ctx = canvas.getContext('2d')!;
	ctx.fillStyle = '#fff'; // JPEG has no transparency
	ctx.fillRect(0, 0, canvas.width, canvas.height);
	await page.render({ canvasContext: ctx, viewport }).promise;
	const image = await new Promise<Blob>((ok, fail) => canvas.toBlob((b) => (b ? ok(b) : fail(new Error('Could not draw a slide'))), 'image/jpeg', 0.85));
	const content = await page.getTextContent();
	const text = content.items.map((i: { str?: string }) => i.str ?? '').join(' ');
	page.cleanup();
	return { image, text };
}

/** How many pages a PDF has, to check it before uploading. */
export async function countPages(file: File): Promise<number> {
	const doc = await openPdf(await file.arrayBuffer());
	const n = doc.numPages;
	await doc.destroy();
	return n;
}

async function putRaw(path: string, body: Blob, type: string) {
	const res = await fetch(path, { method: 'PUT', credentials: 'same-origin', headers: { 'X-Requested-With': 'fetch', 'Content-Type': type }, body });
	if (!res.ok) {
		const data = await res.json().catch(() => ({}));
		throw new ApiError(res.status, data.code ?? 'error', data.fields?.file ?? data.message ?? 'The upload failed', data.fields ?? {});
	}
}

/**
 * Uploads a PDF as a presentation's file: the PDF, then each page rendered
 * here to a JPEG with its text. Students keep the old file until the last
 * slide is in (PR-FR-05).
 */
export async function uploadPdf(id: string, file: File, progress: (done: number, total: number) => void): Promise<Presentation> {
	if (file.size > MAX_PDF_BYTES) throw new Error('A PDF of at most 50 MB, please');
	const doc = await openPdf(await file.arrayBuffer());
	try {
		const total = doc.numPages;
		if (total > MAX_SLIDES) throw new Error(`This PDF has ${total} pages; a presentation has at most ${MAX_SLIDES}`);
		const { version } = await api.post<{ version: number }>(`/api/teacher/presentations/${id}/uploads`, { name: file.name, slides: total });
		const base = `/api/teacher/presentations/${id}/uploads/${version}`;
		progress(0, total);
		await putRaw(base + '/pdf', file, 'application/pdf');
		for (let n = 1; n <= total; n++) {
			const { image, text } = await renderSlide(doc, n);
			const form = new FormData();
			form.append('image', image, `slide-${n}.jpg`);
			form.append('text', text);
			await api.put(`${base}/slides/${n}`, form);
			progress(n, total);
		}
		return await api.post<Presentation>(base + '/finish');
	} finally {
		await doc.destroy();
	}
}
