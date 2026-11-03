// A classroom's gradebook and attendance reports (D-122): the data from
// /api/teacher/classrooms/{id}/gradebook and /attendance, tutoring sessions
// merged in, and the reports built from them (CSV, and a printable page).
import type { ClassroomReport } from '../tutoring/types';
import { toCSV } from '../tutoring/types';

export type Item = { kind: 'quiz' | 'tutorial'; id: string; title: string; topic: string; topic_id?: string; module_id?: string; module?: string; weight: number | null; taken: number; avg: number | null };
export type SessionRef = { id: string; quiz_id: string; title: string; status: string; at: string; attempts: number; results: number; avg: number | null };
export type Mark = { pct: number; points?: number };
export type GradeStudent = { user_id: string; name: string; student_number?: string; marks: Record<string, Mark>; average: number | null; total: number; missing: number; final: number | null };
export type Gradebook = { classroom: string; items: Item[]; students: GradeStudent[]; weight_total: number; sessions?: SessionRef[] };

export type Event = { kind: 'quiz' | 'meeting' | 'tutoring'; id: string; title: string; at: string; quiz_id?: string; topic_id?: string; topic?: string; module_id?: string; module?: string };
export type Attendee = { user_id: string; name: string; student_number?: string; attended: string[]; minutes: Record<string, number> };
export type Attendance = { classroom: string; from: string; to: string; events: Event[]; students: Attendee[] };

export const key = (it: { kind: string; id: string }) => `${it.kind}:${it.id}`;
const r2 = (n: number) => Math.round(n * 100) / 100;

/** Points for a mark: 55 % on an item worth 10 % of the final result is 5.5. */
export const points = (pct: number, weight: number) => r2((pct * weight) / 100);

/** The weights being edited: their total, and whether it's allowed (at most 100). */
export function weightTotal(w: Record<string, number | null | undefined>): { total: number; ok: boolean } {
	const total = r2(Object.values(w).reduce<number>((a, v) => a + (v && v > 0 ? v : 0), 0));
	const ok = Object.values(w).every((v) => v == null || (v >= 0 && v <= 100)) && total <= 100;
	return { total, ok };
}

const KIND: Record<Event['kind'], string> = { quiz: 'Quiz', meeting: 'Meeting', tutoring: 'Tutoring' };

/** The platform's quiz sessions and meetings, with the tutoring service's sessions added. */
export function mergeTutoring(a: Attendance, t: ClassroomReport | null): Attendance {
	if (!t) return a;
	const events: Event[] = [...a.events, ...t.sessions.map((s) => ({ kind: 'tutoring' as const, id: s.id, title: s.title, at: s.started_at }))];
	events.sort((x, y) => x.at.localeCompare(y.at));
	const came = new Map(t.students.map((s) => [s.user_id, s]));
	return {
		...a,
		events,
		students: a.students.map((s) => {
			const x = came.get(s.user_id);
			if (!x) return s;
			// The tutoring report gives each student's total time; it's shared out evenly here only for display totals.
			return { ...s, attended: [...s.attended, ...x.attended], minutes: { ...s.minutes, ['tutoring']: Math.round(x.seconds / 60) } };
		})
	};
}

export type AttendanceSummary = { user_id: string; name: string; number: string; byKind: Record<Event['kind'], { came: number; of: number }>; came: number; of: number; rate: number | null; minutes: number };

/** Per student: sessions came to, of how many, by kind and in all. */
export function summarise(a: Attendance): AttendanceSummary[] {
	const of: Record<Event['kind'], number> = { quiz: 0, meeting: 0, tutoring: 0 };
	const kindOf = new Map<string, Event['kind']>();
	for (const e of a.events) {
		of[e.kind]++;
		kindOf.set(e.id, e.kind);
	}
	const total = a.events.length;
	return a.students.map((s) => {
		const byKind = { quiz: { came: 0, of: of.quiz }, meeting: { came: 0, of: of.meeting }, tutoring: { came: 0, of: of.tutoring } };
		const seen = new Set<string>();
		for (const id of s.attended) {
			const k = kindOf.get(id);
			if (k && !seen.has(id)) {
				seen.add(id);
				byKind[k].came++;
			}
		}
		const came = seen.size;
		const minutes = Math.round(Object.values(s.minutes).reduce((x, v) => x + v, 0));
		return { user_id: s.user_id, name: s.name, number: s.student_number ?? '', byKind, came, of: total, rate: total ? Math.round((came / total) * 100) : null, minutes };
	});
}

const dateOf = (iso: string) => iso.slice(0, 10);

