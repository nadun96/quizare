<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	import MathText from '$lib/richtext/MathText.svelte';
	import { promptDialog } from '$lib/ui/dialog.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import SaveToDrive from '$lib/drive/SaveToDrive.svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import AnalyticsView from '$lib/AnalyticsView.svelte';
	import { describeKey, describeResponse } from '$lib/answerText';
	import { requireRole } from '$lib/guard.svelte';
	import type { Body, Key, QType, Response, Session } from '$lib/types';
	import { toast } from '$lib/ui/toast.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import Icon from '$lib/ui/Icon.svelte';

	type Mark = { question_id: string; code: string; type: QType; text: string; response: Response | null; key: Key; method: string; status: string; score: number | null; max_score: number; correct: boolean | null; feedback: string; ai_feedback: string; ai_rationale?: string; ai_marked: boolean; flagged: boolean; flag_reason?: string };
	type Result = { name?: string; violations?: number; attempt_id: string; user_id: string; student_number: string | null; state: string; score: number; max_score: number; pct: number; passed: boolean; complete: boolean; invalidated: boolean; marks: Mark[]; try?: number; tries?: number };
	type Link = { id: string; scope?: string; token?: string; views: string[]; identify: string; show_answers: boolean; label: string; expires_at: string | null; revoked_at: string | null; created_at: string };
	type Ev = { id: number; kind: string; attempt_id: string | null; details: Record<string, unknown>; created_at: string };

	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let tab = $state<'marking' | 'analytics' | 'share' | 'log'>('marking');
	let session = $state<Session | null>(null);
	let filter = $state<'all' | 'review'>(Paged.fromUrl(urlState, 'review', 'res') === '0' ? 'all' : 'review');
	let kind = $state(Paged.fromUrl(urlState, 'kind', 'log'));
	// Only some students (D-161): passed, failed, incomplete, invalidated or with violations.
	let show = $state(Paged.fromUrl(urlState, 'show', 'res'));
	// Results, links and the integrity log, one page at a time on the server (PL-FR-02).
	const results = new Paged<Result>(() => '/api/teacher/sessions/' + id + '/results', 'results', {
		url: urlState, prefix: 'res', sort: 'joined', filters: () => ({ review: filter === 'review' ? '1' : '0', show: show || undefined })
	});
	const links = new Paged<Link>(() => '/api/teacher/share-links', 'links', { sort: 'created', desc: true, filters: () => ({ target_id: id, scope: 'session' }) });
	const events = new Paged<Ev>(() => '/api/teacher/sessions/' + id + '/events', 'events', { url: urlState, prefix: 'log', sort: 'time', filters: () => ({ kind }) });
	let stats = $state<any>(null);
	let names = $state<Record<string, string>>({});
	// By student too: the try that counts may be an earlier one than the dashboard's (D-131).
	let byUser = $state<Record<string, string>>({});
	let estimate = $state<{ answers: number; estimated_tokens: number } | null>(null);
	let notice = $state('');
	let newToken = $state('');
	let share = $state({ views: ['question_pct'] as string[], identify: 'anonymous', show_answers: false, expires_at: '' });
	let bodies = $state<Record<string, Body>>({});

	async function load() {
		const dash = await api.get<{ session: Session; rows: { attempt_id: string; user_id: string; name: string }[] }>('/api/teacher/sessions/' + id);
		session = dash.session;
		names = Object.fromEntries(dash.rows.map((r) => [r.attempt_id, r.name]));
		byUser = Object.fromEntries(dash.rows.map((r) => [r.user_id, r.name]));
		await results.load();
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
		if (t === 'share') await links.load();
		if (t === 'log') await events.load();
	}
	const needsReview = (m: Mark) => m.status === 'needs_manual' || m.status === 'pending' || m.flagged || m.ai_marked;
	const shown = $derived(results.rows);
	function setFilter(f: 'all' | 'review') {
		filter = f;
		results.refilter();
	}

	async function override(r: Result, m: Mark) {
		const s = await promptDialog({ title: tr("Score for {0}", [m.code]), body: tr("Between 0 and {0}. Leave it empty to keep the current mark.", [m.max_score]), label: tr("Score"), value: m.score == null ? '' : String(m.score), inputmode: 'decimal', allowEmpty: true, confirm: tr("Next") });
		if (s === null) return;
		const fb = await promptDialog({ title: tr("Feedback for the student"), body: tr("Optional. Shown with the mark when results are released."), label: tr("Feedback"), value: m.feedback || m.ai_feedback || '', allowEmpty: true, multiline: true, maxlength: 2000 });
		try {
			await api.put('/api/teacher/marks/' + r.attempt_id + '/' + m.question_id, { score: s === '' ? undefined : Number(s), feedback: fb ?? undefined });
			load();
		} catch (e) {
			notice = e instanceof ApiError ? (Object.values(e.fields).join('; ') || e.message) : tr("Save failed");
		}
	}
	async function remark(r: Result, m: Mark) {
		try {
			await api.post('/api/teacher/marks/' + r.attempt_id + '/' + m.question_id + '/remark');
			toast(tr("Re-marking queued"), 'info');
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
			notice = err instanceof ApiError ? (Object.values(err.fields).join('; ') || err.message) : '';
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
		<p class="small"><a href={'/t/sessions/' + id}>{tr("← Live dashboard")}</a></p>
		<div class="row">
			<h1 style="margin:0">{tr("Results: {0}", [session.title])}</h1>
			<span class="spacer"></span>
			{#if session.classroom_id}<a class="btn" href={`/t/classrooms/${session.classroom_id}?tab=report&scope=session:${id}`}><Icon name="table" size={16} />{tr("Class report")}</a>{/if}
			<a class="btn" href={'/api/teacher/sessions/' + id + '/export.csv'} download>{tr("Export CSV")}</a>
			<SaveToDrive kind="session" {id} />
			{#if session.results_released_at}
				<span class="badge badge-soft badge-success">{tr("Released")}</span><button class="btn" onclick={() => release(false)}>{tr("Hide results")}</button>
			{:else}<button class="btn btn-primary" onclick={() => release(true)}>{tr("Release results now")}</button>{/if}
		</div>
		{#if estimate?.answers}<p class="small muted">{tr("LLM usage for this session: {0} answer(s), about {1} tokens on your own API key.", [estimate.answers, estimate.estimated_tokens.toLocaleString(localeTag())])}</p>{/if}
		{#if notice}<p class="alert alert-soft alert-warning small">{notice}</p>{/if}
		<div class="tabs tabs-border tabs-scroll" role="tablist">
			<button class="tab" role="tab" aria-selected={tab === 'marking'} class:tab-active={tab === 'marking'} onclick={() => openTab('marking')}>{tr("Marking")}</button>
			<button class="tab" role="tab" aria-selected={tab === 'analytics'} class:tab-active={tab === 'analytics'} onclick={() => openTab('analytics')}>{tr("Analytics")}</button>
			<button class="tab" role="tab" aria-selected={tab === 'share'} class:tab-active={tab === 'share'} onclick={() => openTab('share')}>{tr("Public links")}</button>
			<button class="tab" role="tab" aria-selected={tab === 'log'} class:tab-active={tab === 'log'} onclick={() => openTab('log')}>{tr("Integrity log")}</button>
		</div>

		{#if tab === 'marking'}
			<div class="row small">
				<label class="row" style="font-weight:400"><input class="radio" type="radio" name="rf" checked={filter === 'review'} onchange={() => setFilter('review')} /> {tr("Needs review (manual, flagged or AI-marked)")}</label>
				<label class="row" style="font-weight:400"><input class="radio" type="radio" name="rf" checked={filter === 'all'} onchange={() => setFilter('all')} /> {tr("All students")}</label>
				<span class="spacer"></span>
				<ListSearch list={results} placeholder={tr("Name or student ID")} label={tr("Search results by name or student ID")} />
				<label class="small muted flex items-center gap-2">{tr("Show")}
					<select class="select select-sm w-auto" bind:value={show} onchange={() => results.refilter()}>
						<option value="">{tr("Everyone")}</option><option value="passed">{tr("Passed")}</option><option value="failed">{tr("Didn’t pass")}</option>
						<option value="incomplete">{tr("Still being marked")}</option><option value="invalidated">{tr("Invalidated")}</option><option value="violations">{tr("With violations")}</option>
					</select></label>
				<label class="small muted flex items-center gap-2">{tr("Sort")}
					<select class="select select-sm w-auto" value={results.sort} onchange={(e) => results.sortBy(e.currentTarget.value)}>
						<option value="joined">{tr("Join order")}</option><option value="number">{tr("Student ID")}</option><option value="score">{tr("Score")}</option>
					</select></label>
			</div>
			{#each shown as r (r.attempt_id)}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
					<div class="row">
						<strong>{r.name || names[r.attempt_id] || byUser[r.user_id] || tr("Student")}</strong><span class="muted small">{r.student_number ?? ''}</span>{#if (r.tries ?? 1) > 1}<span class="badge badge-soft badge-info badge-sm" title={tr("The best try counts")}>{tr("best of {0} tries · try {1}", [r.tries, r.try])}</span>{/if}
						<span class="spacer"></span>
						{#if r.invalidated}<span class="badge badge-soft badge-error">{tr("invalidated")}</span>{/if}
						{#if !r.complete}<span class="badge badge-soft badge-warning">{tr("incomplete")}</span>{/if}
						<strong>{r.score}/{r.max_score} ({r.pct}%)</strong>
					</div>
					{#each r.marks.filter((m) => filter === 'all' || needsReview(m)) as m (m.question_id)}
						<div class="mark">
							<div class="row small">
								<strong>{m.code}</strong>
								<span class="badge badge-soft {m.status === 'needs_manual' ? 'warn' : m.status === 'pending' ? '' : 'ok'}">{m.status.replace('_', ' ')}</span>
								{#if m.ai_marked}<span class="badge badge-soft">{tr("AI-marked")}</span>{/if}
								{#if m.flagged}<span class="badge badge-soft badge-error" title={m.flag_reason}>{tr("flagged")}</span>{/if}
								<span class="spacer"></span>
								<span>{m.score ?? '—'} / {m.max_score}</span>
								<button class="btn btn-sm" onclick={() => override(r, m)}>{tr("Set mark")}</button>
								{#if m.type === 'ESSAY' || m.type === 'BLANK_TEXT'}<button class="btn btn-sm" onclick={() => remark(r, m)}>{tr("Re-run AI")}</button>{/if}
							</div>
							<p class="small" style="margin:0.25rem 0"><span class="muted">{tr("Answer:")}</span> <span style="white-space:pre-wrap"><MathText text={describeResponse(m.type, bodies[m.question_id] ?? {}, m.response)} /></span></p>
							<p class="small muted" style="margin:0">{tr("Key:")} <MathText text={describeKey(m.type, bodies[m.question_id] ?? {}, m.key)} /></p>
							{#if m.flag_reason}<p class="small" style="color:var(--danger);margin:0">{m.flag_reason}</p>{/if}
							{#if m.ai_rationale}<p class="small muted" style="margin:0">{tr("AI rationale: {0}", [m.ai_rationale])}</p>{/if}
							{#if m.feedback || m.ai_feedback}<p class="small" style="margin:0">{tr("Feedback: {0} {1}", [m.feedback, m.ai_feedback])}</p>{/if}
						</div>
					{/each}
				</div>
			{:else}<p class="muted">{!results.loaded ? tr("Loading…") : results.q || show ? tr("No results match.") : filter === 'review' ? tr("Nothing needs review.") : tr("No finished attempts yet.")}</p>{/each}
			<Pager list={results} label={tr("Results")} />
		{:else if tab === 'analytics'}
			{#if stats}<AnalyticsView cls={stats.class} questions={stats.questions} students={stats.students} />{:else}<Skeleton lines={4} />{/if}
		{:else if tab === 'share'}
			<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={createLink}>
				<strong>{tr("Create a public link")}</strong>
				<p class="small muted">{tr("Anyone with the link can view it. Names and emails are never shown.")}</p>
				<div class="row">
					{#each [['question_pct', 'Question-wise % correct'], ['pass_rate', 'Class pass rate'], ['individual', 'Individual results']] as [v, l] (v)}
						<label class="row" style="font-weight:400"><input class="checkbox" type="checkbox" checked={share.views.includes(v)} onchange={() => toggleView(v)} /> {l}</label>
					{/each}
				</div>
				{#if share.views.includes('individual')}
					<div class="row">
						<label class="row" style="font-weight:400"><input class="radio" type="radio" bind:group={share.identify} value="anonymous" /> {tr("Anonymous")}</label>
						<label class="row" style="font-weight:400"><input class="radio" type="radio" bind:group={share.identify} value="student_id" /> {tr("Show student IDs")}</label>
						<label class="row" style="font-weight:400"><input class="checkbox" type="checkbox" bind:checked={share.show_answers} /> {tr("Include individual answers")}</label>
					</div>
				{/if}
				<div style="max-width:16rem"><label for="exp">{tr("Expires (optional)")}</label><input class="input w-full" id="exp" type="datetime-local" bind:value={share.expires_at} /></div>
				<button class="btn btn-primary" disabled={!share.views.length}>{tr("Create link")}</button>
				{#if newToken}<p class="alert alert-soft alert-success small">{tr("Copy this link now; it is only shown once:")} <a href={newToken} target="_blank" style="word-break:break-all">{newToken}</a></p>{/if}
			</form>
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
				<table class="table"><thead><tr><th>{tr("Shows")}</th><th>{tr("Created")}</th><th>{tr("Expires")}</th><th>{tr("Status")}</th><th></th></tr></thead><tbody>
					{#each links.rows as l (l.id)}
						<tr>
							<td class="small">{l.views.join(', ')}{l.identify === 'student_id' ? tr(" · IDs") : ''}{l.show_answers ? tr(" · answers") : ''}</td>
							<td class="small">{new Date(l.created_at).toLocaleString(localeTag())}</td>
							<td class="small">{l.expires_at ? new Date(l.expires_at).toLocaleString(localeTag()) : 'never'}</td>
							<td>{#if l.revoked_at}<span class="badge badge-soft">{tr("revoked")}</span>{:else}<span class="badge badge-soft badge-success">{tr("active")}</span>{/if}</td>
							<td class="row">{#if !l.revoked_at}<button class="btn btn-sm" onclick={() => revoke(l)}>{tr("Revoke")}</button>{/if}<button class="btn btn-sm" onclick={() => regenerate(l)}>{tr("New link")}</button></td>
						</tr>
					{:else}<tr><td colspan="5" class="muted">{links.loaded ? tr("No links yet.") : tr("Loading…")}</td></tr>
					{/each}
				</tbody></table>
			</div>
			<Pager list={links} label={tr("Links")} />
		{:else}
			<div class="row">
				<ListSearch list={events} placeholder={tr("Search events")} label={tr("Search the integrity log")} />
				<select class="select select-sm w-auto" bind:value={kind} onchange={() => events.refilter()} aria-label={tr("Event kind")}>
					<option value="">{tr("All events")}</option><option value="violation">{tr("Violations")}</option><option value="admitted">{tr("Admitted")}</option><option value="started">{tr("Started")}</option><option value="submitted">{tr("Submitted")}</option><option value="reinstated">{tr("Reinstated")}</option><option value="reopened">{tr("Reopened")}</option>
				</select>
			</div>
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
				<table class="table"><thead><tr><th>{tr("Time")}</th><th>{tr("Event")}</th><th>{tr("Student")}</th><th>{tr("Details")}</th></tr></thead><tbody>
					{#each events.rows as e (e.id)}
						<tr class:violation={e.kind === 'violation'}>
							<td class="small">{new Date(e.created_at).toLocaleTimeString(localeTag())}</td>
							<td>{e.kind.replaceAll('_', ' ')}</td>
							<td class="small">{e.attempt_id ? (names[e.attempt_id] ?? '') : ''}</td>
							<td class="small">{Object.entries(e.details ?? {}).map(([k, v]) => `${k}: ${v}`).join(', ')}</td>
						</tr>
					{:else}<tr><td colspan="4" class="muted">{events.loaded ? tr("No events.") : tr("Loading…")}</td></tr>
					{/each}
				</tbody></table>
			</div>
			<Pager list={events} label={tr("Integrity log")} />
		{/if}
	{/if}
</div>

<style>
	.mark { border-top: 1px solid var(--color-base-300); padding-top: 0.5rem; }
	tr.violation td { color: var(--danger); }
</style>
