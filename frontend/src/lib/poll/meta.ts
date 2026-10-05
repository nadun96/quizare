// Labels, groups and starting values for every poll question type (D-40).
import type { IconName } from '../ui/Icon.svelte';
import type { PollAnswer, PollBody, PollQuestion, PollType } from './types';

export type TypeMeta = { label: string; hint: string; group: string; icon: IconName; body: () => PollBody; text: string };

const opts = (...t: string[]) => t.map((text) => ({ id: '', text }));

export const TYPE_META: Record<PollType, TypeMeta> = {
	SINGLE: { label: 'Single choice', hint: 'Radio buttons or a dropdown', group: 'Choice', icon: 'check-circle', text: 'Which option do you prefer?', body: () => ({ options: opts('Option A', 'Option B', 'Option C'), display: 'radio' }) },
	MULTI: { label: 'Multiple choice', hint: 'Checkboxes; optionally a limit', group: 'Choice', icon: 'check', text: 'Select all that apply', body: () => ({ options: opts('Option A', 'Option B', 'Option C'), max_choices: 0 }) },
	RATING: { label: 'Star rating', hint: '3 to 10 stars', group: 'Scale', icon: 'sparkles', text: 'How would you rate today’s lesson?', body: () => ({ points: 5 }) },
	SLIDER: { label: 'Slider', hint: 'A value on a range, with end labels', group: 'Scale', icon: 'sliders', text: 'How confident do you feel?', body: () => ({ min: 0, max: 10, step: 1, min_label: 'Not at all', max_label: 'Very' }) },
	LIKERT: { label: 'Likert scale', hint: 'Agreement with one or more statements', group: 'Scale', icon: 'chart', text: 'How much do you agree?', body: () => ({ rows: opts('The pace was right'), scale: ['Strongly disagree', 'Disagree', 'Neutral', 'Agree', 'Strongly agree'] }) },
	MATRIX: { label: 'Matrix / grid', hint: 'Rows × columns of radios, checkboxes or text boxes', group: 'Scale', icon: 'menu', text: 'Rate each topic', body: () => ({ rows: opts('Topic 1', 'Topic 2'), columns: opts('Easy', 'OK', 'Hard'), mode: 'single' }) },
	WORD_CLOUD: { label: 'Word cloud', hint: 'A few words each, shown as a live cloud', group: 'Text', icon: 'cloud', text: 'Describe today in one word', body: () => ({ max_entries: 3 }) },
	SHORT_TEXT: { label: 'Short text', hint: 'One line', group: 'Text', icon: 'type', text: 'What’s one thing you learned?', body: () => ({ max_length: 200 }) },
	ESSAY: { label: 'Long text', hint: 'A comment box for longer answers', group: 'Text', icon: 'book', text: 'What would you like to revise next time?', body: () => ({ max_words: 0 }) },
	CODE: { label: 'Code', hint: 'A code editor with tab indentation', group: 'Text', icon: 'type', text: 'Write a function that…', body: () => ({ language: 'Python', max_length: 10000 }) },
	NUMBER: { label: 'Number', hint: 'Optional range, step and unit', group: 'Number & time', icon: 'chart', text: 'Estimate the answer', body: () => ({ unit: '' }) },
	DATE: { label: 'Date', hint: 'A date picker', group: 'Number & time', icon: 'clock', text: 'Which day suits you?', body: () => ({}) },
	TIME: { label: 'Time', hint: 'A time picker', group: 'Number & time', icon: 'clock', text: 'What time suits you?', body: () => ({}) },
	MATCH: { label: 'Matching', hint: 'Pair items on the left with the right', group: 'Quiz-style', icon: 'arrow-right', text: 'Match each item', body: () => ({ left: opts('A', 'B'), right: opts('1', '2') }) },
	BLANK_OPT: { label: 'Fill in the blank (options)', hint: 'Choose a word per blank', group: 'Quiz-style', icon: 'type', text: 'The best part was [[1]]', body: () => ({ options: opts('the activity', 'the discussion', 'the video') }) },
	BLANK_TEXT: { label: 'Fill in the blank (own words)', hint: 'Type a word per blank', group: 'Quiz-style', icon: 'type', text: 'Today I learned about [[1]]', body: () => ({}) },
	DRAG: { label: 'Ranking / sorting', hint: 'Drag to rank, or sort into boxes', group: 'Quiz-style', icon: 'menu', text: 'Rank these from most to least useful', body: () => ({ options: opts('Item 1', 'Item 2', 'Item 3') }) },
	FILE: { label: 'File upload', hint: 'Images, PDFs or documents, up to 5 MB', group: 'Media', icon: 'plus', text: 'Upload a photo of your work', body: () => ({ accept: ['image', 'pdf'], max_mb: 5 }) },
	AUDIO: { label: 'Audio recording', hint: 'Recorded in the browser, up to 60 s', group: 'Media', icon: 'play', text: 'Record your answer', body: () => ({ max_seconds: 30 }) },
	VIDEO: { label: 'Video recording', hint: 'Recorded in the browser, up to 30 s', group: 'Media', icon: 'eye', text: 'Show us your experiment', body: () => ({ max_seconds: 20 }) }
};

