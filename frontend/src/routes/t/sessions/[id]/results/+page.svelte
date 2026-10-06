<script lang="ts">
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import AnalyticsView from '$lib/AnalyticsView.svelte';
	import { describeKey, describeResponse } from '$lib/answerText';
	import { requireRole } from '$lib/guard.svelte';
	import type { Body, Key, QType, Response, Session } from '$lib/types';
	import { toast } from '$lib/ui/toast.svelte';

	type Mark = { question_id: string; code: string; type: QType; text: string; response: Response | null; key: Key; method: string; status: string; score: number | null; max_score: number; correct: boolean | null; feedback: string; ai_feedback: string; ai_rationale?: string; ai_marked: boolean; flagged: boolean; flag_reason?: string };
	type Result = { attempt_id: string; user_id: string; student_number: string | null; state: string; score: number; max_score: number; pct: number; passed: boolean; complete: boolean; invalidated: boolean; marks: Mark[] };
	type Link = { id: string; scope?: string; token?: string; views: string[]; identify: string; show_answers: boolean; label: string; expires_at: string | null; revoked_at: string | null; created_at: string };
	type Ev = { id: number; kind: string; attempt_id: string | null; details: Record<string, unknown>; created_at: string };

	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let tab = $state<'marking' | 'analytics' | 'share' | 'log'>('marking');
	let session = $state<Session | null>(null);
	let results = $state<Result[]>([]);
	let stats = $state<any>(null);
	let links = $state<Link[]>([]);
	let events = $state<Ev[]>([]);
	let names = $state<Record<string, string>>({});
	let estimate = $state<{ answers: number; estimated_tokens: number } | null>(null);
	let filter = $state<'all' | 'review'>('review');
	let notice = $state('');
	let newToken = $state('');
	let share = $state({ views: ['question_pct'] as string[], identify: 'anonymous', show_answers: false, expires_at: '' });
	let bodies = $state<Record<string, Body>>({});

	async function load() {
		const dash = await api.get<{ session: Session; rows: { attempt_id: string; name: string }[] }>('/api/teacher/sessions/' + id);
		session = dash.session;
		names = Object.fromEntries(dash.rows.map((r) => [r.attempt_id, r.name]));
		results = (await api.get<{ results: Result[] }>('/api/teacher/sessions/' + id + '/results')).results ?? [];
		const qs = (await api.get<{ questions: { id: string; body: Body }[] }>('/api/teacher/quizzes/' + session.quiz_id + '/questions')).questions ?? [];
		bodies = Object.fromEntries(qs.map((q) => [q.id, q.body]));
		estimate = await api.get('/api/teacher/sessions/' + id + '/llm-estimate');
	}
	$effect(() => {
		if (ready() && id) load().catch((e) => (notice = e.message));
	});
	async function openTab(t: typeof tab) {
		tab = t;
		if (t === 'analytics') stats = await api.get('/api/teacher/sessions/' + id + '/analytics');
		if (t === 'share') links = ((await api.get<{ links: Link[] }>('/api/teacher/share-links?target_id=' + id)).links ?? []).filter((l) => !l.scope?.startsWith('live_'));
		if (t === 'log') events = (await api.get<{ events: Ev[] }>('/api/teacher/sessions/' + id + '/events')).events ?? [];
	}
	const needsReview = (m: Mark) => m.status === 'needs_manual' || m.status === 'pending' || m.flagged || m.ai_marked;
	const shown = $derived(filter === 'all' ? results : results.filter((r) => r.marks.some(needsReview)));

	async function override(r: Result, m: Mark) {
		const s = prompt(`Score for ${m.code} (0–${m.max_score})`, m.score == null ? '' : String(m.score));
		if (s === null) return;
		const fb = prompt('Feedback for the student (optional)', m.feedback || m.ai_feedback || '');
		try {
			await api.put('/api/teacher/marks/' + r.attempt_id + '/' + m.question_id, { score: s === '' ? undefined : Number(s), feedback: fb ?? undefined });
			load();
		} catch (e) {
			notice = e instanceof ApiError ? Object.values(e.fields).join('; ') || e.message : 'Save failed';
		}
	}
	async function remark(r: Result, m: Mark) {
		try {
			await api.post('/api/teacher/marks/' + r.attempt_id + '/' + m.question_id + '/remark');
			toast('Re-marking queued', 'info');
			load();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : '';
		}
	}
	async function release(v: boolean) {
		await api.post('/api/teacher/sessions/' + id + (v ? '/release' : '/unrelease'));
		load();
	}
	async function createLink(e: SubmitEvent) {
		e.preventDefault();
		try {
			const l = await api.post<Link>('/api/teacher/share-links', {
				scope: 'session', target_id: id, views: share.views, identify: share.identify, show_answers: share.show_answers,
				expires_at: share.expires_at ? new Date(share.expires_at).toISOString() : null
			});
			newToken = location.origin + '/r/' + l.token;
			openTab('share');
		} catch (err) {
			notice = err instanceof ApiError ? Object.values(err.fields).join('; ') || err.message : '';
		}
	}
	async function revoke(l: Link) {
		await api.del('/api/teacher/share-links/' + l.id);
		openTab('share');
	}
	async function regenerate(l: Link) {
		const n = await api.post<Link>('/api/teacher/share-links/' + l.id + '/regenerate');
		newToken = location.origin + '/r/' + n.token;
		openTab('share');
	}
	function toggleView(v: string) {
		share.views = share.views.includes(v) ? share.views.filter((x) => x !== v) : [...share.views, v];
	}