/** The results report: each item's mark and points, the average, the weighted total and the final result. */
export function gradebookCSV(g: Gradebook, att?: Map<string, AttendanceSummary>): string {
	const weighted = g.items.filter((i) => i.weight);
	const head = ['Student', 'Student ID'];
	for (const it of g.items) {
		head.push(`${it.title} (%)`);
		if (it.weight) head.push(`${it.title} (points of ${it.weight})`);
	}
	head.push('Average (%)');
	if (weighted.length) head.push('Weighted total', `Out of`, 'Final (%)', 'Weighted items missed');
	if (att) head.push('Sessions attended', 'Sessions', 'Attendance (%)');
	const rows: (string | number)[][] = [head];
	for (const s of g.students) {
		const row: (string | number)[] = [s.name, s.student_number ?? ''];
		for (const it of g.items) {
			const m = s.marks[key(it)];
			row.push(m ? m.pct : '');
			if (it.weight) row.push(m?.points ?? 0);
		}
		row.push(s.average ?? '');
		if (weighted.length) row.push(s.total, g.weight_total, s.final ?? '', s.missing);
		if (att) {
			const a = att.get(s.user_id);
			row.push(a?.came ?? 0, a?.of ?? 0, a?.rate ?? '');
		}
		rows.push(row);
	}
	// The class: each item's average and how many took it.
	rows.push([]);
	rows.push(['Class average', '', ...g.items.flatMap((it) => (it.weight ? [it.avg ?? '', ''] : [it.avg ?? '']))]);
	rows.push(['Students with a mark', '', ...g.items.flatMap((it) => (it.weight ? [it.taken, ''] : [it.taken]))]);
	if (weighted.length) rows.push(['Weight (% of the final result)', '', ...g.items.flatMap((it) => (it.weight ? [it.weight, ''] : ['not counted']))]);
	return toCSV(rows);
}

/** The attendance summary: per student, by kind and in all. */
export function attendanceSummaryCSV(a: Attendance): string {
	const rows: (string | number)[][] = [['Student', 'Student ID', 'Quiz sessions', 'Meetings', 'Tutoring sessions', 'Attended', 'Of', 'Attendance (%)', 'Minutes (meetings, tutoring)']];
	for (const s of summarise(a)) {
		const f = (k: Event['kind']) => (s.byKind[k].of ? `${s.byKind[k].came}/${s.byKind[k].of}` : '');
		rows.push([s.name, s.number, f('quiz'), f('meeting'), f('tutoring'), s.came, s.of, s.rate ?? '', s.minutes]);
	}
	return toCSV(rows);
}

/** Every session as a column: present or absent, with meeting minutes. */
export function attendanceDetailCSV(a: Attendance): string {
	const head = ['Student', 'Student ID', ...a.events.map((e) => `${KIND[e.kind]}: ${e.title} (${dateOf(e.at)})`)];
	const rows: (string | number)[][] = [head];
	for (const s of a.students) {
		const came = new Set(s.attended);
		rows.push([s.name, s.student_number ?? '', ...a.events.map((e) => (came.has(e.id) ? (s.minutes[e.id] != null ? `present (${Math.round(s.minutes[e.id])} min)` : 'present') : 'absent'))]);
	}
	rows.push(['Present', '', ...a.events.map((e) => a.students.filter((s) => s.attended.includes(e.id)).length)]);
	return toCSV(rows);
}

const esc = (s: string | number) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!);

