import { describe, expect, it } from 'vitest';
import { advice, estimateMinutes, failures, firstLoadSeconds, parseSteps, reportCSV, verdict, type Run, type Step } from './capacity';

const step = (students: number, passed: boolean, more: Partial<Step> = {}): Step => ({
	students, passed, seconds: 60, joined: students, completed: students, drops: 0, errors: {},
	ops: { login: { n: students, p50: 100, p95: 800, p99: 900, max: 1000 }, save: { n: students * 10, p50: 8, p95: 40, p99: 90, max: 120 } },
	down_mb: 1, up_mb: 1, peak_down_mbps: 2, kb_per_student: 30, cpu_peak: 40, cpu_avg: 20, mem_free_min_mb: 5000, load1_peak: 1,
	heap_peak_mb: 50, db_busy_peak: 4, db_waits: 0, saves_per_sec_peak: 30, ...more
});
const run = (status: Run['status'], steps: Step[], max: number, more: Partial<Run['report']> = {}): Run => ({
	id: 'r', admin_name: 'Admin', status, max, started_at: '', finished_at: '',
	plan: { steps: steps.map((s) => s.students), questions: 10, think_seconds: 3, ramp_seconds: 30, one_network: false, link_mbps: 300 },
	report: { steps, max, page_kb: 900, page_files: 12, machine: { cpus: 6, mem_total_mb: 12000, os: 'linux/amd64' }, db_max: 15, ...more }
});

describe('capacity test (D-125)', () => {
	it('reads the steps an admin types', () => {
		expect(parseSteps('100, 250 500')).toEqual([100, 250, 500]);
		expect(parseSteps('1000; 2000')).toEqual([1000, 2000]);
		expect(parseSteps('500, 200')).toMatch(/more students/);
		expect(parseSteps('5000')).toMatch(/1 to 2,000/);
		expect(parseSteps('')).toMatch(/Enter/);
		expect(parseSteps('1 2 3 4 5 6 7')).toMatch(/At most 6/);
	});

	it('says what the test found', () => {
		expect(verdict(run('passed', [step(100, true), step(250, true)], 250))).toMatch(/every step, up to 250 students/);
		const f = run('failed', [step(500, true), step(1000, false, { reasons: ['only 900 of 1000 students finished'] })], 500);
		expect(verdict(f)).toBe('This server handled 500 students taking a quiz at once; 1,000 was too many: only 900 of 1000 students finished.');
		expect(verdict(run('failed', [step(100, false)], 0))).toMatch(/Even the first step, 100 students/);
		expect(verdict(run('error', [], 0, { error: 'the teacher couldn’t sign in' }))).toMatch(/didn’t run: the teacher/);
	});

	it('works out the first download and the advice', () => {
		expect(firstLoadSeconds(500, 1000, 300)).toBeCloseTo(13.65, 1);
		expect(firstLoadSeconds(500, 0, 300)).toBe(0);
		const r = run('failed', [step(2000, false, { cpu_peak: 97, db_waits: 400, errors: { login_limited: 120, save: 3 } })], 0);
		const a = advice(r).join(' ');
		expect(a).toMatch(/120 sign-ins were refused for trying too often/);
		expect(advice({ ...r, plan: { ...r.plan, one_network: true } }).join(' ')).toMatch(/120 sign-ins were held back by the per-network limit/);
		expect(a).toMatch(/processor reached 97 %/);
		expect(a).toMatch(/waited for a database connection 400 times/);
		expect(failures(r.report!.steps[0])).toBe(3);
		expect(estimateMinutes(r.plan)).toBeGreaterThanOrEqual(1);
	});

	it('downloads the report as a spreadsheet', () => {
		const csv = reportCSV(run('failed', [step(100, true), step(250, false, { reasons: ['a, b'] })], 100));
		const lines = csv.split(/\r?\n/).filter(Boolean);
		expect(lines).toHaveLength(3);
		expect(lines[0]).toMatch(/^students,result,reasons/);
		expect(lines[2]).toMatch(/^250,failed,"a, b",250,250,800,,8,40,90,/);
	});
});
