<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { IDENTITY_LABEL, RESULTS_LABEL, TYPE_META } from '$lib/poll/meta';
	import PollQuestionEditor from '$lib/poll/PollQuestionEditor.svelte';
	import PollResults from '$lib/poll/PollResults.svelte';
	import PollSettingsForm from '$lib/poll/PollSettingsForm.svelte';
	import GroupsPanel from '$lib/poll/GroupsPanel.svelte';
	import LiveLinks from '$lib/LiveLinks.svelte';
	import Leaderboard from '$lib/poll/Leaderboard.svelte';
	import { fmtPoints, groupRanks, SCORABLE, settingsOf } from '$lib/poll/scoring';
	import type { Poll, PollQuestion, PollSettings, Rank, TeacherResults } from '$lib/poll/types';
	import RichText from '$lib/richtext/RichText.svelte';
	import { LiveSocket } from '$lib/socket';
	import type { Classroom } from '$lib/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import IconBtn from '$lib/ui/IconBtn.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import StatCounter from '$lib/ui/StatCounter.svelte';
	import { confirmDialog, promptDialog } from '$lib/ui/dialog.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';

	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let poll = $state<Poll | null>(null);
	let results = $state<TeacherResults | null>(null);
	let classrooms = $state<Classroom[]>([]);
	let tab = $state<'questions' | 'results' | 'groups' | 'settings' | 'share'>('questions');
	let editing = $state<PollQuestion | 'new' | null>(null);
	let settings = $state<PollSettings | null>(null);
	let classroomId = $state<string | null>(null);
	let connected = $state(false);
	let socket: LiveSocket | null = null;

	async function load() {
		poll = await api.get<Poll>('/api/teacher/polls/' + id);
		settings = settingsOf(poll);
		classroomId = poll.classroom_id;
		if (!poll.questions?.length) editing = 'new';
	}
	$effect(() => {
		if (!ready() || !id) return;
		load().catch(() => goto('/t/polls'));
		api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms').then((r) => (classrooms = r.classrooms ?? []));
		socket = new LiveSocket('/ws/teacher/polls/' + id);
		socket.onStatus = (c) => (connected = c);
		socket.onMessage = (m) => {
			if (m.type === 'results') {
				results = m as unknown as TeacherResults;
				if (poll) {
					poll.participants = results.participants;
					poll.status = results.poll.status;
				}
			} else if (m.type === 'deleted') goto('/t/polls');
		};
		socket.open();
		return () => socket?.close();
	});
	onDestroy(() => socket?.close());

	const questions = $derived(poll?.questions ?? []);

	async function setStatus(status: Poll['status']) {
		try {
			poll = await api.post<Poll>('/api/teacher/polls/' + id + '/status', { status });
			toast(status === 'open' ? 'Poll is open — share the code' : status === 'closed' ? 'Poll closed' : 'Back to draft');
			if (status === 'open') tab = 'share';
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not change the status', 'error');
		}
	}
	async function saveSettings() {
		if (!settings) return;
		try {
			poll = await api.put<Poll>('/api/teacher/polls/' + id, { settings, classroom_id: classroomId ?? '' });
			toast('Settings saved');
		} catch (e) {
			toast(e instanceof ApiError ? Object.values(e.fields)[0] ?? e.message : 'Could not save', 'error');
		}
	}
	async function rename() {
		const t = await promptDialog({ title: 'Rename poll', label: 'Poll title', value: poll?.title ?? '', maxlength: 200 });
		if (!t?.trim()) return;
		poll = await api.put<Poll>('/api/teacher/polls/' + id, { title: t });
	}
	async function delQ(q: PollQuestion) {
		if (!(await confirmDialog({ title: 'Delete this question?', body: 'Its answers are deleted too.', confirm: 'Delete', danger: true }))) return;
		await api.del('/api/teacher/poll-questions/' + q.id);
		toast('Question deleted');
		await load();
	}
	async function move(i: number, d: number) {
		const ids = questions.map((q) => q.id);
		const j = i + d;
		[ids[i], ids[j]] = [ids[j], ids[i]];
		await api.put('/api/teacher/polls/' + id + '/questions/order', { ids });
		await load();
	}
	async function reset() {
		if (!(await confirmDialog({ title: 'Clear all responses?', body: 'Every participant, answer and uploaded file is deleted. The questions stay.', confirm: 'Clear responses', danger: true }))) return;
		await api.post('/api/teacher/polls/' + id + '/reset');
		toast('Responses cleared');
		await load();
	}
	async function del() {
		if (!(await confirmDialog({ title: 'Delete this poll?', body: 'The questions, answers and files are deleted. This cannot be undone.', confirm: 'Delete poll', danger: true }))) return;
		await api.del('/api/teacher/polls/' + id);
		toast('Poll deleted');
		goto('/t/polls');
	}
	async function moderate(m: { question_id: string; participant_id?: string; word?: string; hidden: boolean }) {
		await api.post('/api/teacher/polls/' + id + '/moderation', m);
		toast(m.hidden ? 'Hidden from participants' : 'Shown again', 'info');
	}
	async function renameParticipant(r: Rank) {
		const n = await promptDialog({ title: 'Rename participant', body: 'Shown on the leaderboard. Leave it empty for "Participant N".', label: 'Nickname', value: r.nickname ?? '', allowEmpty: true, maxlength: 30 });
		if (n === null || !r.participant_id) return;
		await api.post('/api/teacher/polls/' + id + '/moderation', { participant_id: r.participant_id, nickname: n });
		toast('Renamed', 'info');
	}
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast('Link copied');
		} catch {
			toast('Copy failed; select the link instead', 'warning');
		}
	}
	const STATUS = { draft: ['Draft', ''], open: ['Open', 'ok'], closed: ['Closed', 'warn'] } as const;
	const answered = $derived(results ? Object.values(results.results).reduce((s, r) => s + r.responses, 0) : 0);
