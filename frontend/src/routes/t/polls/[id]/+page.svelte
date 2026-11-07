<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	import AnswerList from '$lib/poll/AnswerList.svelte';
	import SaveToDrive from '$lib/drive/SaveToDrive.svelte';
	import ParticipantList from '$lib/poll/ParticipantList.svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import ToonPage from '$lib/poll/ToonPage.svelte';
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
	let tab = $state<'questions' | 'results' | 'groups' | 'settings' | 'share' | 'people'>('questions');
	let editing = $state<PollQuestion | 'new' | null>(null);
	let settings = $state<PollSettings | null>(null);
	let classroomId = $state<string | null>(null);
	let topicId = $state<string | null>(null);
	let connected = $state(false);
	let socket: LiveSocket | null = null;

	async function load() {
		poll = await api.get<Poll>('/api/teacher/polls/' + id);
		settings = settingsOf(poll);
		classroomId = poll.classroom_id;
		topicId = poll.topic_id;
		if (!poll.questions?.length) editing = 'new';
	}
	$effect(() => {
		if (!ready() || !id) return;
		load().catch(() => goto('/t/polls'));
		api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => (classrooms = r.classrooms ?? []));
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
			toast(status === 'open' ? tr("Poll is open — share the code") : status === 'closed' ? tr("Poll closed") : tr("Back to draft"));
			if (status === 'open') tab = 'share';
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("Could not change the status"), 'error');
		}
	}
	async function saveSettings() {
		if (!settings) return;
		try {
			poll = await api.put<Poll>('/api/teacher/polls/' + id, { settings, classroom_id: classroomId ?? '', topic_id: (classroomId && topicId) || '' });
			toast(tr("Settings saved"));
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : tr("Could not save"), 'error');
		}
	}
	async function rename() {
		const t = await promptDialog({ title: tr("Rename poll"), label: tr("Poll title"), value: poll?.title ?? '', maxlength: 200 });
		if (!t?.trim()) return;
		poll = await api.put<Poll>('/api/teacher/polls/' + id, { title: t });
	}
	async function delQ(q: PollQuestion) {
		if (!(await confirmDialog({ title: tr("Delete this question?"), body: tr("Its answers are deleted too."), confirm: tr("Delete"), danger: true }))) return;
		await api.del('/api/teacher/poll-questions/' + q.id);
		toast(tr("Question deleted"));
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
		if (!(await confirmDialog({ title: tr("Clear all responses?"), body: tr("Every participant, answer and uploaded file is deleted. The questions stay."), confirm: tr("Clear responses"), danger: true }))) return;
		await api.post('/api/teacher/polls/' + id + '/reset');
		toast(tr("Responses cleared"));
		await load();
	}
	async function del() {
		if (!(await confirmDialog({ title: tr("Delete this poll?"), body: tr("The questions, answers and files are deleted. This cannot be undone."), confirm: tr("Delete poll"), danger: true }))) return;
		await api.del('/api/teacher/polls/' + id);
		toast(tr("Poll deleted"));
		goto('/t/polls');
	}
	async function moderate(m: { question_id: string; participant_id?: string; word?: string; hidden: boolean }): Promise<void> {
		await api.post('/api/teacher/polls/' + id + '/moderation', m);
		toast(m.hidden ? tr("Hidden from participants") : tr("Shown again"), 'info');
	}
	async function renameParticipant(r: Rank) {
		const n = await promptDialog({ title: tr("Rename participant"), body: tr("Shown on the leaderboard. Leave it empty for \"Participant N\"."), label: tr("Nickname"), value: r.nickname ?? '', allowEmpty: true, maxlength: 30 });
		if (n === null || !r.participant_id) return;
		await api.post('/api/teacher/polls/' + id + '/moderation', { participant_id: r.participant_id, nickname: n });
		toast(tr("Renamed"), 'info');
	}
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast(tr("Link copied"));
		} catch {
			toast(tr("Copy failed; select the link instead"), 'warning');
		}
	}
	const STATUS = { draft: ['Draft', ''], open: ['Open', 'ok'], closed: ['Closed', 'warn'] } as const;
	const answered = $derived(results ? Object.values(results.results).reduce((s, r) => s + r.responses, 0) : 0);
</script>

