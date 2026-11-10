// Presentations in polls (D-165): the teacher asks for them; each student,
// poll group or classroom group submits one; the teacher lets one present
// live, its presenter turns the slides, and everyone in the poll follows.
import { ApiError } from '../api';
import { countPages, deckToPdfVia, fileKind, renderPdfSlides } from '../presentation/presentation';
import { getToken } from './client';

export type PresMode = 'off' | 'student' | 'poll_group' | 'class_group';

export type PresSettings = { mode: PresMode; upload_by: 'any' | 'leader'; out_of: number; rating: boolean; instructions: string };

export type Owner = { kind: Exclude<PresMode, 'off'>; id: string; name: string };

export type Submission = {
	id: string;
	owner: Owner;
	title: string;
	file_name: string;
	slides: number;
	ready: boolean;
	uploaded_by: string;
	presented_at: string | null;
	mark: number | null;
	comment: string;
	rating: number | null;
	ratings: number;
	updated_at: string;
};

export type MyPresentations = PresSettings & {
	owners: Owner[];
	can_upload: Record<string, boolean>;
	submissions: Record<string, Submission>;
	can_convert: boolean;
};

export type Stage = {
	submission_id: string;
	title: string;
	owner: string;
	slide: number;
	slides: number;
	presenter_key?: string;
	presenter_name?: string;
	rating: boolean;
};

export const MODES: { v: PresMode; label: string }[] = [
	{ v: 'off', label: 'Not asked for' },
	{ v: 'student', label: 'One a student' },
	{ v: 'poll_group', label: "One a poll group" },
	{ v: 'class_group', label: 'One a classroom group' }
];

export const MAX_SLIDES = 100;

/** Who's presenting, in words. */
export function presenterText(st: Stage, meKey?: string): string {
	if (st.presenter_key && st.presenter_key === meKey) return 'You are presenting';
	if (st.presenter_name) return `${st.presenter_name} is presenting`;
	return 'The teacher is presenting';
}

/** The next slide within the presentation, or null. */
export function step(st: Pick<Stage, 'slide' | 'slides'>, by: number): number | null {
	const n = st.slide + by;
	return n >= 1 && n <= st.slides ? n : null;
}

/** "3.5 ★ (4)": the audience's rating. */
export const ratingText = (x: Pick<Submission, 'rating' | 'ratings'>) => (x.rating == null ? '' : `${Math.round(x.rating * 10) / 10} ★ (${x.ratings})`);

const base = (code: string) => '/api/polls/' + encodeURIComponent(code);

async function call<T>(code: string, method: string, path: string, body?: BodyInit, type?: string): Promise<T> {
	const h: Record<string, string> = { 'X-Requested-With': 'fetch' };
	const tok = getToken(code);
	if (tok) h['X-Poll-Token'] = tok;
	if (type) h['Content-Type'] = type;
	const res = await fetch(base(code) + path, { method, headers: h, body, credentials: 'same-origin' });
	const text = await res.text();
	let data: any;
	try {
		data = text ? JSON.parse(text) : undefined;
	} catch {
		data = undefined;
	}
	if (!res.ok) throw new ApiError(res.status, data?.code ?? 'error', data?.fields ? (Object.values(data.fields)[0] as string) : data?.message ?? res.statusText, data?.fields ?? {});
	return data as T;
}

const json = 'application/json';

export const presClient = {
	mine: (code: string) => call<MyPresentations>(code, 'GET', '/presentations'),
	slideUrl: (code: string, id: string, n: number) => `${base(code)}/presentations/${id}/slides/${n}`,
	/** A slide of one's own, fetched with the poll token (an <img> can't send it). */
	async ownSlide(code: string, id: string, n: number): Promise<string> {
		const h: Record<string, string> = {};
		const tok = getToken(code);
		if (tok) h['X-Poll-Token'] = tok;
		const res = await fetch(`${base(code)}/presentations/${id}/slides/${n}`, { headers: h, credentials: 'same-origin' });
		if (!res.ok) throw new ApiError(res.status, 'error', 'The slide didn’t load');
		return URL.createObjectURL(await res.blob());
	},
	turn: (code: string, n: number) => call<void>(code, 'PUT', '/stage/slide', JSON.stringify({ n }), json),
	rate: (code: string, id: string, stars: number) => call<void>(code, 'PUT', `/presentations/${id}/rating`, JSON.stringify({ stars }), json),

	/**
	 * Uploads a file as the participant's (or their group's) presentation: a
	 * PowerPoint file goes through the teacher's Drive first; then each page
	 * is drawn here and sent as a slide.
	 */
	async upload(code: string, file: File, title: string, ownerId: string, progress: (done: number, total: number, stage: string) => void): Promise<Submission> {
		let pdf = file;
		const kind = fileKind(file.name);
		if (!kind) throw new Error('Choose a PDF or a PowerPoint file');
		if (kind === 'deck') {
			progress(0, 0, 'Turning PowerPoint into slides…');
			pdf = await deckToPdfVia(`${base(code)}/presentations/convert`, file, getToken(code));
		}
		const total = await countPages(pdf);
		if (total > MAX_SLIDES) throw new Error(`This has ${total} pages; at most ${MAX_SLIDES}, please`);
		const x = await call<Submission>(code, 'POST', '/presentations', JSON.stringify({ title, file_name: file.name, slides: total, owner_id: ownerId }), json);
		progress(0, total, 'Uploading slides…');
		await renderPdfSlides(pdf, MAX_SLIDES, async (n, all, slide) => {
			const form = new FormData();
			form.append('image', slide.image, `slide-${n}.jpg`);
			form.append('text', slide.text);
			await call<void>(code, 'PUT', `/presentations/${x.id}/slides/${n}`, form);
			progress(n, all, 'Uploading slides…');
		});
		return call<Submission>(code, 'POST', `/presentations/${x.id}/finish`);
	}
};
