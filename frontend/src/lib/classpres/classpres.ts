// Group presentations in a classroom (D-166): the teacher sets tasks; each
// classroom group uploads one presentation a task; once the teacher allows
// it, a member presses Present and classmates follow their slides.
import { api } from '../api';
import { countPages, deckToPdfVia, fileKind, renderPdfSlides } from '../presentation/presentation';

export type PresTask = {
	id: string;
	classroom_id: string;
	topic_id: string | null;
	topic: string;
	title: string;
	instructions: string;
	upload_by: 'any' | 'leader';
	out_of: number;
	rating: boolean;
	due_at: string | null;
	created_at: string;
	submitted: number;
	marked: number;
	groups: number;
};

export type GroupPres = {
	id: string;
	task_id: string;
	group_id: string;
	group: string;
	title: string;
	file_name: string;
	slides: number;
	ready: boolean;
	files_removed: boolean;
	allowed: boolean;
	uploaded_by: string;
	presented_at: string | null;
	mark: number | null;
	comment: string;
	rating: number | null;
	ratings: number;
	updated_at: string;
};

export type ClassStage = {
	submission_id: string;
	task_id: string;
	task: string;
	title: string;
	group: string;
	slide: number;
	slides: number;
	presenter_name?: string;
	presenting: boolean;
	mine: boolean;
	rating: boolean;
	my_stars: number;
	started_at: string;
};

export type TaskGroup = { id: string; name: string; members: number; presentation: GroupPres | null };
export type TaskView = { task: PresTask; groups: TaskGroup[]; stage: ClassStage | null };

export type MyTaskGroup = { id: string; name: string; can_upload: boolean; presentation: GroupPres | null };
export type MyTask = PresTask & { my_groups: MyTaskGroup[] };
export type MyClassPresentations = { tasks: MyTask[]; stage: ClassStage | null; can_convert: boolean };

export type TaskInput = { title: string; instructions: string; topic_id: string; upload_by: 'any' | 'leader'; out_of: number; rating: boolean; due_at: string | null };

export const MAX_SLIDES = 100;

/** Who's presenting, in words. */
export function presenterText(st: Pick<ClassStage, 'presenting' | 'presenter_name'>): string {
	if (st.presenting) return 'You are presenting';
	if (st.presenter_name) return `${st.presenter_name} is presenting`;
	return 'The teacher is presenting';
}

/** The slide `by` away, or null past either end. */
export function step(st: Pick<ClassStage, 'slide' | 'slides'>, by: number): number | null {
	const n = st.slide + by;
	return n >= 1 && n <= st.slides ? n : null;
}

/** "3.5 ★ (4)": classmates' rating. */
export const ratingText = (x: Pick<GroupPres, 'rating' | 'ratings'>) => (x.rating == null ? '' : `${Math.round(x.rating * 10) / 10} ★ (${x.ratings})`);

/** Where a group's presentation stands, for the teacher. */
export function statusOf(p: GroupPres | null): { label: string; tone: string } {
	if (!p) return { label: 'Nothing yet', tone: '' };
	if (p.files_removed) return { label: 'Slides deleted', tone: '' };
	if (!p.ready) return { label: 'Uploading', tone: 'badge-warning' };
	if (p.presented_at) return { label: 'Presented', tone: 'badge-success' };
	if (p.allowed) return { label: 'Allowed to present', tone: 'badge-info' };
	return { label: 'Uploaded', tone: 'badge-neutral' };
}

/** A task's form, from the task (or empty). */
export function taskForm(t?: PresTask | null): TaskInput {
	return {
		title: t?.title ?? '',
		instructions: t?.instructions ?? '',
		topic_id: t?.topic_id ?? '',
		upload_by: t?.upload_by ?? 'any',
		out_of: t?.out_of ?? 0,
		rating: t?.rating ?? false,
		due_at: t?.due_at ?? null
	};
}

/** A <input type="datetime-local"> value for a time, and back. */
export const toLocalInput = (iso: string | null) => {
	if (!iso) return '';
	const d = new Date(iso);
	const p = (n: number) => String(n).padStart(2, '0');
	return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
};
export const fromLocalInput = (v: string) => (v ? new Date(v).toISOString() : null);

export const teacherSlide = (id: string, n: number) => `/api/teacher/group-presentations/${id}/slides/${n}`;
export const mySlide = (id: string, n: number) => `/api/my/group-presentations/${id}/slides/${n}`;

/**
 * Uploads a file as a group's presentation for a task: PowerPoint goes
 * through the classroom owner's Drive first; then each page is drawn here
 * and sent as a slide.
 */
export async function uploadGroupPres(
	classroomId: string,
	taskId: string,
	groupId: string,
	file: File,
	title: string,
	progress: (done: number, total: number, stage: string) => void
): Promise<GroupPres> {
	let pdf = file;
	const kind = fileKind(file.name);
	if (!kind) throw new Error('Choose a PDF or a PowerPoint file');
	if (kind === 'deck') {
		progress(0, 0, 'Turning PowerPoint into slides…');
		pdf = await deckToPdfVia(`/api/my/classrooms/${classroomId}/presentations/convert`, file);
	}
	const total = await countPages(pdf);
	if (total > MAX_SLIDES) throw new Error(`This has ${total} pages; at most ${MAX_SLIDES}, please`);
	const x = await api.post<GroupPres>(`/api/my/presentation-tasks/${taskId}/presentations`, { group_id: groupId, title, file_name: file.name, slides: total });
	progress(0, total, 'Uploading slides…');
	await renderPdfSlides(pdf, MAX_SLIDES, async (n, all, slide) => {
		const form = new FormData();
		form.append('image', slide.image, `slide-${n}.jpg`);
		form.append('text', slide.text);
		await api.put<void>(`/api/my/group-presentations/${x.id}/slides/${n}`, form);
		progress(n, all, 'Uploading slides…');
	});
	return api.post<GroupPres>(`/api/my/group-presentations/${x.id}/finish`);
}
