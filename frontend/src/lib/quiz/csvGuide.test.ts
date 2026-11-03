import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { COLUMNS, guideCSV, TYPES } from './csvGuide';

const csvGo = readFileSync(join(__dirname, '../../../../backend/internal/quiz/csv.go'), 'utf8');

describe('the CSV column guide (D-135)', () => {
	it('describes every column the importer reads, in the template’s order', () => {
		const block = csvGo.slice(csvGo.indexOf('var TemplateColumns'), csvGo.indexOf('}', csvGo.indexOf('var TemplateColumns')));
		const backend = [...block.matchAll(/"([a-z_]+)"/g)].map((m) => m[1]);
		expect(backend.length).toBeGreaterThan(10);
		expect(COLUMNS.map((c) => c.name)).toEqual(backend);
	});

	it('explains every question type', () => {
		const goTypes = readFileSync(join(__dirname, '../../../../backend/internal/quiz/question.go'), 'utf8').match(/type must be one of ([A-Z_, ]+)/)![1];
		expect(TYPES.map((t) => t.type)).toEqual(goTypes.split(',').map((s) => s.trim()));
	});

	it('downloads as a spreadsheet', () => {
		const lines = guideCSV().split('\r\n');
		expect(lines[0]).toBe('Column,Required,What it means,How to write it,Example');
		expect(lines[1].startsWith('question_code,yes,')).toBe(true);
		expect(lines.some((l) => l.startsWith('MATCH,Match pairs,'))).toBe(true);
	});
});
