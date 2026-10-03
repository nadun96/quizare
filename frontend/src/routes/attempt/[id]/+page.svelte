<script lang="ts">
	// The student's live quiz (UC-03). The server owns all state and timers;
	// this page renders whatever state it is sent and reports changes back.
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { AnswerQueue } from '$lib/answerQueue';
	import { formatDuration } from '$lib/clock';
	import { Proctor, enterFullscreen, type ViolationKind } from '$lib/proctor';
	import { LiveSocket } from '$lib/socket';
	import { auth, loginUrl } from '$lib/session.svelte';
	import QuestionView from '$lib/QuestionView.svelte';
	import type { Response, StudentState } from '$lib/types';

	const id = $derived(page.params.id ?? '');
	let st = $state<StudentState | null>(null);
	let error = $state('');
	let notice = $state('');
	let online = $state(true);
	let pendingSaves = $state(0);
	let now = $state(Date.now());
	let advancing = $state(false);
	let fullscreenOffer = $state(false);

	let socket: LiveSocket;
	let queue: AnswerQueue;
	let proctor: Proctor;
	let ticker: ReturnType<typeof setInterval>;
	let flusher: ReturnType<typeof setInterval>;

	const running = () => st?.state === 'in_progress';

	function apply(next: StudentState) {
		const wasRunning = st?.state === 'in_progress';
		st = next;
		if (!wasRunning && next.state === 'in_progress' && !document.fullscreenElement && document.fullscreenEnabled) fullscreenOffer = true;
		if (next.state !== 'in_progress' && document.fullscreenElement) document.exitFullscreen().catch(() => {});
	}

	async function refresh() {
		try {
			apply(await api.get<StudentState>('/api/attempts/' + id));
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) goto(loginUrl('/attempt/' + id));
			else if (e instanceof ApiError && e.status !== 0) error = e.message;
		}
	}

	function beacon(kind: ViolationKind) {
		const body = new Blob([JSON.stringify({ kind, client_ts: Date.now() })], { type: 'text/plain' });
		if (!navigator.sendBeacon?.('/beacon/attempts/' + id + '/violations', body)) socket.send({ type: 'violation', kind, client_ts: Date.now() });
	}

	onMount(() => {
		queue = new AnswerQueue('answers:' + id, async (s) => {
			try {
				await api.put('/api/attempts/' + id + '/answers/' + s.questionId, { response: s.response, seq: s.seq });
				return 'ok';
			} catch (e) {
				if (e instanceof ApiError && (e.status === 0 || e.status >= 500)) return 'retry';
				if (e instanceof ApiError) notice = e.message;
				return 'reject';
			}
		});
		queue.onChange = (n) => (pendingSaves = n);
		pendingSaves = queue.size;

		socket = new LiveSocket('/ws/attempts/' + id);
		socket.onMessage = (m) => {
			if (m.type === 'state') apply(m as unknown as StudentState);
			else if (m.type === 'error') notice = String(m.message ?? '');
		};
		socket.onStatus = (c) => {
			online = c;
			if (c) void queue.flush();
		};
		socket.open();

		proctor = new Proctor({
			report: (kind) => {
				if (!socket.send({ type: 'violation', kind, client_ts: Date.now() })) beacon(kind);
			},
			beacon,
			blurGraceMs: () => st?.blur_grace_ms ?? 2000,
			active: running
		});
		proctor.start();

		ticker = setInterval(() => (now = Date.now()), 250);
		flusher = setInterval(() => void queue.flush(), 3000);
		refresh();
	});

	onDestroy(() => {
		socket?.close();
		proctor?.stop();
		clearInterval(ticker);
		clearInterval(flusher);
	});

	// Remaining times from server deadlines (ADR-07); `now` drives re-rendering.
	const remaining = (deadline: number | null | undefined) => {
		void now;
		return socket ? socket.clock.remaining(deadline) : null;
	};
	const countdown = $derived(st ? remaining(st.countdown_deadline) : null);
	const quizLeft = $derived(st ? (st.state === 'paused' ? st.quiz_remaining_ms : remaining(st.quiz_deadline)) : null);
	const questionLeft = $derived(st ? (st.state === 'paused' ? st.question_remaining_ms : remaining(st.question_deadline)) : null);
	const isLast = $derived(!!st && st.index >= st.total - 1);

	async function start() {
		await enterFullscreen(); // FR-PR-01, needs this tap on most browsers
		if (!socket.send({ type: 'start' })) {
			try {
				apply(await api.post<StudentState>('/api/attempts/' + id + '/start'));
			} catch (e) {
				notice = e instanceof ApiError ? e.message : 'Could not start';
			}
		}
	}

	function onAnswer(r: Response) {
		if (st?.question) queue.enqueue(st.question.id, r);
	}

	async function next() {
		if (!st?.question) return;
		advancing = true;
		try {
			await queue.flush();
			if (!st.one_way && isLast) apply(await api.post<StudentState>('/api/attempts/' + id + '/submit'));
			else apply(await api.post<StudentState>('/api/attempts/' + id + '/advance', { question_id: st.question.id }));
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Could not continue; we will retry';
		} finally {
			advancing = false;
		}
	}

	async function goTo(i: number) {
		await queue.flush();
		try {
			apply(await api.post<StudentState>('/api/attempts/' + id + '/goto', { index: i }));
		} catch (e) {
			notice = e instanceof ApiError ? e.message : '';
		}
	}

	async function submitNow() {
		if (!confirm('Submit your answers now? You cannot change them afterwards.')) return;
		await queue.flush();
		apply(await api.post<StudentState>('/api/attempts/' + id + '/submit'));
	}

	function beforeUnload(e: BeforeUnloadEvent) {
		if (running()) e.preventDefault();
	}
