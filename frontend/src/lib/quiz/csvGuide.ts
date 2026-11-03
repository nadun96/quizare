// What each column of the questions CSV means (D-135), as the importer
// reads it (backend/internal/quiz/csv.go). The import screens show it, and
// it downloads as a guide next to the template.

export type Column = { name: string; required: boolean; means: string; format: string; example: string };
export type TypeHow = { type: string; label: string; options: string; answer: string; example: { options: string; answer: string; text?: string } };

export const COLUMNS: Column[] = [
	{ name: 'question_code', required: true, means: 'A short name for the question, unique in the quiz or pool. Results and image names refer to it.', format: '1–32 letters, digits, _ . or -. If the pool already has the code, the import renames it (Q1 → Q1-2).', example: 'HTML-01' },
	{ name: 'type', required: true, means: 'What kind of question it is. See “Question types” below for how to fill options and correct_answer for each.', format: 'SINGLE, MULTI, MATCH, BLANK_OPT, BLANK_TEXT, DRAG or ESSAY (any case).', example: 'SINGLE' },
	{ name: 'question_text', required: true, means: 'What the student reads. Fill-in-the-blank questions mark each blank with [[1]], [[2]]…', format: 'Up to 10,000 characters. Put it in "double quotes" if it contains a comma.', example: 'Which tag makes a paragraph?' },
	{ name: 'options', required: false, means: 'The choices, items or options the student picks from. Not used by BLANK_TEXT or ESSAY.', format: 'Separated by | (a vertical bar). MATCH and sorting DRAG: left items, then ||, then right items.', example: '<p>|<para>|<pg>|<text>' },
	{ name: 'correct_answer', required: false, means: 'The right answer, written using the exact text of the options. For ESSAY, a model answer (optional).', format: 'Depends on the type: see “Question types”. Several answers are separated by |.', example: '<p>' },
	{ name: 'marks', required: true, means: 'How many marks the question is worth.', format: 'A number above 0, up to 1000. Decimals allowed (0.5).', example: '1' },
	{ name: 'time_limit_sec', required: false, means: 'Seconds the student has for this question. Leave blank or 0 for no limit of its own.', format: 'A whole number of seconds.', example: '30' },
	{ name: 'evaluation', required: false, means: 'How it is marked. Blank: by the answer key, and essays by AI (or the teacher when no AI is set up).', format: 'KEY (answer key), LLM (AI; ESSAY and BLANK_TEXT only) or MANUAL (the teacher marks it).', example: 'KEY' },
	{ name: 'rubric', required: false, means: 'What earns marks, for AI or manual marking of essays and written answers.', format: 'Free text; separate points with ; if you like.', example: 'Mentions light; chlorophyll; glucose' },
	{ name: 'feedback_correct', required: false, means: 'Shown to the student after a right answer, when feedback is on.', format: 'Free text.', example: 'Well done: <p> is a paragraph.' },
	{ name: 'feedback_incorrect', required: false, means: 'Shown after a wrong answer, when feedback is on. A good place to explain the right one.', format: 'Free text.', example: '<p> makes a paragraph; <para> isn’t HTML.' },
	{ name: 'negative_marks', required: false, means: 'Marks taken away for a wrong answer (not for a blank one).', format: 'A number from 0 up to the question’s marks. Blank: 0.', example: '0.25' },
	{ name: 'partial_credit', required: false, means: 'Whether a partly right answer earns part of the marks (some of the right options, pairs or blanks).', format: 'yes or no. Blank: no for SINGLE and MULTI, yes for the other types.', example: 'yes' },
	{ name: 'word_limit', required: false, means: 'The most words an essay answer may have.', format: 'A whole number. Blank: no limit.', example: '100' },
	{ name: 'case_sensitive', required: false, means: 'Whether capitals matter in BLANK_TEXT answers (Paris vs paris).', format: 'yes or no. Blank: no.', example: 'no' },
	{ name: 'tolerance', required: false, means: 'How many spelling slips a BLANK_TEXT answer may have and still count (letters added, dropped or changed).', format: 'A whole number. Blank: 0 (exact).', example: '1' },
	{ name: 'category', required: false, means: 'Groups questions in a pool (Easy, Plants, Week 3). Quizzes draw so many questions from each category. Left out, the category comes from the file name or the import screen.', format: 'Up to 60 characters.', example: 'HTML' }
];

export const TYPES: TypeHow[] = [
	{ type: 'SINGLE', label: 'One right answer', options: 'The choices, separated by |.', answer: 'The right choice, written exactly as in options.', example: { options: 'Paris|Rome|Madrid', answer: 'Paris' } },
	{ type: 'MULTI', label: 'Several right answers', options: 'The choices, separated by |.', answer: 'Every right choice, separated by |.', example: { options: '2|3|4|9', answer: '2|3' } },
	{ type: 'MATCH', label: 'Match pairs', options: 'Left items, then ||, then right items.', answer: 'Each pair as left=right, separated by |.', example: { options: 'France|Japan||Paris|Tokyo', answer: 'France=Paris|Japan=Tokyo' } },
	{ type: 'BLANK_OPT', label: 'Fill blanks from a list', options: 'The choices for the blanks, separated by |.', answer: 'Each blank as number=choice, separated by |.', example: { text: 'The [[1]] is the largest planet', options: 'Jupiter|Mars|Venus', answer: '1=Jupiter' } },
	{ type: 'BLANK_TEXT', label: 'Fill blanks in own words', options: 'Leave empty.', answer: 'Each blank as number=answer, separated by |. Repeat a blank to accept other answers (1=100|1=hundred).', example: { text: 'Water boils at [[1]] °C', options: '', answer: '1=100|1=hundred' } },
	{ type: 'DRAG', label: 'Put in order, or sort into groups', options: 'Order: the items, separated by |. Sort: items, then ||, then the groups.', answer: 'Order: the items in the right order. Sort: each item as item=group.', example: { options: 'Frog|Eagle||Amphibian|Bird', answer: 'Frog=Amphibian|Eagle=Bird' } },
	{ type: 'ESSAY', label: 'Written answer', options: 'Leave empty.', answer: 'Optional: a model answer for whoever marks it. Use rubric and word_limit too.', example: { text: 'Explain photosynthesis in about 100 words.', options: '', answer: 'Plants use light energy to turn water and carbon dioxide into glucose and oxygen.' } }
];

/** The guide as a spreadsheet: every column, then every question type. */
export function guideCSV(): string {
	const q = (v: string) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
	const rows = [
		['Column', 'Required', 'What it means', 'How to write it', 'Example'],
		...COLUMNS.map((c) => [c.name, c.required ? 'yes' : 'no', c.means, c.format, c.example]),
		[],
		['Type', 'What it is', 'options', 'correct_answer', 'Example options', 'Example correct_answer'],
		...TYPES.map((t) => [t.type, t.label, t.options, t.answer, t.example.options, t.example.answer])
	];
	return rows.map((r) => r.map(q).join(',')).join('\r\n') + '\r\n';
}