/** A printable page of one table (the browser's Print saves it as PDF). */
export function printHTML(title: string, subtitle: string, head: string[], rows: (string | number)[][]): string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>${esc(title)}</title><style>
body{font:12px/1.4 system-ui,sans-serif;margin:24px;color:#111}h1{font-size:18px;margin:0}p{margin:4px 0 12px;color:#555}
table{border-collapse:collapse;width:100%}th,td{border:1px solid #ccc;padding:4px 6px;text-align:left}th{background:#f2f2f2}
td.n{text-align:right;font-variant-numeric:tabular-nums}@page{size:landscape;margin:12mm}</style></head><body>
<h1>${esc(title)}</h1><p>${esc(subtitle)}</p><table><thead><tr>${head.map((h) => `<th>${esc(h)}</th>`).join('')}</tr></thead><tbody>
${rows.map((r) => `<tr>${r.map((c) => `<td${typeof c === 'number' ? ' class="n"' : ''}>${esc(c)}</td>`).join('')}</tr>`).join('\n')}
</tbody></table><script>window.onload=()=>window.print()<\/script></body></html>`;
}

export function openPrint(html: string) {
	const w = window.open('', '_blank');
	if (!w) return false;
	w.document.write(html);
	w.document.close();
	return true;
}

// ---------- reports of part of a classroom (D-130) ----------

/** What a report covers: the whole classroom, a module, a topic, or one quiz or tutorial (by key). */
export type Scope = { kind: 'all' } | { kind: 'module'; id: string } | { kind: 'topic'; id: string } | { kind: 'item'; id: string };
export type ScopeOption = { value: string; label: string; group: string };

export const scopeValue = (s: Scope) => (s.kind === 'all' ? 'all' : `${s.kind}:${s.id}`);
export function scopeFrom(v: string | null | undefined): Scope {
	const [kind, ...rest] = (v ?? '').split(':');
	const id = rest.join(':');
	return (kind === 'module' || kind === 'topic' || kind === 'item') && id ? { kind, id } : { kind: 'all' };
}

/** The scopes a classroom's items offer: its modules, its topics, then each quiz and tutorial. */
export function scopeOptions(items: Item[]): ScopeOption[] {
	const out: ScopeOption[] = [{ value: 'all', label: 'The whole classroom', group: '' }];
	const seen = new Set<string>();
	for (const it of items) if (it.module_id && !seen.has('m' + it.module_id)) {
		seen.add('m' + it.module_id);
		out.push({ value: `module:${it.module_id}`, label: it.module || 'Module', group: 'Modules' });
	}
	for (const it of items) if (it.topic_id && !seen.has('t' + it.topic_id)) {
		seen.add('t' + it.topic_id);
		out.push({ value: `topic:${it.topic_id}`, label: it.module ? `${it.topic} (${it.module})` : it.topic, group: 'Topics' });
	}
	for (const it of items) out.push({ value: `item:${key(it)}`, label: `${it.title} · ${it.kind === 'quiz' ? 'quiz' : 'tutorial'}`, group: 'Quizzes and tutorials' });
	return out;
}

export function inScope(it: Pick<Item, 'kind' | 'id' | 'topic_id' | 'module_id'>, s: Scope): boolean {
	switch (s.kind) {
		case 'all': return true;
		case 'module': return it.module_id === s.id;
		case 'topic': return it.topic_id === s.id;
		case 'item': return key(it) === s.id;
	}
}

/** The scope's name, for titles and file names. */
export function scopeLabel(items: Item[], s: Scope): string {
	return scopeOptions(items).find((o) => o.value === scopeValue(s))?.label.replace(/ · (quiz|tutorial)$/, '') ?? 'The whole classroom';
}

/**
 * The gradebook for part of the classroom: only the scope's items, and each
 * student's average, points, missed items and final result worked out again
 * over them, as the server does for the whole classroom.
 */
export function scopedGradebook(g: Gradebook, s: Scope): Gradebook {
	if (s.kind === 'all') return g;
	const items = g.items.filter((it) => inScope(it, s));
	const weightTotal = r2(items.reduce((a, it) => a + (it.weight ?? 0), 0));
	const keys = new Set(items.map(key));
	const students = g.students.map((st) => {
		const marks = Object.fromEntries(Object.entries(st.marks).filter(([k]) => keys.has(k)));
		let sum = 0, n = 0, total = 0, missing = 0;
		for (const it of items) {
			const m = marks[key(it)];
			if (!m) {
				if (it.weight) missing++;
				continue;
			}
			sum += m.pct;
			n++;
			if (it.weight) total += m.points ?? points(m.pct, it.weight);
		}
		total = r2(total);
		return { ...st, marks, average: n ? Math.round((sum / n) * 10) / 10 : null, total, missing, final: weightTotal > 0 ? Math.round((total / weightTotal) * 1000) / 10 : null };
	});
	const quizIds = new Set(items.filter((it) => it.kind === 'quiz').map((it) => it.id));
	return { ...g, items, students, weight_total: weightTotal, sessions: (g.sessions ?? []).filter((x) => quizIds.has(x.quiz_id)) };
}

/**
 * Attendance for part of the classroom: the scope's quiz sessions. Meetings
 * and tutoring sessions belong to no module, so they count only for the
 * whole classroom.
 */
export function scopedAttendance(a: Attendance, s: Scope): Attendance {
	if (s.kind === 'all') return a;
	const events = a.events.filter((e) => e.kind === 'quiz' && inScope({ kind: 'quiz', id: e.quiz_id ?? '', topic_id: e.topic_id, module_id: e.module_id }, s));
	const ids = new Set(events.map((e) => e.id));
	return { ...a, events, students: a.students.map((st) => ({ ...st, attended: st.attended.filter((id) => ids.has(id)) })) };
}

/** The scopes attendance offers, from its quiz sessions: modules, topics, quizzes. */
export function attendanceScopes(a: Attendance): ScopeOption[] {
	const items: Item[] = [];
	const seen = new Set<string>();
	for (const e of a.events) if (e.kind === 'quiz' && e.quiz_id && !seen.has(e.quiz_id)) {
		seen.add(e.quiz_id);
		items.push({ kind: 'quiz', id: e.quiz_id, title: e.title, topic: e.topic ?? '', topic_id: e.topic_id, module_id: e.module_id, module: e.module, weight: null, taken: 0, avg: null });
	}
	return scopeOptions(items);
}
