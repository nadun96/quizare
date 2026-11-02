// The capacity test (D-125): its plan, report and the sums the admin's page
// shows. The server measures; this turns the numbers into sentences.

export type Plan = { steps: number[]; questions: number; think_seconds: number; ramp_seconds: number; one_network: boolean; link_mbps: number };
export type Latency = { n: number; p50: number; p95: number; p99: number; max: number };
export type Sample = { t: number; cpu: number; mem_free_mb: number; load1: number; heap_mb: number; goroutines: number; db_busy: number; db_waits: number; down_mbps: number; up_mbps: number; active: number; saves: number };
export type Step = {
	students: number; passed: boolean; reasons?: string[]; seconds: number; joined: number; completed: number;
	ops: Record<string, Latency>; errors: Record<string, number>; first_error?: Record<string, string>; drops: number;
	down_mb: number; up_mb: number; peak_down_mbps: number; kb_per_student: number;
	cpu_peak: number; cpu_avg: number; mem_free_min_mb: number; load1_peak: number; heap_peak_mb: number;
	db_busy_peak: number; db_waits: number; saves_per_sec_peak: number; samples?: Sample[];
};
export type Report = { steps: Step[]; max: number; page_kb: number; page_files: number; machine: { cpus: number; mem_total_mb: number; os: string }; error?: string; db_max: number };
export type Status = 'running' | 'passed' | 'failed' | 'stopped' | 'interrupted' | 'error';
export type Run = { id: string; admin_name: string; status: Status; plan: Plan; report?: Report; max: number; started_at: string; finished_at: string | null };
export type Progress = { run_id: string; plan: Plan; step: number; students: number; phase: string; joined: number; finished: number; errors: number; elapsed: number; sample: Sample | null; done: Step[]; stopping: boolean };

export const PRESETS: { key: string; label: string; hint: string; steps: number[] }[] = [
	{ key: 'limit', label: 'Find the limit', hint: '100, 250, 500, 1,000, then 2,000 students, stopping at the first that’s too many', steps: [100, 250, 500, 1000, 2000] },
	{ key: 'class', label: 'One big class', hint: '500 students at once', steps: [500] },
	{ key: 'quick', label: 'Quick check', hint: '50 students, about a minute', steps: [50] }
];

export const STATUS: Record<Status, { label: string; tone: string }> = {
	running: { label: 'Running', tone: 'badge-info' },
	passed: { label: 'Every step passed', tone: 'badge-success' },
	failed: { label: 'Found the limit', tone: 'badge-warning' },
	stopped: { label: 'Stopped', tone: 'badge-ghost' },
	interrupted: { label: 'Cut short', tone: 'badge-ghost' },
	error: { label: 'Didn’t run', tone: 'badge-error' }
};

/** "100, 250, 500" → [100, 250, 500], or why not. */
export function parseSteps(text: string, most = 2000, maxSteps = 6): number[] | string {
	const parts = text.split(/[\s,;]+/).filter(Boolean);
	if (!parts.length) return 'Enter how many students, for example 100, 250, 500';
	const steps = parts.map((p) => Number(p.replace(/[^\d]/g, '')));
	if (steps.some((n) => !Number.isInteger(n) || n < 1 || n > most)) return `Each step is 1 to ${most.toLocaleString()} students`;
	if (steps.length > maxSteps) return `At most ${maxSteps} steps`;
	if (steps.some((n, i) => i > 0 && n <= steps[i - 1])) return 'Each step needs more students than the one before';
	return steps;
}

/** About how long a test of this plan takes, in minutes, if every step runs. */
export function estimateMinutes(p: Plan): number {
	const perStep = p.ramp_seconds + 15 + p.questions * p.think_seconds * 1.1 + 10;
	return Math.max(1, Math.round((p.steps.length * perStep + 20) / 60));
}

/** Seconds for n students' first visit to download the app over the server's upload. */
export function firstLoadSeconds(n: number, pageKB: number, mbps: number): number {
	if (!pageKB || !mbps) return 0;
	return (n * pageKB * 1024 * 8) / (mbps * 1e6);
}

