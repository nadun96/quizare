import { describe, expect, it } from 'vitest';
import { attendanceRows, csvCell, toCSV, type ClassroomReport } from './types';

// Attendance reports in the browser (D-87).

describe('attendance reports', () => {
	const report: ClassroomReport = {
		sessions: [
			{ id: 's1', title: 'One', started_at: '2026-10-01T10:00:00Z', ended_at: '2026-10-01T11:00:00Z', seconds: 3600 },
			{ id: 's2', title: 'Two', started_at: '2026-10-08T10:00:00Z', ended_at: '2026-10-08T11:00:00Z', seconds: 3600 }
		],
		students: [
			{ user_id: 'a', name: 'Ann', attended: ['s1', 's2'], seconds: 5400 },
			{ user_id: 'x', name: 'Left the class', attended: ['s1'], seconds: 600 }
		],
		counts_in_results: true
	};

	it('sets the report beside the roster: those who never came too, those who left not', () => {
		const rows = attendanceRows(report, [
			{ user_id: 'c', student_name: 'Cy', student_number: 'S3' },
			{ user_id: 'a', student_name: 'Ann', student_number: null }
		]);
		expect(rows).toEqual([
			{ user_id: 'a', name: 'Ann', number: '', attended: 2, of: 2, rate: 100, minutes: 90 },
			{ user_id: 'c', name: 'Cy', number: 'S3', attended: 0, of: 2, rate: 0, minutes: 0 }
		]);
		expect(attendanceRows({ ...report, sessions: [] }, [{ user_id: 'c', student_name: 'Cy' }])[0].rate).toBeNull();
	});

	it('writes CSV a spreadsheet can’t run', () => {
		expect(csvCell('=SUM(A1)')).toBe("'=SUM(A1)");
		expect(csvCell('-1')).toBe("'-1");
		expect(csvCell('Perera, Ann')).toBe('"Perera, Ann"');
		expect(csvCell('say "hi"')).toBe('"say ""hi"""');
		expect(csvCell(null)).toBe('');
		expect(toCSV([['a', 1], ['b', 2]])).toBe('a,1\r\nb,2\r\n');
	});
});
