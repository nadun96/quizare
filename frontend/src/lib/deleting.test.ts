import { describe, expect, it } from 'vitest';
import { contentsList, resultsList, type DeleteContents } from './deleting';

const none: DeleteContents = {
	name: 'Grade 7', modules: 0, topics: 0, quizzes: 0, tutorials: 0, presentations: 0,
	sessions: 0, attempts: 0, tutorial_runs: 0, enrolments: 0, live_sessions: 0, needs_confirm: false
};

describe('deleting with contents', () => {
	it('lists what goes, in words, leaving out what there is none of', () => {
		expect(contentsList(none)).toBe('');
		expect(contentsList({ ...none, quizzes: 1 })).toBe('1 quiz');
		expect(contentsList({ ...none, topics: 2, quizzes: 3 })).toBe('2 topics and 3 quizzes');
		expect(contentsList({ ...none, modules: 1, topics: 2, quizzes: 1, presentations: 1, enrolments: 30 })).toBe(
			'1 module, 2 topics, 1 quiz, 1 presentation and 30 enrolments'
		);
	});

	it('says which results go', () => {
		expect(resultsList(none)).toBe('');
		expect(resultsList({ ...none, sessions: 1, attempts: 1 })).toBe("1 quiz session with 1 student's attempt");
		expect(resultsList({ ...none, sessions: 2, attempts: 40, tutorial_runs: 3 })).toBe(
			"2 quiz sessions with 40 students' attempts and 3 students' tutorial work"
		);
	});
});