export const fmtMs = (ms: number | undefined) => (ms === undefined || !Number.isFinite(ms) ? '–' : ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms)} ms`);
export const fmtPct = (x: number) => (x < 0 ? '–' : `${Math.round(x)} %`);
export const fmtMB = (x: number) => (x < 0 ? '–' : x >= 1024 ? `${(x / 1024).toFixed(1)} GB` : `${Math.round(x)} MB`);
export const fmtSecs = (s: number) => (s < 1 ? 'under a second' : s < 90 ? `${Math.round(s)} s` : `${Math.round(s / 60)} min`);

/** Failed requests, leaving out sign-ins the server asked to retry. */
export function failures(st: Step): number {
	return Object.entries(st.errors ?? {}).reduce((n, [k, v]) => (k === 'login_busy' || k === 'login_limited' ? n : n + v), 0);
}

/** The test's answer in one sentence. */
export function verdict(r: Run): string {
	const rep = r.report;
	if (r.status === 'running') return 'The test is running.';
	if (!rep || (!rep.steps?.length && rep.error)) return rep?.error ? `The test didn’t run: ${rep.error}` : 'The test didn’t run.';
	const last = rep.steps[rep.steps.length - 1];
	const n = (x: number) => x.toLocaleString();
	if (r.status === 'passed') return `This server handled every step, up to ${n(rep.max)} students taking a quiz at once.`;
	if (r.status === 'failed') {
		const why = last.reasons?.[0] ? `: ${last.reasons[0]}` : '';
		return rep.max ? `This server handled ${n(rep.max)} students taking a quiz at once; ${n(last.students)} was too many${why}.` : `Even the first step, ${n(last.students)} students, was too many${why}.`;
	}
	if (r.status === 'stopped') return rep.max ? `Stopped early; up to then it handled ${n(rep.max)} students.` : 'Stopped before a step finished.';
	if (r.status === 'interrupted') return 'The server stopped during the test, so it was cut short.';
	return rep.error ? `Something went wrong: ${rep.error}` : 'Something went wrong.';
}

/** Advice from the report, for the admin. */
export function advice(r: Run): string[] {
	const rep = r.report;
	if (!rep?.steps?.length) return [];
	const out: string[] = [];
	const limited = rep.steps.reduce((n, s) => n + (s.errors?.login_limited ?? 0), 0);
	if (limited)
		out.push(r.plan.one_network
			? `${limited.toLocaleString()} sign-ins were held back by the per-network limit (300 at once, then 5 a second from one internet address) and tried again. A school where everyone shares one address should have students sign in a few minutes early, or ask for the limit to be raised.`
			: `${limited.toLocaleString()} sign-ins were refused for trying too often: students who kept retrying while the server was busy reached the per-account limit (10 tries, then one every 30 s).`);
	const busy = rep.steps.reduce((n, s) => n + (s.errors?.login_busy ?? 0), 0);
	if (busy) out.push(`The server asked ${busy.toLocaleString()} sign-ins to wait while it checked passwords (two at a time, by design). Signing in early spreads this out.`);
	const top = rep.steps[rep.steps.length - 1];
	if (top.cpu_peak >= 85) out.push(`The processor reached ${Math.round(top.cpu_peak)} % with ${top.students.toLocaleString()} students: that’s the limit here. More cores would raise it.`);
	if (top.db_waits > 100) out.push(`Requests waited for a database connection ${top.db_waits.toLocaleString()} times at ${top.students.toLocaleString()} students (the server uses ${rep.db_max}).`);
	if (rep.page_kb && r.plan.link_mbps) {
		const s = firstLoadSeconds(rep.max || rep.steps[0].students, rep.page_kb, r.plan.link_mbps);
		if (s > 30) out.push(`If ${(rep.max || rep.steps[0].students).toLocaleString()} students open the app for the first time at the same moment, the download takes about ${fmtSecs(s)} at ${r.plan.link_mbps} Mbit/s. After that their browsers keep it.`);
	}
	return out;
}

/** The report as a spreadsheet: one row per step. */
export function reportCSV(r: Run): string {
	const cols = ['students', 'result', 'reasons', 'joined', 'finished', 'sign-in p95 ms', 'join p95 ms', 'save p50 ms', 'save p95 ms', 'save p99 ms', 'advance p95 ms', 'failed requests', 'dropped connections', 'cpu peak %', 'cpu average %', 'free memory min MB', 'db connections peak', 'db waits', 'KB per student', 'peak Mbit/s', 'seconds'];
	const q = (v: unknown) => {
		const s = String(v ?? '');
		return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
	};
	const rows = (r.report?.steps ?? []).map((s) => [
		s.students, s.passed ? 'passed' : 'failed', (s.reasons ?? []).join('; '), s.joined, s.completed,
		s.ops.login?.p95 ?? '', s.ops.join?.p95 ?? '', s.ops.save?.p50 ?? '', s.ops.save?.p95 ?? '', s.ops.save?.p99 ?? '', s.ops.advance?.p95 ?? '',
		failures(s), s.drops, s.cpu_peak < 0 ? '' : s.cpu_peak, s.cpu_avg < 0 ? '' : s.cpu_avg, s.mem_free_min_mb < 0 ? '' : s.mem_free_min_mb,
		s.db_busy_peak, s.db_waits, s.kb_per_student, s.peak_down_mbps, s.seconds
	]);
	return [cols, ...rows].map((row) => row.map(q).join(',')).join('\r\n') + '\r\n';
}
