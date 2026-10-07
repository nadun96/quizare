<script lang="ts">
	// Presenter screen for a poll (D-40): big, live results for the room.
	// Keys: ← → question, R reveal (presenter-led), A answer and L leaderboard
	// (scored polls, D-42), N add a question while live (V2-05), Q code,
	// F full screen.
	import { onDestroy } from 'svelte';
	import { Tween } from 'svelte/motion';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { TYPE_META } from '$lib/poll/meta';
	import Leaderboard from '$lib/poll/Leaderboard.svelte';
	import PollQuestionEditor from '$lib/poll/PollQuestionEditor.svelte';
	import PollResults from '$lib/poll/PollResults.svelte';
	import { groupRanks, secondsLeft } from '$lib/poll/scoring';
	import type { Poll, TeacherResults } from '$lib/poll/types';
	import RichText from '$lib/richtext/RichText.svelte';
	import { LiveSocket } from '$lib/socket';
	import Icon from '$lib/ui/Icon.svelte';
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
	let clockOffset = 0;
	let now = $state(Date.now());
	let connected = $state(false);
	let socket: LiveSocket | null = null;

	$effect(() => {
		if (!ready() || !id) return;
		socket = new LiveSocket('/ws/teacher/polls/' + id);
		socket.onStatus = (c) => (connected = c);
		socket.onMessage = (m) => {
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
		else if (e.key.toLowerCase() === 'f') document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen?.().catch(() => {});
	}
	const host = $derived(poll ? poll.join_url.replace(/^https?:\/\//, '').replace(/\/p\/.*$/, '') : '');
</script>

<svelte:window onkeydown={key} />
<svelte:head><title>{poll ? poll.title + ' · Presenting' : 'Presenting'}</title></svelte:head>

<div class="stage">
	{#if !poll}
		<div class="p-8"><Skeleton lines={5} /></div>
	{:else}
		<header class="bar">
			<strong class="truncate text-lg">{poll.title}</strong>
			<span class="spacer"></span>
			<span class="join-hint">Join at <strong>{host}/join</strong> with <strong class="tabular code-inline">{poll.join_code}</strong></span>
			<span class="badge badge-soft badge-lg gap-1 tabular" aria-live="polite"><Icon name="users" size={16} />{Math.round(joined.current)}</span>
			{#if !connected}<span class="badge badge-soft badge-warning gap-1"><Icon name="wifi-off" size={14} />Reconnecting</span>{/if}
			<button class="btn btn-ghost btn-sm" onclick={() => (showCode = !showCode)} title="Show the join code (Q)"><Icon name="qr" size={18} /></button>
		</header>

		{#if poll.status !== 'open'}
			<div class="alert alert-soft alert-warning mx-auto mt-4 max-w-3xl">
				<Icon name="info" /><span>The poll is {poll.status}. Participants can't answer until it's open.</span>
				<button class="btn btn-sm btn-primary" onclick={openPoll}>Open now</button>
			</div>
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
						<div class="flex flex-wrap items-center gap-3">
							<p class="muted m-0 text-lg">Question {cur + 1} of {questions.length} · {TYPE_META[q.type].label}{poll.scoring && q.key ? ` · ${q.points ?? 100} points` : ''}</p>
							{#if secs !== null && !answerShown}<span class="timer tabular" class:low={secs <= 5} role="timer"><Icon name="clock" size={22} />{secs === 0 ? "Time's up" : secs}</span>{/if}
						</div>
						<RichText text={q.text} format={q.body.format} blanks="line" class="q-title" />
						{#if presenter && poll.show_results === 'presenter' && !poll.revealed}
							<p class="hidden-note muted"><Icon name="eye-off" size={18} /> Results are hidden from participants. Press <kbd class="kbd kbd-sm">R</kbd> to reveal them.</p>
						{/if}
						<PollResults question={q} result={data?.results[q.id]} teacher big pollId={id} answerKey={answerShown || poll.status === 'closed' ? q.key : null} />
					</section>
				{/key}
			{:else}
				<p class="muted p-8 text-center text-xl">Add questions to this poll first.</p>
			{/if}
		</main>

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
				{#if q?.key && !board}<button class="btn" aria-pressed={answerShown} onclick={toggleAnswer} title="{sharedReveal ? 'Shows participants too' : 'This screen only'} (A)"><Icon name="check-circle" size={18} />{answerShown ? 'Hide answer' : 'Show answer'}</button>{/if}
				<button class="btn" aria-pressed={board} onclick={() => (board = !board)} title="Leaderboard (L)"><Icon name="trophy" size={18} />{board ? 'Question' : 'Leaderboard'}</button>
			{/if}
			<button class="btn" onclick={openAdd} title="Add a question now (N)"><Icon name="plus" size={18} />Add question</button>
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
		<p class="keys small muted">← → move · {presenter ? 'R reveal · ' : ''}{poll.scoring ? 'A answer · L leaderboard · ' : ''}N add · Q code · F full screen{presenter ? ' · participants follow this screen' : ''}</p>
	{/if}
</div>

<style>
	.stage { min-height: 100dvh; display: flex; flex-direction: column; background: var(--color-base-200); }
	.bar { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1.5rem; background: var(--color-base-100); border-bottom: 1px solid var(--color-base-300); }
	.join-hint { font-size: 1.05rem; }
	@media (max-width: 720px) { .join-hint { display: none; } }
	.code-inline { letter-spacing: 0.08em; }
	.main { flex: 1; width: 100%; max-width: 78rem; margin: 0 auto; padding: 2rem 1.5rem 1rem; }
	.question :global(.q-title) { font-size: clamp(1.6rem, 1rem + 2.2vw, 2.75rem); font-weight: 700; line-height: 1.2; margin: 0.25rem 0 1.5rem; }
	.boards { display: grid; gap: 2rem; }
	@media (min-width: 1000px) { .boards { grid-template-columns: 1.4fr 1fr; } }
	.add-box { width: min(72rem, calc(100vw - 2rem)); max-width: none; max-height: calc(100dvh - 2rem); }
	.timer { display: inline-flex; align-items: center; gap: 0.4rem; font-size: 1.6rem; font-weight: 800; padding: 0.2rem 0.9rem; border-radius: 999px; background: var(--color-base-100); border: 2px solid var(--color-base-300); }
	.timer.low { color: var(--color-error); border-color: var(--color-error); }
	.hidden-note { display: flex; align-items: center; gap: 0.5rem; font-size: 1.1rem; }
	.lobby { display: grid; gap: 3rem; grid-template-columns: auto minmax(0, 1fr); align-items: center; min-height: 60vh; }
	@media (max-width: 860px) { .lobby { grid-template-columns: 1fr; justify-items: center; text-align: center; } }
	.qr-box { background: #fff; padding: 1rem; border-radius: 1.25rem; box-shadow: 0 12px 40px -20px rgb(0 0 0 / 40%); }
	.big-code { font-size: clamp(3.5rem, 2rem + 6vw, 7rem); font-weight: 800; letter-spacing: 0.12em; line-height: 1.1; color: var(--color-primary); }
	.controls { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1.5rem; flex-wrap: wrap; justify-content: center; }
	.dots { display: flex; gap: 0.4rem; flex-wrap: wrap; justify-content: center; flex: 1; }
	/* 24 px hit area (WCAG 2.5.8) around a small visible dot. */
	.dot { width: 1.5rem; height: 1.5rem; border-radius: 999px; border: 0; background: transparent; cursor: pointer; padding: 0; display: grid; place-items: center; }
	.dot::before { content: ''; width: 0.7rem; height: 0.7rem; border-radius: 999px; background: var(--color-field); transition: transform var(--motion-fast), background-color var(--motion-fast); }
	.dot.on::before { background: var(--color-primary); transform: scale(1.4); }
	.keys { text-align: center; margin: 0 0 0.75rem; }
</style>