</script>

<svelte:window onbeforeunload={beforeUnload} />

<div class="quiz">
	<header class="bar">
		<strong class="title">{st?.quiz_title ?? 'Quiz'}</strong>
		<span class="spacer"></span>
		{#if !online}<span class="badge warn" role="status">Reconnecting…</span>{/if}
		{#if pendingSaves > 0}<span class="badge warn" role="status">{pendingSaves} unsaved</span>{/if}
		{#if quizLeft != null && (st?.state === 'in_progress' || st?.state === 'paused')}
			<span class="badge" class:danger={quizLeft < 60_000} aria-label="Time left in the quiz">⏱ {formatDuration(quizLeft)}</span>
		{/if}
	</header>

	<section class="content">
		{#if error}
			<p class="alert danger">{error}</p>
		{:else if !st}
			<p>Loading…</p>
		{:else if st.state === 'waiting'}
			<div class="center stack">
				<h1>You're in the waiting room</h1>
				<p>Your teacher will let you in shortly. Keep this page open.</p>
				{#if st.student_number}<p class="muted">Student ID: {st.student_number}</p>{/if}
				<p class="small muted">Leaving this page or switching apps once the quiz starts may {st.violation_policy === 'log_only' ? 'be recorded' : 'end your attempt'}.</p>
			</div>
		{:else if st.state === 'admitted'}
			<div class="center stack">
				<h1>Get ready</h1>
				<p class="big" aria-live="polite">{formatDuration(countdown)}</p>
				<p>The quiz starts automatically when the countdown ends.</p>
				<button class="primary" onclick={start}>Start now</button>
				<p class="small muted">{st.total} questions. Stay on this screen until you finish.</p>
			</div>
		{:else if st.state === 'in_progress' && st.question}
			{#if fullscreenOffer && !document.fullscreenElement}
				<button class="fs" onclick={() => enterFullscreen().then(() => (fullscreenOffer = false))}>Tap to go full screen</button>
			{/if}
			{#if st.warnings > 0 && st.violation_policy === 'warn_then_invalidate'}
				<p class="alert" role="alert">Warning {st.warnings} of {st.allowed_warnings}: leaving the quiz was detected. Next time your attempt will end.</p>
			{/if}
			<div class="row small muted">
				<span>Question {st.index + 1} of {st.total} · {st.question.marks} mark{st.question.marks === 1 ? '' : 's'}</span>
				<span class="spacer"></span>
				{#if questionLeft != null}<span class="badge" class:danger={questionLeft < 10_000}>This question: {formatDuration(questionLeft)}</span>{/if}
			</div>
			{#if !st.one_way && st.answered}
				<nav class="nav-q" aria-label="Questions">
					{#each st.answered as done, i (i)}
						<button class="small" class:cur={i === st.index} class:done onclick={() => goTo(i)}>{i + 1}</button>
					{/each}
				</nav>
			{/if}
			<div class="card">
				<QuestionView question={st.question} value={st.answer} onchange={onAnswer} />
			</div>
			{#if notice}<p class="alert small">{notice}</p>{/if}
			<div class="row actions">
				{#if !st.one_way}<button onclick={submitNow}>Submit quiz</button>{/if}
				<span class="spacer"></span>
				<button class="primary" disabled={advancing} onclick={next}>{isLast ? 'Finish' : 'Next'}</button>
			</div>
			{#if st.one_way}<p class="small muted">You can't go back to a question once you move on.</p>{/if}
		{:else if st.state === 'paused'}
			<div class="center stack">
				<h1>Paused</h1>
				<p>Your teacher has paused the quiz. Your time is stopped.</p>
			</div>
		{:else if st.state === 'submitted'}
			<div class="center stack">
				<h1>Submitted</h1>
				<p>Your answers are in. Results appear when your teacher releases them.</p>
				<a class="button primary" href="/my">My quizzes</a>
			</div>
		{:else if st.state === 'invalidated'}
			<div class="center stack">
				<h1>Attempt ended</h1>
				<p class="alert danger">Your attempt was stopped because leaving the quiz was detected ({st.invalid_reason?.replace('_', ' ')}). Your teacher has been told and can reinstate it.</p>
			</div>
		{:else}
			<div class="center stack"><h1>This session has ended</h1><a href="/my">My quizzes</a></div>
		{/if}
	</section>
</div>

<style>
	.quiz { min-height: 100dvh; display: flex; flex-direction: column; background: var(--bg); }
	.bar { display: flex; gap: 0.5rem; align-items: center; padding: 0.5rem 1rem; background: var(--surface); border-bottom: 1px solid var(--border); position: sticky; top: 0; z-index: 2; }
	.title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.content { width: 100%; max-width: 760px; margin: 0 auto; padding: 1rem; display: grid; gap: 0.75rem; }
	.center { text-align: center; padding: 2rem 0; }
	.big { font-size: 3.5rem; font-weight: 700; font-variant-numeric: tabular-nums; margin: 0; }
	.actions { position: sticky; bottom: 0; padding: 0.5rem 0; background: var(--bg); }
	.nav-q { display: flex; gap: 0.25rem; flex-wrap: wrap; }
	.nav-q .cur { border-color: var(--primary); }
	.nav-q .done { background: var(--ok-bg); }
	.badge.danger { background: var(--danger-bg); color: var(--danger); }
	.fs { width: 100%; }
	.quiz :global(*) { -webkit-user-select: none; user-select: none; }
	.quiz :global(textarea), .quiz :global(input) { -webkit-user-select: text; user-select: text; }
</style>