<ToonPage>
<div class="page-container vstack">
	{#if !poll || !settings}
		<Skeleton lines={5} />
	{:else}
		<p class="small m-0"><a href="/t/polls">{tr("← Polls")}</a></p>
		<div class="row">
			<h1 class="m-0">{poll.title}</h1>
			<span class="badge badge-soft gap-1 {STATUS[poll.status][1]}">{#if poll.status === 'open'}<span class="status status-success animate-pulse" aria-hidden="true"></span>{/if}{STATUS[poll.status][0]}</span>
			<button class="btn btn-ghost btn-xs" onclick={rename}>{tr("Rename")}</button>
			<span class="spacer"></span>
			{#if poll.status !== 'open'}
				<button class="btn btn-primary" onclick={() => setStatus('open')} disabled={!questions.length}><Icon name="play" size={16} />{poll.status === 'closed' ? tr("Reopen") : tr("Open poll")}</button>
			{:else}
				<button class="btn" onclick={() => setStatus('closed')}><Icon name="pause" size={16} />{tr("Close")}</button>
			{/if}
			<a class="btn" href={'/t/polls/' + id + '/present'} target="_blank" rel="noopener"><Icon name="monitor" size={16} />{tr("Present")}</a>
		</div>
		<p class="small muted m-0">{tr("{0} · {1} · Results: {2}{3} · Code", [IDENTITY_LABEL[poll.identity], poll.pacing === 'presenter' ? tr("Presenter-led") : tr("Self-paced"), RESULTS_LABEL[poll.show_results].toLowerCase(), poll.scoring ? tr(" · Scored") : ''])} <strong class="tabular">{poll.join_code}</strong></p>

		<div class="stats-grid" role="list" aria-label={tr("Poll counts")}>
			<StatCounter label={tr("Participants")} value={poll.participants} icon="users" tone="primary" />
			<StatCounter label={tr("Questions")} value={questions.length} icon="menu" />
			<StatCounter label={tr("Answers")} value={answered} icon="check-circle" tone="success" />
		</div>

		<div class="tabs tabs-border tabs-scroll" role="tablist">
			{#each [['questions', `Questions (${questions.length})`], ['results', 'Live results'], ['people', `Participants (${poll.participants})`], ...(poll.groups !== 'off' ? [['groups', 'Groups']] : []), ['settings', 'Settings'], ['share', 'Share']] as [k, l] (k)}
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
				<div><button class="btn btn-primary" onclick={() => (editing = 'new')}><Icon name="plus" size={16} />{tr("Add a question")}</button></div>
			{/if}
			{#each questions as q, i (q.id)}
				<div class="card card-border bg-base-100 p-4 shadow-sm" in:flyIn={{ delay: Math.min(i, 6) * 30 }}>
					<div class="flex flex-wrap items-center gap-2">
						<span class="q-num">{i + 1}</span>
						<span class="badge badge-soft badge-primary gap-1"><Icon name={TYPE_META[q.type].icon} size={12} />{tr(TYPE_META[q.type].label)}</span>
						{#if q.required}<span class="badge badge-soft badge-sm">{tr("required")}</span>{/if}
						{#if poll.scoring && q.key}<span class="badge badge-soft badge-success badge-sm gap-1"><Icon name="check" size={11} />{tr("{0} pts", [fmtPoints(q.points ?? 100)])}</span>
						{:else if poll.scoring && SCORABLE.has(q.type)}<span class="tooltip" data-tip={tr("Edit the question to set its correct answer")}><span class="badge badge-soft badge-warning badge-sm">{tr("no answer key")}</span></span>{/if}
						{#if q.time_limit_sec && poll.pacing === 'presenter'}<span class="badge badge-soft badge-sm tabular">{q.time_limit_sec}s</span>{/if}
						<span class="small muted tabular">{tr("{0} answers", [results?.results[q.id]?.responses ?? 0])}</span>
						<span class="spacer"></span>
						<IconBtn icon="arrow-up" label={tr("Up")} hint={tr("Move question {0} up", [i + 1])} disabled={i === 0} onclick={() => move(i, -1)} />
						<IconBtn icon="arrow-down" label={tr("Down")} hint={tr("Move question {0} down", [i + 1])} disabled={i === questions.length - 1} onclick={() => move(i, 1)} />
						<button class="btn btn-sm" onclick={() => (editing = q)}>{tr("Edit")}</button>
						<button class="btn btn-sm btn-ghost text-error" onclick={() => delQ(q)}>{tr("Delete")}</button>
					</div>
					<RichText text={q.text} format={q.body.format} blanks="chip" class="mt-2" />
				</div>
			{:else}
				{#if !editing}<div class="card card-border bg-base-100"><EmptyState icon="menu" title={tr("No questions yet")}>{tr("Add one to open the poll.")}</EmptyState></div>{/if}
			{/each}
		{:else if tab === 'results'}
			<div class="row small">
				<span class="badge badge-soft gap-1" class:ok={connected}>{#if connected}<span class="status status-success animate-pulse" aria-hidden="true"></span>{tr("Live")}{:else}<Icon name="wifi-off" size={12} />{tr("Reconnecting…")}{/if}</span>
				<span class="spacer"></span>
				<a class="btn btn-sm" href={'/api/teacher/polls/' + id + '/export.csv'} download><Icon name="arrow-right" size={14} />{tr("Export CSV")}</a>
				<SaveToDrive kind="poll" {id} size="btn-sm" />
				<button class="btn btn-sm btn-ghost text-error" onclick={reset}>{tr("Clear responses")}</button>
			</div>
			{#if poll.scoring}
				<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6">
					<div class="lb-grid" class:two={!!results?.group_leaderboard?.length}>
						{#if results?.group_leaderboard?.length}
							<div>
								<h2 class="m-0 mb-3 flex items-center gap-2 text-lg"><Icon name="users" size={18} />{tr("Groups")}</h2>
								<Leaderboard ranks={groupRanks(results.group_leaderboard)} limit={50} />
							</div>
						{/if}
						<div>
							<h2 class="m-0 mb-3 flex items-center gap-2 text-lg"><Icon name="trophy" size={18} />{results?.group_leaderboard?.length ? tr("Individuals") : tr("Leaderboard")}</h2>
							<Leaderboard ranks={results?.leaderboard ?? []} teacher limit={50} onrename={renameParticipant} />
						</div>
					</div>
				</section>
			{/if}
			{#each questions as q, i (q.id)}
				<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6">
					<p class="small muted m-0">{tr("Question {0} · {1}", [i + 1, tr(TYPE_META[q.type].label)])}</p>
					<RichText text={q.text} format={q.body.format} blanks="chip" class="mb-3 font-semibold" />
					<PollResults question={q} result={results?.results[q.id]} teacher toon pollId={id} onmoderate={moderate} />
					<AnswerList pollId={id} questionId={q.id} onmoderate={moderate} />
				</section>
			{:else}
				<div class="card card-border bg-base-100"><EmptyState icon="chart" title={tr("Nothing to show yet")}>{tr("Add questions and open the poll.")}</EmptyState></div>
			{/each}
		{:else if tab === 'people'}
			<ParticipantList pollId={id} scoring={poll.scoring} groups={poll.groups !== 'off'} />
		{:else if tab === 'groups'}
			<GroupsPanel pollId={id} settings={settingsOf(poll)} hasClassroom={!!poll.classroom_id} participants={poll.participants} />
		{:else if tab === 'settings'}
			<div class="card card-border vstack bg-base-100 p-4 shadow-sm sm:p-6">
				<PollSettingsForm bind:settings bind:classroomId bind:topicId {classrooms} locked={poll.participants > 0} />
				<div class="flex flex-wrap gap-2">
					<button class="btn btn-primary" onclick={saveSettings}>{tr("Save settings")}</button>
					<span class="spacer"></span>
					{#if poll.status !== 'draft'}<button class="btn btn-ghost" onclick={() => setStatus('draft')}>{tr("Back to draft")}</button>{/if}
					<button class="btn btn-error btn-outline" onclick={del}>{tr("Delete poll")}</button>
				</div>
			</div>
		{:else}
			<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-6">
				{#if poll.status !== 'open'}<div class="alert alert-soft alert-warning mb-4"><Icon name="info" />{tr("The poll is {0}; open it so people can join.", [poll.status])}</div>{/if}
				<div class="share">
					<div class="qr"><QrCode text={poll.join_url} size={220} /></div>
					<div class="vstack min-w-0">
						<p class="small muted m-0">{tr("Join code")}</p>
						<p class="code tabular m-0">{poll.join_code}</p>
						<p class="small muted m-0">{tr("or open")}</p>
						<div class="join w-full">
							<input class="input input-sm join-item w-full" readonly value={poll.join_url} aria-label={tr("Join link")} onfocus={(e) => e.currentTarget.select()} />
							<button class="btn btn-sm join-item" onclick={() => copy(poll!.join_url)}>{tr("Copy")}</button>
						</div>
						<p class="small muted m-0">{tr("People can also type the code at")} <strong>{tr("/join")}</strong>. {poll.identity === 'identified' ? tr("They will be asked to log in.") : poll.identity === 'optional' ? tr("Logging in is optional.") : tr("No login is needed.")}</p>
						<a class="btn btn-primary w-fit" href={'/t/polls/' + id + '/present'} target="_blank" rel="noopener"><Icon name="monitor" size={16} />{tr("Open the presenter screen")}</a>
					</div>
				</div>
			</div>
			{#if poll.scoring}
				<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-6"><LiveLinks scope="live_poll" targetId={id} teams={poll.groups !== 'off'} /></div>
			{:else}
				<p class="small muted m-0">{tr("Turn on")} <strong>{tr("Score answers")}</strong> {tr("in Settings to share a live leaderboard.")}</p>
			{/if}
		{/if}
	{/if}
</div>
</ToonPage>

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