</script>

<div class="page-container vstack">
	{#if !poll || !settings}
		<Skeleton lines={5} />
	{:else}
		<p class="small m-0"><a href="/t/polls">← Polls</a></p>
		<div class="row">
			<h1 class="m-0">{poll.title}</h1>
			<span class="badge badge-soft gap-1 {STATUS[poll.status][1]}">{#if poll.status === 'open'}<span class="status status-success animate-pulse" aria-hidden="true"></span>{/if}{STATUS[poll.status][0]}</span>
			<button class="btn btn-ghost btn-xs" onclick={rename}>Rename</button>
			<span class="spacer"></span>
			{#if poll.status !== 'open'}
				<button class="btn btn-primary" onclick={() => setStatus('open')} disabled={!questions.length}><Icon name="play" size={16} />{poll.status === 'closed' ? 'Reopen' : 'Open poll'}</button>
			{:else}
				<button class="btn" onclick={() => setStatus('closed')}><Icon name="pause" size={16} />Close</button>
			{/if}
			<a class="btn" href={'/t/polls/' + id + '/present'} target="_blank" rel="noopener"><Icon name="monitor" size={16} />Present</a>
		</div>
		<p class="small muted m-0">{IDENTITY_LABEL[poll.identity]} · {poll.pacing === 'presenter' ? 'Presenter-led' : 'Self-paced'} · Results: {RESULTS_LABEL[poll.show_results].toLowerCase()}{poll.scoring ? ' · Scored' : ''} · Code <strong class="tabular">{poll.join_code}</strong></p>

		<div class="stats-grid" role="list" aria-label="Poll counts">
			<StatCounter label="Participants" value={poll.participants} icon="users" tone="primary" />
			<StatCounter label="Questions" value={questions.length} icon="menu" />
			<StatCounter label="Answers" value={answered} icon="check-circle" tone="success" />
		</div>

		<div class="tabs tabs-border tabs-scroll" role="tablist">
			{#each [['questions', `Questions (${questions.length})`], ['results', 'Live results'], ...(poll.groups !== 'off' ? [['groups', 'Groups']] : []), ['settings', 'Settings'], ['share', 'Share']] as [k, l] (k)}
				<button class="tab" role="tab" aria-selected={tab === k} class:tab-active={tab === k} onclick={() => (tab = k as typeof tab)}>{l}</button>
			{/each}
		</div>

		{#if tab === 'questions'}
			{#if editing}
				<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-6" in:flyIn>
					{#key editing}
						<PollQuestionEditor pollId={id} question={editing === 'new' ? null : editing} scoring={poll.scoring} pacing={poll.pacing} onsaved={async () => { editing = null; await load(); }} oncancel={() => (editing = null)} />
					{/key}
				</div>
			{:else}
				<div><button class="btn btn-primary" onclick={() => (editing = 'new')}><Icon name="plus" size={16} />Add a question</button></div>
			{/if}
			{#each questions as q, i (q.id)}
				<div class="card card-border bg-base-100 p-4 shadow-sm" in:flyIn={{ delay: Math.min(i, 6) * 30 }}>
					<div class="flex flex-wrap items-center gap-2">
						<span class="q-num">{i + 1}</span>
						<span class="badge badge-soft badge-primary gap-1"><Icon name={TYPE_META[q.type].icon} size={12} />{TYPE_META[q.type].label}</span>
						{#if q.required}<span class="badge badge-soft badge-sm">required</span>{/if}
						{#if poll.scoring && q.key}<span class="badge badge-soft badge-success badge-sm gap-1"><Icon name="check" size={11} />{fmtPoints(q.points ?? 100)} pts</span>
						{:else if poll.scoring && SCORABLE.has(q.type)}<span class="tooltip" data-tip="Edit the question to set its correct answer"><span class="badge badge-soft badge-warning badge-sm">no answer key</span></span>{/if}
						{#if q.time_limit_sec && poll.pacing === 'presenter'}<span class="badge badge-soft badge-sm tabular">{q.time_limit_sec}s</span>{/if}
						<span class="small muted tabular">{results?.results[q.id]?.responses ?? 0} answers</span>
						<span class="spacer"></span>
						<IconBtn icon="arrow-up" label="Up" hint="Move question {i + 1} up" disabled={i === 0} onclick={() => move(i, -1)} />
						<IconBtn icon="arrow-down" label="Down" hint="Move question {i + 1} down" disabled={i === questions.length - 1} onclick={() => move(i, 1)} />
						<button class="btn btn-sm" onclick={() => (editing = q)}>Edit</button>
						<button class="btn btn-sm btn-ghost text-error" onclick={() => delQ(q)}>Delete</button>
					</div>
					<RichText text={q.text} format={q.body.format} blanks="chip" class="mt-2" />
				</div>
			{:else}
				{#if !editing}<div class="card card-border bg-base-100"><EmptyState icon="menu" title="No questions yet">Add one to open the poll.</EmptyState></div>{/if}
			{/each}
		{:else if tab === 'results'}
			<div class="row small">
				<span class="badge badge-soft gap-1" class:ok={connected}>{#if connected}<span class="status status-success animate-pulse" aria-hidden="true"></span>Live{:else}<Icon name="wifi-off" size={12} />Reconnecting…{/if}</span>
				<span class="spacer"></span>
				<a class="btn btn-sm" href={'/api/teacher/polls/' + id + '/export.csv'} download><Icon name="arrow-right" size={14} />Export CSV</a>
				<button class="btn btn-sm btn-ghost text-error" onclick={reset}>Clear responses</button>
			</div>
			{#if poll.scoring}
				<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6">
					<div class="lb-grid" class:two={!!results?.group_leaderboard?.length}>
						{#if results?.group_leaderboard?.length}
							<div>
								<h2 class="m-0 mb-3 flex items-center gap-2 text-lg"><Icon name="users" size={18} />Groups</h2>
								<Leaderboard ranks={groupRanks(results.group_leaderboard)} limit={50} />
							</div>
						{/if}
						<div>
							<h2 class="m-0 mb-3 flex items-center gap-2 text-lg"><Icon name="trophy" size={18} />{results?.group_leaderboard?.length ? 'Individuals' : 'Leaderboard'}</h2>
							<Leaderboard ranks={results?.leaderboard ?? []} teacher limit={50} onrename={renameParticipant} />
						</div>
					</div>
				</section>
			{/if}
			{#each questions as q, i (q.id)}
				<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6">
					<p class="small muted m-0">Question {i + 1} · {TYPE_META[q.type].label}</p>
					<RichText text={q.text} format={q.body.format} blanks="chip" class="mb-3 font-semibold" />
					<PollResults question={q} result={results?.results[q.id]} teacher pollId={id} onmoderate={moderate} />
				</section>
			{:else}
				<div class="card card-border bg-base-100"><EmptyState icon="chart" title="Nothing to show yet">Add questions and open the poll.</EmptyState></div>
			{/each}
		{:else if tab === 'groups'}
			<GroupsPanel pollId={id} settings={settingsOf(poll)} hasClassroom={!!poll.classroom_id} participants={poll.participants} />
		{:else if tab === 'settings'}
			<div class="card card-border vstack bg-base-100 p-4 shadow-sm sm:p-6">
				<PollSettingsForm bind:settings bind:classroomId {classrooms} locked={poll.participants > 0} />
				<div class="flex flex-wrap gap-2">
					<button class="btn btn-primary" onclick={saveSettings}>Save settings</button>
					<span class="spacer"></span>
					{#if poll.status !== 'draft'}<button class="btn btn-ghost" onclick={() => setStatus('draft')}>Back to draft</button>{/if}
					<button class="btn btn-error btn-outline" onclick={del}>Delete poll</button>
				</div>
			</div>
		{:else}
			<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-6">
				{#if poll.status !== 'open'}<div class="alert alert-soft alert-warning mb-4"><Icon name="info" />The poll is {poll.status}; open it so people can join.</div>{/if}
				<div class="share">
					<div class="qr"><QrCode text={poll.join_url} size={220} /></div>
					<div class="vstack min-w-0">
						<p class="small muted m-0">Join code</p>
						<p class="code tabular m-0">{poll.join_code}</p>
						<p class="small muted m-0">or open</p>
						<div class="join w-full">
							<input class="input input-sm join-item w-full" readonly value={poll.join_url} aria-label="Join link" onfocus={(e) => e.currentTarget.select()} />
							<button class="btn btn-sm join-item" onclick={() => copy(poll!.join_url)}>Copy</button>
						</div>
						<p class="small muted m-0">People can also type the code at <strong>/join</strong>. {poll.identity === 'identified' ? 'They will be asked to log in.' : poll.identity === 'optional' ? 'Logging in is optional.' : 'No login is needed.'}</p>
						<a class="btn btn-primary w-fit" href={'/t/polls/' + id + '/present'} target="_blank" rel="noopener"><Icon name="monitor" size={16} />Open the presenter screen</a>
					</div>
				</div>
			</div>
			{#if poll.scoring}
				<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-6"><LiveLinks scope="live_poll" targetId={id} teams={poll.groups !== 'off'} /></div>
			{:else}
				<p class="small muted m-0">Turn on <strong>Score answers</strong> in Settings to share a live leaderboard.</p>
			{/if}
		{/if}
	{/if}
</div>

<style>
	.lb-grid { display: grid; gap: 1.5rem; }
	@media (min-width: 900px) { .lb-grid.two { grid-template-columns: 1fr 1fr; } }
	.stats-grid { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr)); }
	.q-num { width: 1.75rem; height: 1.75rem; border-radius: 999px; display: grid; place-items: center; font-weight: 700; font-size: 0.85rem; background: var(--color-base-200); }
	.share { display: grid; gap: 1.5rem; grid-template-columns: auto minmax(0, 1fr); align-items: center; }
	@media (max-width: 640px) { .share { grid-template-columns: minmax(0, 1fr); justify-items: center; text-align: center; } }
	.qr { background: #fff; padding: 0.75rem; border-radius: var(--radius-box); }
	.code { font-size: 2.5rem; font-weight: 800; letter-spacing: 0.12em; }
</style>
