import { describe, expect, it } from 'vitest';
import { activeFilters, emptyFilter, emptyProps, filterFrom, filterParams, groupBy, hueOf, parseTags, propsLine, term } from './props';

const p = (more: Partial<ReturnType<typeof emptyProps>>) => ({ ...emptyProps(), ...more });

describe('details of classrooms, meetings and sessions (D-128)', () => {
	it('puts the details on one line', () => {
		expect(propsLine(p({ course_code: 'IS2201', department: 'Information Systems', year: 2025, semester: 'Semester 1' }))).toBe('IS2201 · Information Systems · 2025 · Semester 1');
		expect(propsLine(p({ course_code: 'IS2201', year: 2025 }), false)).toBe('2025');
		expect(propsLine(undefined)).toBe('');
	});

	it("says where a classroom's term is", () => {
		const today = new Date(2026, 2, 1); // 1 Mar 2026
		expect(term(p({}), today)).toBeNull();
		expect(term(p({ starts_on: '2026-04-01', ends_on: '2026-07-31' }), today)?.state).toBe('upcoming');
		expect(term(p({ starts_on: '2025-09-01', ends_on: '2026-01-31' }), today)).toMatchObject({ state: 'ended', label: 'Ended' });
		const run = term(p({ starts_on: '2026-01-01', ends_on: '2026-03-11' }), today)!;
		expect(run.state).toBe('running');
		expect(run.label).toBe('10 days left');
		expect(run.progress).toBeGreaterThan(0.8);
		expect(term(p({ starts_on: '2026-01-01' }), today)).toMatchObject({ state: 'running', label: 'Running', progress: null });
	});

	it('turns the filter into query parameters and back', () => {
		const f = { ...emptyFilter(), department: 'IS', year: '2025', tags: ['lab', 'first year'], when: 'current' as const };
		const q = filterParams(f);
		expect(q).toEqual({ department: 'IS', course_code: undefined, year: '2025', semester: undefined, tags: 'lab,first year', when: 'current' });
		const back = filterFrom(new URLSearchParams(Object.entries(q).filter(([, v]) => v) as [string, string][]));
		expect(back).toEqual(f);
		expect(activeFilters(f)).toBe(5);
		expect(filterFrom(new URLSearchParams('when=soon')).when).toBe('');
	});

	it('reads typed tags once each', () => {
		expect(parseTags(' #Lab, first   year,lab ', ['Lab'])).toEqual(['Lab', 'first year']);
		expect(parseTags('a,b,c,d,e,f,g,h,i,j,k,l')).toHaveLength(10);
	});

	it('groups rows, "none" last, years newest first', () => {
		const rows = [{ props: p({ year: 2024 }) }, { props: p({ year: null }) }, { props: p({ year: 2025 }) }];
		expect(groupBy(rows, 'year', 'No year').map((g) => g.label)).toEqual(['2025', '2024', 'No year']);
		expect(groupBy(rows, '', '')).toHaveLength(1);
		expect(hueOf('Information Systems')).toBe(hueOf('information systems'));
	});
});
