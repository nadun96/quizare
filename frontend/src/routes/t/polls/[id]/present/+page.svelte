<script lang="ts">
	// Presenter screen for a poll (D-40): big, live results for the room.
	// Keys: ← → question, R reveal (presenter-led), A answer and L leaderboard
	// (scored polls, D-42), N add a question while live (V2-05), B whiteboard
	// (V2-09), Q code, F full screen.
	import { onDestroy } from 'svelte';
	import { Tween } from 'svelte/motion';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { TYPE_META } from '$lib/poll/meta';
	import Leaderboard from '$lib/poll/Leaderboard.svelte';
	import PollQuestionEditor from '$lib/poll/PollQuestionEditor.svelte';
	import Whiteboard from '$lib/board/Whiteboard.svelte';
	import { BoardState, type BoardAccess, type BoardEvent } from '$lib/board/strokes.svelte';
	import { teacherBoard } from '$lib/board/teacher';
	import type { GroupsView } from '$lib/poll/types';
	import PollResults from '$lib/poll/PollResults.svelte';
	import '@fontsource-variable/fredoka';
	import '$lib/poll/toon.css';
	import { toonVars } from '$lib/poll/toon';
	import { groupRanks, secondsLeft } from '$lib/poll/scoring';
	import type { Poll, TeacherResults } from '$lib/poll/types';
	import RichText from '$lib/richtext/RichText.svelte';
	import { LiveSocket } from '$lib/socket';
	import Icon from '$lib/ui/Icon.svelte';
	import IconBtn from '$lib/ui/IconBtn.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn, reduced } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';

	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let data = $state<TeacherResults | null>(null);
	let index = $state(0); // self-paced: what this screen shows
	let showCode = $state(true);
	let board = $state(false); // leaderboard instead of the question
	let showKey = $state(false); // self-paced or other modes: this screen only
	// Adding a question while live (V2-05).
	let addDialog = $state<HTMLDialogElement>();
	let adding = $state(false);
	let addNext = $state(true);
	let addShow = $state(true);
	let jumpTo = $state('');
	// Whiteboard (V2-09).
	let showBoard = $state(false);
	const wb = new BoardState();
	const tb = $derived(teacherBoard(id));
	let access = $state<BoardAccess>({ open: false, mode: 'teacher', groups: [], participants: [] });
	let people = $state<GroupsView | null>(null);
	async function loadBoard() {
		wb.fetching();
		const v = await tb.view();
		wb.load(v);
		if (v.access) access = v.access;
	}
	async function toggleBoard() {
		showBoard = !showBoard;
		if (showBoard) {
			showCode = board = false;
			await loadBoard().catch(() => toast('Could not load the board', 'error'));
		}
	}
	async function saveAccess(patch: Partial<BoardAccess>) {
		try {
			access = await tb.access({ ...access, ...patch });
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not save', 'error');
		}
	}
	async function loadPeople() {
		people = await api.get<GroupsView>('/api/teacher/polls/' + id + '/groups').catch(() => null);
	}
	$effect(() => {
		if (showBoard && access.mode === 'selected' && !people) loadPeople();
	});
	const toggleIn = (list: string[], x: string) => (list.includes(x) ? list.filter((y) => y !== x) : [...list, x]);
	let clockOffset = 0;
	let now = $state(Date.now());
	let connected = $state(false);
	let socket: LiveSocket | null = null;

	$effect(() => {
		if (!ready() || !id) return;
		socket = new LiveSocket('/ws/teacher/polls/' + id);
		let wasOpen = false;
		socket.onStatus = (c) => {
			connected = c;
			// Board events missed while disconnected: reload the board (D-52).
			if (c && wasOpen && showBoard) loadBoard().catch(() => {});
			wasOpen ||= c;
		};
		socket.onMessage = (m) => {
			if (m.type === 'board') {
				if (wb.apply(m as unknown as BoardEvent) && showBoard) loadBoard().catch(() => {});
				return;
			}
			if (m.type !== 'results') return;
			const first = !data;
			data = m as unknown as TeacherResults;
			clockOffset = data.server_time - Date.now();
			if (first) {
				index = data.poll.pacing === 'presenter' ? data.poll.current_index : 0;
				showCode = data.participants === 0;
			}
		};
		socket.open();
		return () => socket?.close();
	});
	onDestroy(() => socket?.close());

	const poll = $derived(data?.poll ?? null);
	const questions = $derived(poll?.questions ?? []);
	const presenter = $derived(poll?.pacing === 'presenter');
	const cur = $derived(presenter ? (poll?.current_index ?? 0) : index);
	const q = $derived(questions[cur]);
	// Self-paced: when a question is inserted before the one on screen, keep
	// showing the same question.
	let shownId = '';
	$effect(() => {
		if (presenter || !q) return;
		if (shownId && q.id !== shownId && !jumpTo) {
			const i = questions.findIndex((x) => x.id === shownId);
			if (i >= 0 && i !== index) {
				index = i;
				return;
			}
		}
		shownId = q.id;
	});
	const joined = new Tween(0, { duration: 500 });
	$effect(() => {
		joined.set(data?.participants ?? 0, reduced() ? { duration: 0 } : undefined);
	});

	async function go(i: number, revealed = false, answersRevealed = false) {
		if (!poll || i < 0 || i >= questions.length) return;
		if (i !== cur) showKey = false;
		if (!presenter) {
			index = i;
			shownId = questions[i].id; // moved on purpose, so the check for inserted questions leaves it
			return;
		}
		try {
			const p = await api.post<Poll>('/api/teacher/polls/' + id + '/present', { index: i, revealed, answers_revealed: answersRevealed });
			if (data) data.poll = { ...data.poll, current_index: p.current_index, revealed: p.revealed, answers_revealed: p.answers_revealed, question_started_at: p.question_started_at };
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not move on', 'error');
		}
	}
	const reveal = () => poll && go(cur, !poll.revealed, poll.answers_revealed);
	// "When I reveal" tells participants too; otherwise only this screen shows it.
	const sharedReveal = $derived(presenter && poll?.show_answers === 'presenter');
	const answerShown = $derived(!!poll && (sharedReveal ? poll.answers_revealed : showKey));
	function toggleAnswer() {
		if (!poll || !q?.key) return;
		if (sharedReveal) go(cur, poll.revealed, !poll.answers_revealed);
		else showKey = !showKey;
	}
	const startedAt = $derived(poll?.question_started_at ? Date.parse(poll.question_started_at) : undefined);
	const secs = $derived(presenter && q ? secondsLeft(startedAt, q.time_limit_sec, now + clockOffset) : null);
	$effect(() => {
		if (!presenter || !q?.time_limit_sec) return;
		const t = setInterval(() => (now = Date.now()), 250);
		return () => clearInterval(t);
	});
	async function openPoll() {
		try {
			await api.post('/api/teacher/polls/' + id + '/status', { status: 'open' });
			toast('Poll is open');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not open', 'error');
		}
	}
	function openAdd() {
		adding = true;
		queueMicrotask(() => addDialog?.showModal());
	}
	function closeAdd() {
		addDialog?.close();
		adding = false;
	}
	function added(q: { id: string }) {
		closeAdd();
		showCode = board = false;
		if (addShow) jumpTo = q.id; // self-paced: this screen moves once the new question arrives
		toast(addShow && presenter ? 'Question added and shown to everyone' : 'Question added');
	}
	$effect(() => {
		if (!jumpTo) return;
		const i = questions.findIndex((x) => x.id === jumpTo);
		if (i < 0) return;
		if (!presenter) {
			index = i;
			shownId = jumpTo;
		}
		jumpTo = '';
	});
	function key(e: KeyboardEvent) {
		if (adding || (e.target as HTMLElement)?.closest('input, textarea, select, [contenteditable], dialog')) return;
		if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') (e.preventDefault(), go(cur + 1));
		else if (e.key === 'ArrowLeft' || e.key === 'PageUp') (e.preventDefault(), go(cur - 1));
		else if (e.key.toLowerCase() === 'r' && presenter) reveal();
		else if (e.key.toLowerCase() === 'a' && poll?.scoring) toggleAnswer();
		else if (e.key.toLowerCase() === 'l' && poll?.scoring) board = !board;
		else if (e.key.toLowerCase() === 'q') showCode = !showCode;
		else if (e.key.toLowerCase() === 'n') (e.preventDefault(), openAdd());
		else if (e.key.toLowerCase() === 'b') toggleBoard();
		else if (e.key.toLowerCase() === 'f') document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen?.().catch(() => {});
	}
	const host = $derived(poll ? poll.join_url.replace(/^https?:\/\//, '').replace(/\/p\/.*$/, '') : '');
</script>

<svelte:window onkeydown={key} />
<svelte:head><title>{poll ? poll.title + ' · Presenting' : 'Presenting'}</title></svelte:head>

<div class="stage toon" data-theme="quiz" style={toonVars}>
	<svg class="sky" viewBox="0 0 1200 500" preserveAspectRatio="xMidYMin slice" aria-hidden="true">
		<path d="M120 150a45 45 0 0 1 8-89 70 70 0 0 1 132-14 58 58 0 0 1 70 56 47 47 0 0 1-8 47z" />
		<path d="M880 110a36 36 0 0 1 6-71 56 56 0 0 1 106-11 46 46 0 0 1 56 45 38 38 0 0 1-6 37z" />
		<path d="M1010 380a30 30 0 0 1 5-60 47 47 0 0 1 89-9 39 39 0 0 1 47 38 32 32 0 0 1-5 31z" />
		<path d="M30 420a30 30 0 0 1 5-60 47 47 0 0 1 89-9 39 39 0 0 1 47 38 32 32 0 0 1-5 31z" />
	</svg>
	{#if !poll}
		<div class="p-8"><Skeleton lines={5} /></div>
	{:else}
		<header class="bar">
			<strong class="truncate text-lg">{poll.title}</strong>
			<span class="spacer"></span>
			<span class="join-hint">Join at <strong>{host}/join</strong> with <strong class="tabular code-inline">{poll.join_code}</strong></span>
			<span class="badge badge-soft badge-lg gap-1 tabular" aria-live="polite"><Icon name="users" size={16} />{Math.round(joined.current)}</span>
			{#if !connected}<span class="badge badge-soft badge-warning gap-1"><Icon name="wifi-off" size={14} />Reconnecting</span>{/if}
			<IconBtn icon="qr" label="Join code" hint="Show the join code (Q)" size={18} tip="left" onclick={() => (showCode = !showCode)} />
		</header>

		{#if poll.status !== 'open'}
			<div class="alert alert-soft alert-warning mx-auto mt-4 max-w-3xl">
				<Icon name="info" /><span>The poll is {poll.status}. Participants can't answer until it's open.</span>
				<button class="btn btn-sm btn-primary" onclick={openPoll}>Open now</button>
			</div>
		{/if}

		<div class="layout" class:wide={!showCode && !showBoard}>
		{#if !showCode && !showBoard}
			<aside class="toon-panel join-card" aria-label="How to join">
				<strong class="side-title">Join</strong>
				<div class="mini-qr"><QrCode text={poll.join_url} size={150} /></div>
				<p class="mini-code tabular m-0">{poll.join_code}</p>
				<p class="small m-0">Go to <strong>{host}/join</strong> or scan the code</p>
			</aside>
		{/if}
		<main class="main">
			{#if showCode}
				<section class="lobby" in:flyIn>
					<div class="qr-box"><QrCode text={poll.join_url} size={320} /></div>
					<div>
						<p class="muted m-0 text-xl">Go to <strong>{host}/join</strong> and enter</p>
						<p class="big-code tabular m-0">{poll.join_code}</p>
						<p class="muted m-0 text-xl">{poll.identity === 'identified' ? 'Log in to take part.' : poll.identity === 'optional' ? 'Log in if you want your name on your answers.' : 'No login needed — answers are anonymous.'}</p>
						<button class="btn btn-primary btn-lg mt-6" onclick={() => (showCode = false)}>Show the question<Icon name="arrow-right" size={18} /></button>
					</div>
				</section>
			{:else if showBoard}
				<section class="board-view" in:flyIn aria-label="Whiteboard">
					<div class="board-controls">
						<label class="m-0 flex items-center gap-2 font-normal"><input type="checkbox" class="toggle toggle-sm toggle-primary" checked={access.open} onchange={(e) => saveAccess({ open: e.currentTarget.checked })} />Show the board to participants</label>
						<label class="m-0 flex items-center gap-2 font-normal" for="bd-mode">Who may draw</label>
						<select id="bd-mode" class="select select-sm w-48" value={access.mode} onchange={(e) => saveAccess({ mode: e.currentTarget.value as BoardAccess['mode'] })}>
							<option value="teacher">Only me</option>
							<option value="everyone">Everyone</option>
							<option value="selected">Selected people or groups</option>
						</select>
						{#if !access.open}<span class="small muted">Participants don't see the board yet.</span>{/if}
					</div>
					{#if access.mode === 'selected'}
						<div class="pick small">
							{#if !people}<span class="loading loading-dots loading-sm"></span>
							{:else}
								{#each people.groups as g (g.id)}
									<label class="badge badge-outline badge-lg m-0 cursor-pointer gap-1.5 font-normal"><input type="checkbox" class="checkbox checkbox-xs" checked={access.groups.includes(g.id)} onchange={() => saveAccess({ groups: toggleIn(access.groups, g.id) })} /><span class="gdot" style:background="var(--cat-{g.color})" aria-hidden="true"></span>{g.name}</label>
								{/each}
								{#each [...people.groups.flatMap((g) => g.members ?? []), ...people.ungrouped] as m (m.participant_id)}
									<label class="badge badge-outline badge-lg m-0 cursor-pointer gap-1.5 font-normal"><input type="checkbox" class="checkbox checkbox-xs" checked={access.participants.includes(m.participant_id)} onchange={() => saveAccess({ participants: toggleIn(access.participants, m.participant_id) })} />{m.name}</label>
								{:else}<span class="muted">Nobody has joined yet.</span>{/each}
								<button type="button" class="btn btn-ghost btn-xs" onclick={loadPeople}>Refresh</button>
							{/if}
						</div>
					{/if}
					<Whiteboard board={wb} client={tb} teacher title={poll.title + ' whiteboard'} />
				</section>
			{:else if board && poll.scoring}
				<section class="question" in:flyIn>
					<h1 class="q-title m-0 mb-6 flex items-center gap-3"><Icon name="trophy" size={36} />Leaderboard</h1>
					{#if data?.group_leaderboard?.length}
						<div class="boards">
							<div><h2 class="m-0 mb-3 text-2xl">Groups</h2><Leaderboard ranks={groupRanks(data.group_leaderboard)} big /></div>
							<div><h2 class="m-0 mb-3 text-2xl">Individuals</h2><Leaderboard ranks={data.leaderboard ?? []} /></div>
						</div>
					{:else}
						<Leaderboard ranks={data?.leaderboard ?? []} big />
					{/if}
				</section>
			{:else if q}
				{#key q.id}
					<section class="question" in:flyIn={{ x: 30, y: 0, duration: 260 }}>
						<div class="toon-panel q-banner">
							<div class="flex flex-wrap items-center gap-3">
							<p class="muted m-0 text-lg">Question {cur + 1} of {questions.length} · {TYPE_META[q.type].label}{poll.scoring && q.key ? ` · ${q.points ?? 100} points` : ''}</p>
							{#if secs !== null && !answerShown}<span class="timer tabular" class:low={secs <= 5} role="timer"><Icon name="clock" size={22} />{secs === 0 ? "Time's up" : secs}</span>{/if}
						</div>
						<RichText text={q.text} format={q.body.format} blanks="line" class="q-title" />
							</div>
						{#if presenter && poll.show_results === 'presenter' && !poll.revealed}
							<p class="hidden-note muted"><Icon name="eye-off" size={18} /> Results are hidden from participants. Press <kbd class="kbd kbd-sm">R</kbd> to reveal them.</p>
						{/if}
						<PollResults question={q} result={data?.results[q.id]} teacher big toon pollId={id} answerKey={answerShown || poll.status === 'closed' ? q.key : null} />
					</section>
				{/key}
			{:else}
				<p class="muted p-8 text-center text-xl">Add questions to this poll first.</p>
			{/if}
		</main>
		{#if !showCode && !showBoard && !board}
			<aside class="toon-panel people-card" aria-label="Who's taking part">
				<strong class="side-title">Participants ({data?.participants ?? 0})</strong>
				{#if poll.scoring}
					<Leaderboard ranks={data?.leaderboard ?? []} limit={8} />
				{:else if q}
					<p class="answers-in tabular m-0">{data?.results[q.id]?.responses ?? 0}</p>
					<p class="small m-0">answered this question</p>
				{/if}
			</aside>
		{/if}
		</div>

		<footer class="controls">
			<button class="btn" disabled={cur === 0} onclick={() => go(cur - 1)}><Icon name="arrow-left" size={18} />Previous</button>
			<div class="dots" aria-label="Questions">
				{#each questions as qq, i (qq.id)}
					<button class="dot" class:on={i === cur} aria-label="Question {i + 1}" aria-current={i === cur ? 'step' : undefined} onclick={() => go(i)}></button>
				{/each}
			</div>
			{#if presenter && poll.show_results === 'presenter'}
				<button class="btn" class:btn-primary={!poll.revealed} onclick={reveal}><Icon name={poll.revealed ? 'eye-off' : 'eye'} size={18} />{poll.revealed ? 'Hide results' : 'Reveal results'}</button>
			{/if}
			{#if poll.scoring}
				{#if q?.key && !board}<span class="tooltip" data-tip="{sharedReveal ? 'Shows participants too' : 'This screen only'} (A)"><button class="btn" aria-pressed={answerShown} onclick={toggleAnswer}><Icon name="check-circle" size={18} />{answerShown ? 'Hide answer' : 'Show answer'}</button></span>{/if}
				<span class="tooltip" data-tip="Leaderboard (L)"><button class="btn" aria-pressed={board} onclick={() => (board = !board)}><Icon name="trophy" size={18} />{board ? 'Question' : 'Leaderboard'}</button></span>
			{/if}
			<span class="tooltip" data-tip="Whiteboard (B)"><button class="btn" aria-pressed={showBoard} onclick={toggleBoard}><Icon name="brush" size={18} />{showBoard ? 'Questions' : 'Board'}</button></span>
			<span class="tooltip" data-tip="Add a question now (N)"><button class="btn" onclick={openAdd}><Icon name="plus" size={18} />Add question</button></span>
			<button class="btn btn-primary" disabled={cur >= questions.length - 1} onclick={() => go(cur + 1)}>Next<Icon name="arrow-right" size={18} /></button>
		</footer>
		<dialog bind:this={addDialog} class="modal" aria-labelledby="add-h" oncancel={(e) => { e.preventDefault(); closeAdd(); }}>
			{#if adding}
				<div class="modal-box add-box">
					<div class="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2">
						<h2 id="add-h" class="m-0 text-lg">Add a question now</h2>
						<span class="spacer"></span>
						<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={addNext} />Put it next</label>
						<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={addShow} />{presenter ? 'Show it to everyone now' : 'Show it on this screen'}</label>
					</div>
					<PollQuestionEditor pollId={id} scoring={poll.scoring} pacing={poll.pacing} extra={{ after_id: addNext && q ? q.id : '', present: addShow && presenter }} onsaved={added} oncancel={closeAdd} />
				</div>
				<div class="modal-backdrop"><button tabindex="-1" aria-label="Close" onclick={closeAdd}>close</button></div>
			{/if}
		</dialog>
		<p class="keys small muted">← → move · {presenter ? 'R reveal · ' : ''}{poll.scoring ? 'A answer · L leaderboard · ' : ''}N add · B board · Q code · F full screen{presenter ? ' · participants follow this screen' : ''}</p>
	{/if}
</div>

<style>
	.stage { position: relative; min-height: 100dvh; display: flex; flex-direction: column; isolation: isolate; overflow-x: clip; }
	/* Clouds, behind everything. */
	.sky { position: absolute; inset: 0; width: 100%; height: 100%; z-index: -1; pointer-events: none; }
	.sky path { fill: #fff; opacity: 0.75; }
	.bar { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1.5rem; background: #fff; border-bottom: var(--toon-line); }
	.join-hint { font-size: 1.05rem; }
	@media (max-width: 720px) { .join-hint { display: none; } }
	.layout { flex: 1; display: grid; grid-template-columns: minmax(0, 1fr); align-items: start; width: 100%; max-width: 110rem; margin: 0 auto; }
	/* Join on the left, people on the right, as long as the question keeps its room. */
	@media (min-width: 1180px) {
		.layout.wide { grid-template-columns: 13rem minmax(0, 1fr) 16rem; gap: 0 0.5rem; padding: 0 1.25rem; }
		.layout.wide .join-card { order: 0; }
		.layout.wide .main { order: 1; }
		.layout.wide .people-card { order: 2; }
		.layout.wide .join-card, .layout.wide .people-card { margin: 2rem 0 0; }
	}
	/* A presenter's screen: exactly the window, with nothing to scroll for a choice
	   question. What's still too tall, such as a long list of answers, scrolls in the middle. */
	@media (min-width: 1180px) and (min-height: 620px) {
		.stage { height: 100dvh; overflow: clip; }
		.layout { min-height: 0; grid-template-rows: minmax(0, 1fr); align-items: stretch; }
		.main { display: flex; flex-direction: column; min-height: 0; overflow-y: auto; padding-top: 1.25rem; padding-bottom: 0.5rem; }
		.question { flex: 1; min-height: 0; display: flex; flex-direction: column; }
		.q-banner { flex: none; margin-bottom: 0.6rem; padding: 0.6rem 1.25rem; }
		.question :global(.q-title) { font-size: clamp(1.4rem, 0.8rem + 1.6vw, 2.5rem); }
		.layout.wide .join-card, .layout.wide .people-card { margin-top: 1.25rem; align-self: start; max-height: calc(100% - 2rem); overflow-y: auto; }
		.controls { padding-block: 0.5rem; }
		.keys { margin-bottom: 0.4rem; }
	}
	.side-title { display: block; font-size: 1.1rem; font-weight: 650; }
	.join-card, .people-card { display: grid; gap: 0.5rem; padding: 0.9rem; margin: 0 1.5rem 1rem; min-width: 0; }
	.join-card { justify-items: center; text-align: center; }
	@media (max-width: 1179px) { .join-card { display: none; } .layout .main { order: 0; } .layout .people-card { order: 1; } }
	.mini-qr { width: 150px; max-width: 100%; }
	.mini-code { font-size: 1.9rem; font-weight: 700; letter-spacing: 0.08em; font-kerning: none; line-height: 1.1; }
	.answers-in { font-size: 3.5rem; font-weight: 700; line-height: 1; }
	.q-banner { padding: 0.9rem 1.25rem; margin-bottom: 1.25rem; text-align: center; }
	.q-banner > div { justify-content: center; }
	.code-inline { letter-spacing: 0.08em; }
	.main { flex: 1; width: 100%; min-width: 0; max-width: 78rem; margin: 0 auto; padding: 2rem 1.5rem 1rem; }
	.question :global(.q-title) { font-size: clamp(1.6rem, 1rem + 2.2vw, 2.75rem); font-weight: 650; line-height: 1.2; margin: 0.25rem 0 0; }
	.boards { display: grid; gap: 2rem; }
	@media (min-width: 1000px) { .boards { grid-template-columns: 1.4fr 1fr; } }
	.board-view { display: grid; gap: 0.75rem; }
	.board-controls { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 1rem; }
	.pick { display: flex; flex-wrap: wrap; gap: 0.35rem; align-items: center; }
	.gdot { width: 0.6rem; height: 0.6rem; border-radius: 999px; display: inline-block; }
	.add-box { width: min(72rem, calc(100vw - 2rem)); max-width: none; max-height: calc(100dvh - 2rem); }
	.timer { display: inline-flex; align-items: center; gap: 0.4rem; font-size: 1.6rem; font-weight: 800; padding: 0.2rem 0.9rem; border-radius: 999px; background: #ffc72e; border: var(--toon-line); }
	.timer.low { background: #ff8177; color: var(--toon-ink); }
	.hidden-note { display: flex; align-items: center; gap: 0.5rem; font-size: 1.1rem; }
	.lobby { display: grid; gap: 3rem; grid-template-columns: auto minmax(0, 1fr); align-items: center; min-height: 60vh; }
	@media (max-width: 860px) { .lobby { grid-template-columns: 1fr; justify-items: center; text-align: center; } }
	.qr-box { background: #fff; padding: 1rem; border: var(--toon-line); border-radius: 1.25rem; box-shadow: 8px 8px 0 var(--toon-ink); rotate: -2deg; }
	.big-code { font-size: clamp(3.5rem, 2rem + 6vw, 7rem); font-weight: 700; letter-spacing: 0.1em; font-kerning: none; line-height: 1.1; color: var(--toon-ink); }
	.controls { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1.5rem; flex-wrap: wrap; justify-content: center; }
	.dots { display: flex; gap: 0.4rem; flex-wrap: wrap; justify-content: center; flex: 1; }
	/* 24 px hit area (WCAG 2.5.8) around a small visible dot. */
	.dot { width: 1.5rem; height: 1.5rem; border-radius: 999px; border: 0; background: transparent; cursor: pointer; padding: 0; display: grid; place-items: center; }
	.dot::before { content: ''; width: 0.75rem; height: 0.75rem; border-radius: 999px; background: #fff; border: 2px solid var(--toon-ink); transition: transform var(--motion-fast), background-color var(--motion-fast); }
	.dot.on::before { background: #ffc72e; transform: scale(1.4); }
	.keys { text-align: center; margin: 0 0 0.75rem; }
</style>
