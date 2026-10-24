<script lang="ts">
	// A quiz's workspace: questions, CSV import, pools, settings, sessions and
	// preview. The question bank (D-66) is a quiz of kind "bank" and uses the
	// same page with only questions, import and preview.
	import RowMenu from '$lib/ui/RowMenu.svelte';
	import PoolsEditor from '$lib/PoolsEditor.svelte';
	import { count } from '$lib/words';
	import Icon from '$lib/ui/Icon.svelte';
	import { goto } from '$app/navigation';
	import Sortable from 'sortablejs';
	import { api, ApiError } from '$lib/api';
	import { describeKey } from '$lib/answerText';
	import { confirmDialog, promptDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { requireRole } from '$lib/guard.svelte';
	import QuestionEditor from '$lib/QuestionEditor.svelte';
	import QuestionView from '$lib/QuestionView.svelte';
	import RichText from '$lib/richtext/RichText.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import SortHeader from '$lib/ui/SortHeader.svelte';
	import { driveStatus, pickFiles, shareByLink } from '$lib/drive';
	import { QTYPE_LABEL, type Overrides, type PoolInfo, type Question, type Quiz, type Session, type StudentQuestion } from '$lib/types';

	type Report = { imported: number; rejected: { row: number; code: string; errors: Record<string, string> }[] };
	const ready = requireRole('teacher');
	let { quizId }: { quizId: string } = $props();
	const id = $derived(quizId);
	let quiz = $state<Quiz | null>(null);
	let questions = $state<Question[]>([]);
	// A quiz's sessions, newest first, one page at a time (PL-FR-02).
	const sessions = new Paged<Session>(() => '/api/teacher/quizzes/' + id + '/sessions', 'sessions', { url: urlState, prefix: 'ses', sort: 'created', desc: true });
	let readiness = $state<{ blocking: string[]; warnings: string[] }>({ blocking: [], warnings: [] });
	let tab = $state<'questions' | 'import' | 'pools' | 'settings' | 'sessions' | 'preview'>('questions');
	const bank = $derived(quiz?.kind === 'bank');
	// Categories (D-66): a filter over the list, and suggestions in the editor.
	let filter = $state<string | null>(null);
	let bankCategories = $state<string[]>([]);
	const categories = $derived([...new Set(questions.map((q) => q.category).filter(Boolean))].sort((a, b) => a.localeCompare(b)));
	const suggestions = $derived([...new Set([...categories, ...bankCategories])].sort((a, b) => a.localeCompare(b)));
	const shown = $derived(filter === null ? questions : questions.filter((q) => q.category === filter));
	const uncategorised = $derived(questions.filter((q) => !q.category).length);
	let editing = $state<Question | 'new' | null>(null);
	let report = $state<Report | null>(null);
	let resReport = $state<Report | null>(null);
	let preview = $state<StudentQuestion[]>([]);
	let notice = $state('');
	let errors = $state<Record<string, string>>({});
	let listEl = $state<HTMLElement>();

	async function load() {
		quiz = await api.get<Quiz>('/api/teacher/quizzes/' + id);
		questions = (await api.get<{ questions: Question[] }>('/api/teacher/quizzes/' + id + '/questions')).questions ?? [];
		if (quiz.kind === 'bank') return;
		await sessions.load();
		readiness = await api.get('/api/teacher/quizzes/' + id + '/readiness');
		api.get<PoolInfo>('/api/teacher/quizzes/' + id + '/pools').then((p) => (bankCategories = p.bank.map((c) => c.category).filter(Boolean)), () => {});
	}
	$effect(() => {
		if (ready() && id) load();
	});
	$effect(() => {
		// Reordering needs the whole list, so it's off while a category filter is on.
		if (!listEl || tab !== 'questions' || filter !== null) return;
		const s = Sortable.create(listEl, {
			handle: '.handle',
			animation: 150,
			onEnd: async () => {
				const ids = [...listEl!.querySelectorAll<HTMLElement>('[data-id]')].map((el) => el.dataset.id!);
				await api.put('/api/teacher/quizzes/' + id + '/questions/order', { ids });
				load();
			}
		});
		return () => s.destroy();
	});

	async function setStatus(status: 'ready' | 'draft', acceptWarnings = false) {
		notice = '';
		try {
			quiz = await api.post<Quiz>('/api/teacher/quizzes/' + id + '/status', { status, accept_warnings: acceptWarnings });
		} catch (e) {
			if (e instanceof ApiError && e.code === 'quiz_has_warnings') {
if (await confirmDialog({ title: 'Some resource links are broken', body: e.message + ' Mark the quiz Ready anyway?', confirm: 'Mark Ready' })) return setStatus(status, true);
			} else notice = e instanceof ApiError ? e.message : 'Could not change status';
		}
	}
	async function upload(kind: 'questions' | 'resources', input: HTMLInputElement) {
		const f = input.files?.[0];
		if (!f) return;
		const form = new FormData();
		form.append('file', f);
		try {
			const r = await api.post<Report>('/api/teacher/quizzes/' + id + '/' + kind + '/import', form);
			if (kind === 'questions') report = r;
			else resReport = r;
			load();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Upload failed';
		}
		input.value = '';
	}
	// Google Drive (D-71): shown to teachers who connected it.
	let drive = $state(false);
	$effect(() => {
		driveStatus().then((s) => (drive = s.connected && !s.reconnect));
	});
	function driveFail(e: unknown) {
		notice = e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : e instanceof Error ? e.message : 'Google Drive didn’t answer';
	}
	async function importFromDrive() {
		try {
			const [f] = await pickFiles({ kind: 'questions', title: 'Choose a CSV file or a Google Sheet of questions' });
			if (!f) return;
			report = await api.post<Report>('/api/teacher/quizzes/' + id + '/questions/import-drive', { file_id: f.id });
			load();
		} catch (e) {
			driveFail(e);
		}
	}
	// Fills a question's image form with a picked Drive image, shared by link.
	async function imageFromDrive(e: MouseEvent) {
		const form = (e.currentTarget as HTMLButtonElement).form;
		try {
			const [f] = await pickFiles({ kind: 'images', title: 'Choose an image' });
			if (!f || !form) return;
			const shared = await shareByLink(f.id);
			(form.elements.namedItem('url') as HTMLInputElement).value = shared.image_url ?? shared.web_link;
			const alt = form.elements.namedItem('alt') as HTMLInputElement;
			if (!alt.value) alt.value = shared.name.replace(/\.[a-z0-9]+$/i, '');
			toast('Shared by link: students see it without a Google account. Add alt text, then Attach.', 'info');
		} catch (err) {
			driveFail(err);
		}
	}
	async function dup(q: Question) {
		await api.post('/api/teacher/questions/' + q.id + '/duplicate');
		load();
	}
	async function delQ(q: Question) {
if (!(await confirmDialog({ title: 'Delete question ' + q.code + '?', confirm: 'Delete', danger: true }))) return;
		await api.del('/api/teacher/questions/' + q.id);
		load();
	}
	async function addResource(q: Question, e: SubmitEvent) {
		e.preventDefault();
		// Kept now: the event's currentTarget is null once the request is awaited.
		const form = e.currentTarget as HTMLFormElement;
		const fd = new FormData(form);
		try {
			await api.put('/api/teacher/questions/' + q.id + '/resources', { role: fd.get('role'), n: Number(fd.get('n')), url: fd.get('url'), alt_text: fd.get('alt') });
			form.reset();
			load();
		} catch (err) {
			notice = err instanceof ApiError ? Object.values(err.fields).join('; ') || err.message : 'Could not add the resource';
		}
	}
	async function delResource(rid: string) {
		await api.del('/api/teacher/resources/' + rid);
		load();
	}
	async function checkLinks() {
		await api.post('/api/teacher/quizzes/' + id + '/resources/check');
		toast('Checking links in the background — refresh in a moment.', 'info');
	}
	async function saveSettings(o: Overrides) {
		errors = {};
		try {
			quiz = await api.patch<Quiz>('/api/teacher/quizzes/' + id, { settings: o });
		} catch (e) {
			if (e instanceof ApiError) errors = e.fields;
			throw e;
		}
	}
	async function rename() {
		const t = await promptDialog({ title: 'Rename quiz', label: 'Quiz title', value: quiz?.title ?? '', maxlength: 200 });
		if (t) quiz = await api.patch<Quiz>('/api/teacher/quizzes/' + id, { title: t });
	}
	async function delQuiz() {
if (!(await confirmDialog({ title: 'Delete this quiz?', body: 'If it has results it is archived instead, so the results stay.', confirm: 'Delete quiz', danger: true }))) return;
		const r = await api.del<{ archived: boolean }>('/api/teacher/quizzes/' + id);
		if (r?.archived) load();
		else goto('/t/topics/' + quiz!.topic_id);
	}
	async function newSession() {
		try {
			const s = await api.post<Session>('/api/teacher/quizzes/' + id + '/sessions', {});
			goto('/t/sessions/' + s.id);
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Could not create a session';
		}
	}
	async function loadPreview() {
		tab = 'preview';
		preview = (await api.get<{ questions: StudentQuestion[] }>('/api/teacher/quizzes/' + id + '/preview')).questions;
	}
</script>

<div class="page-container vstack">
	{#if quiz}
		{#if bank}
			<div class="row">
				<h1 style="margin:0">Question bank</h1>
			</div>
			<p class="muted small m-0">Your questions for any of your quizzes: give them categories, then draw from those categories in a quiz's <strong>Pools</strong>. {count(questions.length, 'question')} in {count(categories.length, 'category', 'categories')}.</p>
		{:else}
		<p class="small"><a href={'/t/topics/' + quiz.topic_id}>← Topic</a></p>
		<div class="row">
			<h1 style="margin:0">{quiz.title}</h1>
			<span class="badge badge-soft {quiz.status === 'ready' ? 'ok' : ''}">{quiz.status}</span>
			<span class="spacer"></span>
			<button class="btn btn-sm" onclick={rename}>Rename</button>
			{#if quiz.status === 'ready'}
				<button class="btn btn-primary" onclick={newSession}>Start a session</button>
				<button class="btn btn-sm" onclick={() => setStatus('draft')}>Back to draft</button>
			{:else}
				<button class="btn btn-primary" onclick={() => setStatus('ready')} disabled={readiness.blocking.length > 0}>Mark Ready</button>
			{/if}
		</div>
		<p class="muted small">{count(quiz.question_count, 'question')} · {count(quiz.total_marks, 'mark')}{#if quiz.effective?.quiz_time_limit_sec}{' · '}{Math.round(Number(quiz.effective.quiz_time_limit_sec) / 60)} min limit{/if}{#if quiz.pools?.length}{' · '}drawn from {count(quiz.pools.length, 'pool')}{:else if Number(quiz.effective?.question_draw) > 0 && Number(quiz.effective?.question_draw) < quiz.question_count}{' · '}each student gets {quiz.effective?.question_draw} at random{/if}</p>
		{/if}
		{#each readiness.blocking as b (b)}<p class="alert alert-soft alert-error small">{b}</p>{/each}
		{#each readiness.warnings as w (w)}<p class="alert alert-soft alert-warning small">{w}</p>{/each}
		{#if notice}<p class="alert alert-soft alert-warning">{notice}</p>{/if}

		<div class="tabs tabs-border tabs-scroll" role="tablist">
			<button class="tab" role="tab" aria-selected={tab === 'questions'} class:tab-active={tab === 'questions'} onclick={() => (tab = 'questions')}>Questions</button>
			<button class="tab" role="tab" aria-selected={tab === 'import'} class:tab-active={tab === 'import'} onclick={() => (tab = 'import')}>Import</button>
			{#if !bank}
				<button class="tab" role="tab" aria-selected={tab === 'pools'} class:tab-active={tab === 'pools'} onclick={() => (tab = 'pools')}>Pools{quiz.pools?.length ? ` (${quiz.pools.length})` : ''}</button>
				<button class="tab" role="tab" aria-selected={tab === 'settings'} class:tab-active={tab === 'settings'} onclick={() => (tab = 'settings')}>Settings</button>
				<button class="tab" role="tab" aria-selected={tab === 'sessions'} class:tab-active={tab === 'sessions'} onclick={() => (tab = 'sessions')}>Sessions ({sessions.total})</button>
			{/if}
			<button class="tab" role="tab" aria-selected={tab === 'preview'} class:tab-active={tab === 'preview'} onclick={loadPreview}>Preview</button>
		</div>

		{#if tab === 'questions'}
			{#if editing}
				{#key editing}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
						<QuestionEditor quizId={id} categories={suggestions} question={editing === 'new' ? null : editing} onsaved={(saved) => { toast(`Question ${saved.code} saved`); editing = null; load(); }} oncancel={() => (editing = null)} />
					</div>
				{/key}
			{:else}
				<button class="btn btn-primary" onclick={() => (editing = 'new')}>Add a question</button>
			{/if}
			{#if categories.length}
				<div class="flex flex-wrap items-center gap-1.5" role="group" aria-label="Show questions by category">
					<span class="small muted mr-1">Category</span>
					<button type="button" class="btn btn-xs" class:btn-primary={filter === null} aria-pressed={filter === null} onclick={() => (filter = null)}>All ({questions.length})</button>
					{#each categories as c (c)}
						<button type="button" class="btn btn-xs" class:btn-primary={filter === c} aria-pressed={filter === c} onclick={() => (filter = c)}>{c} ({questions.filter((q) => q.category === c).length})</button>
					{/each}
					{#if uncategorised}<button type="button" class="btn btn-xs" class:btn-primary={filter === ''} aria-pressed={filter === ''} onclick={() => (filter = '')}>No category ({uncategorised})</button>{/if}
				</div>
				{#if filter !== null}<p class="small muted m-0">Showing one category. Show all to drag questions into a new order.</p>{/if}
			{/if}
			<div bind:this={listEl} class="vstack">
				{#each shown as q (q.id)}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" data-id={q.id}>
						<div class="row">
							{#if filter === null}<span class="tooltip handle" data-tip="Drag to reorder" style="cursor:grab"><Icon name="menu" size={16} /></span>{/if}
							<strong>{q.code}</strong><span class="badge badge-soft">{QTYPE_LABEL[q.type]}</span>{#if q.category}<span class="badge badge-soft badge-primary">{q.category}</span>{/if}<span class="small muted">{count(q.marks, 'mark')}</span>
							{#if q.settings?.question_time_limit_sec}<span class="small muted">· {q.settings.question_time_limit_sec}s</span>{/if}
							<span class="spacer"></span>
							<button class="btn btn-sm" onclick={() => (editing = q)}><Icon name="pencil" size={14} />Edit</button>
							<RowMenu hint="More actions for {q.code}" actions={[
								{ label: 'Duplicate', icon: 'copy', onclick: () => dup(q) },
								{ label: 'Delete question', icon: 'trash', danger: true, onclick: () => delQ(q) }
							]} />
						</div>
						<RichText text={q.text} format={q.body.format} blanks="chip" />
						<p class="small muted" style="margin:0">Answer: {describeKey(q.type, q.body, q.key) || '(marked by LLM or manually)'}</p>
						<details>
							<summary class="small">Resources ({q.resources.length})</summary>
							{#each q.resources as r (r.id)}
								<div class="row small">
									<span>{q.code}_{r.role}_{r.n}</span>
									<a href={r.url} target="_blank" rel="noopener noreferrer">{r.alt_text || 'link'}</a>
									<span class="badge badge-soft {r.status === 'ok' ? 'ok' : r.status === 'broken' ? 'danger' : ''}">{r.status}</span>
									{#if r.message}<span class="muted">{r.message}</span>{/if}
									<button class="btn btn-sm" onclick={() => delResource(r.id)}>Remove</button>
								</div>
							{/each}
							<form class="row" onsubmit={(e) => addResource(q, e)} style="margin-top:0.5rem">
								<select class="select w-full" name="role" style="width:8rem" aria-label="Attach to"><option value="Q">Question</option><option value="O">Option</option><option value="F">Feedback</option></select>
								<input class="input w-full" name="n" type="number" min="1" value="1" style="width:5rem" aria-label="Number" />
								<input class="input w-full" name="url" placeholder="Public image URL (Google Drive links work)" required style="flex:1;min-width:12rem" aria-label="URL" />
								{#if drive}<button type="button" class="btn btn-sm" onclick={imageFromDrive}><Icon name="cloud" size={14} />From Drive</button>{/if}
								<input class="input w-full" name="alt" placeholder="Alt text" style="width:10rem" aria-label="Alt text" />
								<button class="btn btn-sm">Attach</button>
							</form>
						</details>
					</div>
				{/each}
			</div>
		{:else if tab === 'import'}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
				<h3 style="margin-top:0">Import questions</h3>
				<p class="small">One row per question. Download the <a href="/api/teacher/quiz-template.csv" download>template</a> for examples of all seven types. Add a <code>category</code> column to sort questions into pools. Valid rows are imported; invalid ones are listed below so you can fix and re-upload just those.</p>
				<input class="file-input w-full" type="file" accept=".csv,text/csv" onchange={(e) => upload('questions', e.currentTarget)} aria-label="Questions CSV" />
				{#if drive}<div><button class="btn" onclick={importFromDrive}><Icon name="cloud" size={16} />Import from Google Drive</button> <span class="small muted">A CSV file, or a Google Sheet with the same columns.</span></div>{/if}
				{#if report}
					<p class="alert alert-soft {report.rejected.length ? '' : 'ok'}">Imported {report.imported} question(s); rejected {report.rejected.length}.</p>
					{#if report.rejected.length}
						<div class="table-wrap"><table class="table"><thead><tr><th>Row</th><th>Code</th><th>Problems</th></tr></thead><tbody>
							{#each report.rejected as r (r.row)}<tr><td>{r.row}</td><td>{r.code}</td><td>{Object.entries(r.errors).map(([k, v]) => k + ': ' + v).join('; ')}</td></tr>{/each}
						</tbody></table></div>
					{/if}
				{/if}
			</div>
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
				<h3 style="margin-top:0">Attach images from a mapping CSV</h3>
				<p class="small">Columns <code>resource_name,url,alt_text</code>. Names follow <code>&lt;question_code&gt;_&lt;Q|O|F&gt;_&lt;n&gt;</code>, e.g. <code>Q001_Q_1</code> for the question image or <code>Q002_O_3</code> for option 3. Use publicly shared links.</p>
				<input class="file-input w-full" type="file" accept=".csv,text/csv" onchange={(e) => upload('resources', e.currentTarget)} aria-label="Resources CSV" />
				{#if resReport}
					<p class="alert alert-soft {resReport.rejected.length ? '' : 'ok'}">Attached {resReport.imported}; rejected {resReport.rejected.length}.</p>
					{#each resReport.rejected as r (r.row)}<p class="small">Row {r.row} ({r.code}): {Object.values(r.errors).join('; ')}</p>{/each}
				{/if}
				<div><button class="btn" onclick={checkLinks}>Re-check all links</button></div>
			</div>
		{:else if tab === 'pools'}
			<PoolsEditor quizId={id} onchange={load} />
		{:else if tab === 'settings'}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><SettingsEditor level="quiz" value={quiz.settings} effective={quiz.effective} onsave={saveSettings} {errors} /></div>
			<p class="small muted">Changes apply to new sessions; running sessions keep the settings they started with.</p>
			<div><button class="btn btn-error btn-outline" onclick={delQuiz}>Delete quiz</button></div>
		{:else if tab === 'sessions'}
			<ListSearch list={sessions} placeholder="Search title or join code" label="Search sessions" />
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
				<table class="table"><thead><tr><th><SortHeader list={sessions} key="title" label="Session" /></th><th>Code</th><th><SortHeader list={sessions} key="status" label="Status" /></th><th><SortHeader list={sessions} key="created" label="Started" desc /></th><th><span class="sr-only">Actions</span></th></tr></thead><tbody>
					{#each sessions.rows as s (s.id)}
						<tr>
							<td>{s.title}</td><td><strong>{s.join_code}</strong></td>
							<td><span class="badge badge-soft {s.status === 'live' ? 'ok' : ''}">{s.status}</span></td>
							<td class="small">{new Date(s.created_at).toLocaleString()}</td>
							<td class="row"><a class="btn btn-sm" href={'/t/sessions/' + s.id}>Dashboard</a><a class="btn btn-sm" href={'/t/sessions/' + s.id + '/results'}>Results</a></td>
						</tr>
					{:else}<tr><td colspan="5" class="muted">{sessions.q ? 'No sessions match.' : 'No sessions yet.'}</td></tr>{/each}
				</tbody></table>
			</div>
			<Pager list={sessions} label="Sessions" />
			{#if sessions.total}<a class="btn" href={'/t/quizzes/' + id + '/analytics'}>Quiz analytics across sessions</a>{/if}
		{:else}
			<p class="small muted">This is what students see. Answer keys are not included.</p>
			{#each preview as q, i (q.id)}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><p class="small muted">Question {i + 1}</p><QuestionView question={q} onchange={() => {}} /></div>
			{/each}
		{/if}
	{/if}
</div>