</script>

<div class="page-container vstack" style="max-width:1200px">
	{#if session}
		<p class="small"><a href={'/t/sessions/' + id}>← Live dashboard</a></p>
		<div class="row">
			<h1 style="margin:0">Results: {session.title}</h1>
			<span class="spacer"></span>
			<a class="btn" href={'/api/teacher/sessions/' + id + '/export.csv'} download>Export CSV</a>
			{#if session.results_released_at}
				<span class="badge badge-soft badge-success">Released</span><button class="btn" onclick={() => release(false)}>Hide results</button>
			{:else}<button class="btn btn-primary" onclick={() => release(true)}>Release results now</button>{/if}
		</div>
		{#if estimate?.answers}<p class="small muted">LLM usage for this session: {estimate.answers} answer(s), about {estimate.estimated_tokens.toLocaleString()} tokens on your own API key.</p>{/if}
		{#if notice}<p class="alert alert-soft alert-warning small">{notice}</p>{/if}
		<div class="tabs tabs-border tabs-scroll" role="tablist">
			<button class="tab" role="tab" aria-selected={tab === 'marking'} class:tab-active={tab === 'marking'} onclick={() => openTab('marking')}>Marking</button>
			<button class="tab" role="tab" aria-selected={tab === 'analytics'} class:tab-active={tab === 'analytics'} onclick={() => openTab('analytics')}>Analytics</button>
			<button class="tab" role="tab" aria-selected={tab === 'share'} class:tab-active={tab === 'share'} onclick={() => openTab('share')}>Public links</button>
			<button class="tab" role="tab" aria-selected={tab === 'log'} class:tab-active={tab === 'log'} onclick={() => openTab('log')}>Integrity log</button>
		</div>

		{#if tab === 'marking'}
			<div class="row small">
				<label class="row" style="font-weight:400"><input class="radio" type="radio" bind:group={filter} value="review" /> Needs review (manual, flagged or AI-marked)</label>
				<label class="row" style="font-weight:400"><input class="radio" type="radio" bind:group={filter} value="all" /> All students</label>
			</div>
			{#each shown as r (r.attempt_id)}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
					<div class="row">
						<strong>{names[r.attempt_id] ?? 'Student'}</strong><span class="muted small">{r.student_number ?? ''}</span>
						<span class="spacer"></span>
						{#if r.invalidated}<span class="badge badge-soft badge-error">invalidated</span>{/if}
						{#if !r.complete}<span class="badge badge-soft badge-warning">incomplete</span>{/if}
						<strong>{r.score}/{r.max_score} ({r.pct}%)</strong>
					</div>
					{#each r.marks.filter((m) => filter === 'all' || needsReview(m)) as m (m.question_id)}
						<div class="mark">
							<div class="row small">
								<strong>{m.code}</strong>
								<span class="badge badge-soft {m.status === 'needs_manual' ? 'warn' : m.status === 'pending' ? '' : 'ok'}">{m.status.replace('_', ' ')}</span>
								{#if m.ai_marked}<span class="badge badge-soft">AI-marked</span>{/if}
								{#if m.flagged}<span class="badge badge-soft badge-error" title={m.flag_reason}>flagged</span>{/if}
								<span class="spacer"></span>
								<span>{m.score ?? '—'} / {m.max_score}</span>
								<button class="btn btn-sm" onclick={() => override(r, m)}>Set mark</button>
								{#if m.type === 'ESSAY' || m.type === 'BLANK_TEXT'}<button class="btn btn-sm" onclick={() => remark(r, m)}>Re-run AI</button>{/if}
							</div>
							<p class="small" style="margin:0.25rem 0"><span class="muted">Answer:</span> <span style="white-space:pre-wrap">{describeResponse(m.type, bodies[m.question_id] ?? {}, m.response)}</span></p>
							<p class="small muted" style="margin:0">Key: {describeKey(m.type, bodies[m.question_id] ?? {}, m.key)}</p>
							{#if m.flag_reason}<p class="small" style="color:var(--danger);margin:0">{m.flag_reason}</p>{/if}
							{#if m.ai_rationale}<p class="small muted" style="margin:0">AI rationale: {m.ai_rationale}</p>{/if}
							{#if m.feedback || m.ai_feedback}<p class="small" style="margin:0">Feedback: {m.feedback} {m.ai_feedback}</p>{/if}
						</div>
					{/each}
				</div>
			{:else}<p class="muted">{filter === 'review' ? 'Nothing needs review.' : 'No finished attempts yet.'}</p>{/each}
		{:else if tab === 'analytics'}
			{#if stats}<AnalyticsView cls={stats.class} questions={stats.questions} students={stats.students} />{:else}<Skeleton lines={4} />{/if}
		{:else if tab === 'share'}
			<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={createLink}>
				<strong>Create a public link</strong>
				<p class="small muted">Anyone with the link can view it. Names and emails are never shown.</p>
				<div class="row">
					{#each [['question_pct', 'Question-wise % correct'], ['pass_rate', 'Class pass rate'], ['individual', 'Individual results']] as [v, l] (v)}
						<label class="row" style="font-weight:400"><input class="checkbox" type="checkbox" checked={share.views.includes(v)} onchange={() => toggleView(v)} /> {l}</label>
					{/each}
				</div>
				{#if share.views.includes('individual')}
					<div class="row">
						<label class="row" style="font-weight:400"><input class="radio" type="radio" bind:group={share.identify} value="anonymous" /> Anonymous</label>
						<label class="row" style="font-weight:400"><input class="radio" type="radio" bind:group={share.identify} value="student_id" /> Show student IDs</label>
						<label class="row" style="font-weight:400"><input class="checkbox" type="checkbox" bind:checked={share.show_answers} /> Include individual answers</label>
					</div>
				{/if}
				<div style="max-width:16rem"><label for="exp">Expires (optional)</label><input class="input w-full" id="exp" type="datetime-local" bind:value={share.expires_at} /></div>
				<button class="btn btn-primary" disabled={!share.views.length}>Create link</button>
				{#if newToken}<p class="alert alert-soft alert-success small">Copy this link now; it is only shown once: <a href={newToken} target="_blank" style="word-break:break-all">{newToken}</a></p>{/if}
			</form>
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
				<table class="table"><thead><tr><th>Shows</th><th>Created</th><th>Expires</th><th>Status</th><th></th></tr></thead><tbody>
					{#each links as l (l.id)}
						<tr>
							<td class="small">{l.views.join(', ')}{l.identify === 'student_id' ? ' · IDs' : ''}{l.show_answers ? ' · answers' : ''}</td>
							<td class="small">{new Date(l.created_at).toLocaleString()}</td>
							<td class="small">{l.expires_at ? new Date(l.expires_at).toLocaleString() : 'never'}</td>
							<td>{#if l.revoked_at}<span class="badge badge-soft">revoked</span>{:else}<span class="badge badge-soft badge-success">active</span>{/if}</td>
							<td class="row">{#if !l.revoked_at}<button class="btn btn-sm" onclick={() => revoke(l)}>Revoke</button>{/if}<button class="btn btn-sm" onclick={() => regenerate(l)}>New link</button></td>
						</tr>
					{/each}
				</tbody></table>
			</div>
		{:else}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
				<table class="table"><thead><tr><th>Time</th><th>Event</th><th>Student</th><th>Details</th></tr></thead><tbody>
					{#each events as e (e.id)}
						<tr class:violation={e.kind === 'violation'}>
							<td class="small">{new Date(e.created_at).toLocaleTimeString()}</td>
							<td>{e.kind.replaceAll('_', ' ')}</td>
							<td class="small">{e.attempt_id ? (names[e.attempt_id] ?? '') : ''}</td>
							<td class="small">{Object.entries(e.details ?? {}).map(([k, v]) => `${k}: ${v}`).join(', ')}</td>
						</tr>
					{/each}
				</tbody></table>
			</div>
		{/if}
	{/if}
</div>

<style>
	.mark { border-top: 1px solid var(--color-base-300); padding-top: 0.5rem; }
	tr.violation td { color: var(--danger); }
</style>
