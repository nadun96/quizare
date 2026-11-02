import { describe, expect, it } from 'vitest';
import { byCategory, categoryLine, fromFileName, tidy } from './pools';
import type { Question } from '../types';

const rooms = [
	{ id: 'g10', name: 'Grade 10 Science' },
	{ id: 'g10a', name: 'Grade 10 Science A' },
	{ id: 'mgt', name: 'MGT/IS/2025' }
];

describe('question pools (D-123)', () => {
	it('reads the classroom and the category from a file name', () => {
		expect(fromFileName('Grade 10 Science__Algebra.csv', rooms)).toEqual({ classroomId: 'g10', category: 'Algebra' });
		expect(fromFileName('grade_10_science - Plant cells.csv', rooms)).toEqual({ classroomId: 'g10', category: 'Plant cells' });
		expect(fromFileName('Grade 10 Science A__Waves.CSV', rooms)).toEqual({ classroomId: 'g10a', category: 'Waves' });
		expect(fromFileName('Grade 10 Science_Heat_and_light.csv', rooms)).toEqual({ classroomId: 'g10', category: 'Heat and light' });
	});

	it('takes a name with only a classroom, or only a category', () => {
		expect(fromFileName('Grade 10 Science.csv', rooms)).toEqual({ classroomId: 'g10', category: '' });
		expect(fromFileName('Trigonometry.csv', rooms)).toEqual({ classroomId: null, category: 'Trigonometry' });
		expect(fromFileName('Unknown room__Optics.csv', rooms)).toEqual({ classroomId: null, category: 'Unknown room__Optics'.replace(/_/g, ' ').replace(/\s+/g, ' ') });
	});

	it('tidies categories as the server does', () => {
		expect(tidy('  Number__theory  ')).toBe('Number theory');
		expect(tidy('x'.repeat(80))).toHaveLength(60);
	});

	it('groups questions by category, "no category" last', () => {
		const q = (id: string, category: string) => ({ id, category }) as Question;
		const g = byCategory([q('1', 'Geometry'), q('2', ''), q('3', 'Algebra'), q('4', 'Geometry')]);
		expect(g.map((x) => [x.category, x.questions.length])).toEqual([['Algebra', 1], ['Geometry', 2], ['', 1]]);
		expect(categoryLine({ Geometry: 100, Algebra: 100, '': 2 })).toBe('No category 2 · Algebra 100 · Geometry 100');
	});
});