export const GROUPS = ['Choice', 'Scale', 'Text', 'Number & time', 'Quiz-style', 'Media'];
export const TYPES_BY_GROUP = GROUPS.map((g) => ({ group: g, types: (Object.keys(TYPE_META) as PollType[]).filter((t) => TYPE_META[t].group === g) }));

/** Gives unsaved choices ids the way the server does (o1, l1, r1, z1, row1, col1), for previews. */
export function withIds(b: PollBody): PollBody {
	const fill = (cs: { id: string; text: string }[] | undefined, p: string) => {
		if (!cs) return cs;
		const used = new Set(cs.map((c) => c.id).filter(Boolean));
		let n = 1;
		return cs.map((c) => {
			if (c.id) return c;
			while (used.has(p + n)) n++;
			used.add(p + n);
			return { ...c, id: p + n };
		});
	};
	return { ...b, options: fill(b.options, 'o'), left: fill(b.left, 'l'), right: fill(b.right, 'r'), zones: fill(b.zones, 'z'), rows: fill(b.rows, 'row'), columns: fill(b.columns, 'col') };
}

/** True when the answer holds something (mirrors CheckAnswer's "empty"). */
export function hasAnswer(q: PollQuestion, a: PollAnswer | undefined): boolean {
	if (!a) return false;
	switch (q.type) {
		case 'SINGLE':
		case 'MULTI':
			return !!a.selected?.length;
		case 'MATCH':
			return !!a.pairs && Object.keys(a.pairs).length > 0;
		case 'BLANK_OPT':
		case 'BLANK_TEXT':
			return !!a.blanks && Object.values(a.blanks).some((v) => v.trim());
		case 'DRAG':
			return q.body.zones?.length ? !!a.pairs && Object.keys(a.pairs).length > 0 : !!a.order?.length;
		case 'ESSAY':
		case 'SHORT_TEXT':
		case 'CODE':
			return !!a.text?.trim();
		case 'WORD_CLOUD':
			return !!a.words?.length;
		case 'NUMBER':
		case 'SLIDER':
		case 'RATING':
			return a.number !== undefined && a.number !== null;
		case 'DATE':
			return !!a.date;
		case 'TIME':
			return !!a.time;
		case 'LIKERT':
			return !!a.rows && Object.keys(a.rows).length > 0;
		case 'MATRIX':
			return q.body.mode === 'multi' ? !!a.multi && Object.keys(a.multi).length > 0 : q.body.mode === 'text' ? !!a.cells && Object.keys(a.cells).length > 0 : !!a.rows && Object.keys(a.rows).length > 0;
		case 'FILE':
		case 'AUDIO':
		case 'VIDEO':
			return !!a.file;
	}
}

export const IDENTITY_LABEL = { anonymous: 'Anonymous', identified: 'Identified (login required)', optional: 'Participant chooses' } as const;
export const RESULTS_LABEL = { live: 'Live, all the time', after_answer: 'After they answer', presenter: 'When the presenter reveals', never: 'Never' } as const;

export const fmtSize = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${(n / 1024).toFixed(0)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);
