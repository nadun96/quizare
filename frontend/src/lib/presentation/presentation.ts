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

export type MyPresentation = {
	id: string;
	title: string;
	classroom: string;
	slide_count: number;
	opens_at?: string;
	due_at?: string;
	started: boolean;
	reached: number;
	finished: boolean;
	live: boolean; // being presented now
	published: boolean;
};

// Presenting live (D-77).
export type Live = { id: string; presentation_id: string; code: string; slide: number; max_shown: number; allow_browse: boolean; started_at: string };
export type LiveView = { presentation: { id: string; title: string; version: number; published: boolean }; slides: Slide[]; live: Live };
export type Person = { user_id: string; name: string; slide: number; following: boolean; visible: boolean };
export type Counts = { joined: number; following: number; looking_back: number; away: number };

/** Where a student may go on their own while live: back to what's been shown, never ahead. */
export function liveCanGo(l: Pick<Live, 'max_shown' | 'allow_browse'>, n: number): boolean {
	return l.allow_browse && n >= 1 && n <= l.max_shown;
}

/** "12:05" since the start, or "1:02:05" past an hour. */
export function elapsed(fromIso: string, now: number): string {
	const s = Math.max(0, Math.floor((now - new Date(fromIso).getTime()) / 1000));
	const p = (n: number) => String(n).padStart(2, '0');
	const h = Math.floor(s / 3600);
	return h ? `${h}:${p(Math.floor((s % 3600) / 60))}:${p(s % 60)}` : `${Math.floor(s / 60)}:${p(s % 60)}`;
}

/** A person in the audience, in a word for the presenter. */
export function personState(p: Person, slide: number): string {
	if (!p.visible) return 'Away from the page';
	if (p.following) return 'Following';
	return p.slide === slide ? 'On your slide' : `Looking at slide ${p.slide}`;
}

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

// ---- PowerPoint through Google Drive, and time against marks (D-78) ----

/** Files the browser renders itself, and ones Google Drive converts first. */
export const PDF_TYPES = ['.pdf'];
export const DECK_TYPES = ['.pptx', '.ppt', '.odp', '.key'];
export const MAX_DECK_BYTES = 100 * 1024 * 1024;
export const ACCEPT = [...PDF_TYPES, ...DECK_TYPES, 'application/pdf'].join(',');

export function fileKind(name: string): 'pdf' | 'deck' | null {
	const ext = name.toLowerCase().match(/\.[a-z0-9]+$/)?.[0] ?? '';
	if (PDF_TYPES.includes(ext)) return 'pdf';
	if (DECK_TYPES.includes(ext)) return 'deck';
	return null;
}

async function pdfFrom(res: Response, fallbackName: string): Promise<File> {
	if (!res.ok) {
		const data = await res.json().catch(() => ({}));
		throw new ApiError(res.status, data.code ?? 'error', data.fields?.file ?? data.message ?? 'Google Drive couldn’t convert it', data.fields ?? {});
	}
	const name = decodeURIComponent(res.headers.get('X-File-Name') ?? '') || fallbackName;
	return new File([await res.blob()], name, { type: 'application/pdf' });
}

/** A PowerPoint, OpenDocument or Keynote file, as a PDF converted in the teacher's Google Drive. */
export async function deckToPdf(file: File): Promise<File> {
	if (file.size > MAX_DECK_BYTES) throw new Error('A presentation of at most 100 MB, please');
	const res = await fetch('/api/teacher/drive/slides-pdf', {
		method: 'POST', credentials: 'same-origin', body: file,
		headers: { 'X-Requested-With': 'fetch', 'Content-Type': 'application/octet-stream', 'X-File-Name': encodeURIComponent(file.name) }
	});
	return pdfFrom(res, file.name.replace(/\.[a-z0-9]+$/i, '') + '.pdf');
}

/** A presentation picked in Drive (Google Slides, a PDF or PowerPoint), as a PDF. */
export async function drivePdf(fileId: string, name: string): Promise<File> {
	const res = await fetch(`/api/teacher/drive/files/${encodeURIComponent(fileId)}/slides-pdf`, {
		method: 'POST', credentials: 'same-origin', headers: { 'X-Requested-With': 'fetch' }
	});
	return pdfFrom(res, name.replace(/\.[a-z0-9]+$/i, '') + '.pdf');
}

export type Linkable = { kind: 'quiz' | 'tutorial'; id: string; title?: string };
export type Links = { linked: Linkable[]; options: Linkable[] };
export type Point = { user_id: string; name: string; seconds: number; seen: number; score_pct: number };
export type Trend = { per_ten_minutes: number; intercept: number; r: number };
export type SlideSplit = { n: number; text: string; median_seconds: number; longer_n: number; longer_pct?: number; shorter_n: number; shorter_pct?: number; skipped: number };
export type Analysis = { linked: Linkable[]; points: Point[]; no_score: number; not_viewed: number; trend?: Trend; slides: SlideSplit[] };

/** A round top for the time axis, in seconds, just above the largest value; quarters stay whole seconds. */
export function niceSeconds(v: number): number {
	for (const c of [20, 40, 60, 120, 240, 300, 600, 1200, 1800, 3600]) if (c >= v) return c;
	return Math.ceil(v / 3600) * 3600;
}

/**
 * The trend in a sentence a teacher reads at a glance. It speaks only of the
 * range the students' times cover, never beyond it.
 */
export function trendWords(t: Trend, fromSecs: number, toSecs: number, fmt: (secs: number) => string): string {
	const r = Math.abs(t.r);
	const strength = r >= 0.7 ? 'A strong' : r >= 0.4 ? 'A clear' : r >= 0.2 ? 'A loose' : '';
	if (!strength || t.per_ten_minutes === 0 || toSecs <= fromSecs) return 'Time on the slides and score hardly go together here.';
	const at = (secs: number) => Math.round(Math.min(100, Math.max(0, t.intercept + (t.per_ten_minutes / 600) * secs)));
	const dir = t.per_ten_minutes > 0 ? 'higher' : 'lower';
	return `${strength} link: students who spent longer scored ${dir}. Along the trend, from ${at(fromSecs)}% at ${fmt(fromSecs)} to ${at(toSecs)}% at ${fmt(toSecs)} (r = ${t.r.toFixed(2)}).`;
}
