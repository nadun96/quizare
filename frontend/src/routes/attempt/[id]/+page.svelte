<script lang="ts">
	// The student's live quiz (UC-03). The server owns all state and timers;
	// this page renders whatever state it is sent and reports changes back.
	// Layout and feedback follow the UX research (D-39): progress, visible
	// saving, a calm timer that can be collapsed, and time-left notices.
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
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { flyIn, scaleIn } from '$lib/ui/motion';
	import { prefs } from '$lib/ui/prefs.svelte';

	const id = $derived(page.params.id ?? '');
	let st = $state<StudentState | null>(null);
	let error = $state('');
	let notice = $state('');
	let online = $state(true);
	let pendingSaves = $state(0);
	let now = $state(Date.now());
	let advancing = $state(false);
	let fullscreenOffer = $state(false);
	let savedAt = $state(0); // last time the queue drained, for the "Saved" confirmation
	let announcement = $state(''); // time-left notices for screen readers

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
		queue.onChange = (n) => {
			if (pendingSaves > 0 && n === 0) savedAt = Date.now();
			pendingSaves = n;
		};
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
		const unanswered = st?.answered?.filter((a) => !a).length ?? 0;
		const ok = await confirmDialog({
			title: 'Submit your answers?',
			body: (unanswered ? `${unanswered} question${unanswered === 1 ? ' is' : 's are'} still unanswered. ` : '') + "You can't change your answers afterwards.",
			confirm: 'Submit quiz',
			cancel: 'Keep working'
		});
		if (!ok) return;
		await queue.flush();
		apply(await api.post<StudentState>('/api/attempts/' + id + '/submit'));
	}

	// Time-left notices (research: students want them; WCAG 4.1.3 status messages).
	const MARKS = [5 * 60_000, 60_000, 30_000];
	let lastLeft: number | null = null;
	$effect(() => {
		const left = quizLeft;
		if (left != null && lastLeft != null && st?.state === 'in_progress') {
			const crossed = MARKS.find((m) => lastLeft! > m && left <= m);
			if (crossed) announcement = crossed >= 60_000 ? `${crossed / 60_000} minute${crossed === 60_000 ? '' : 's'} left` : '30 seconds left';
		}
		lastLeft = left;
	});
	const urgency = (left: number | null, warnAt: number, dangerAt: number) => (left == null ? '' : left <= dangerAt ? 'danger' : left <= warnAt ? 'warn' : '');
	const quizUrgency = $derived(urgency(quizLeft, 5 * 60_000, 60_000));
	// The timer can be made small, but it always comes back for the last minute.
	const timerCompact = $derived(prefs.timer === 'compact' && quizUrgency !== 'danger');
	const savedRecently = $derived((void now, savedAt > 0 && Date.now() - savedAt < 2500));
	const answeredCount = $derived(st?.answered?.filter(Boolean).length ?? null);
	const countdownPct = $derived(countdown == null ? 0 : Math.max(0, Math.min(100, (countdown / 10_000) * 100)));

	function beforeUnload(e: BeforeUnloadEvent) {
		if (running()) e.preventDefault();
	}
</script>

<svelte:window onbeforeunload={beforeUnload} />

<div class="quiz">
	<header class="quiz-bar">
		<div class="quiz-bar-inner">
			<strong class="title">{st?.quiz_title ?? 'Quiz'}</strong>
			<span class="spacer"></span>
			{#if st?.team}
				<span class="tooltip tooltip-bottom" data-tip="Your team"><span class="badge badge-soft gap-1 max-w-40"><span class="tdot" style:background="var(--cat-{st.team.color})" aria-hidden="true"></span><span class="truncate">{st.team.name}</span>{#if st.captain}<Icon name="star" size={12} /><span class="sr-only">(captain)</span>{/if}</span></span>
			{/if}
			{#if !online}
				<span class="badge badge-soft badge-warning gap-1" role="status"><Icon name="wifi-off" size={14} />Offline · answers will sync</span>
			{:else if pendingSaves > 0}
				<span class="badge badge-soft gap-1" role="status"><span class="loading loading-spinner loading-xs"></span>Saving…</span>
			{:else if savedRecently}
				<span class="badge badge-soft badge-success gap-1" role="status" in:scaleIn><Icon name="check" size={14} />Saved</span>
			{/if}
			{#if quizLeft != null && (st?.state === 'in_progress' || st?.state === 'paused')}
				<button
					class="timer {quizUrgency}"
					class:compact={timerCompact}
					aria-label={`Time left: ${formatDuration(quizLeft)}. ${timerCompact ? 'Show' : 'Hide'} the timer`}
					title={timerCompact ? 'Show timer' : 'Make the timer smaller'}
					onclick={() => prefs.set('timer', prefs.timer === 'compact' ? 'shown' : 'compact')}
				>
					<Icon name="clock" size={16} />
					{#if !timerCompact}<span class="tabular">{formatDuration(quizLeft)}</span>{/if}
				</button>
			{/if}
		</div>
		{#if st && (st.state === 'in_progress' || st.state === 'paused') && st.total > 0}
			<progress class="progress progress-primary quiz-progress" value={st.index + 1} max={st.total} aria-label={`Question ${st.index + 1} of ${st.total}`}></progress>
		{/if}
	</header>
	<p class="sr-only" aria-live="assertive">{announcement}</p>

	<section class="content">
		{#if error}
			<div class="alert alert-soft alert-error" role="alert"><Icon name="alert" />{error}</div>
		{:else if !st}
			<Skeleton lines={4} />
		{:else if st.state === 'waiting'}
			<div class="state-card" in:flyIn>
				<div class="pulse-dot" aria-hidden="true"><span></span></div>
				<h1>You're in the waiting room</h1>
				<p class="lead">Your teacher will let you in shortly. Keep this page open.</p>
				{#if st.student_number}<p class="badge badge-soft badge-lg">Student ID: {st.student_number}</p>{/if}
				<div class="alert alert-soft alert-info small text-left">
					<Icon name="info" />
					<span>Once the quiz starts, stay on this screen. Leaving it or switching apps {st.violation_policy === 'log_only' ? 'is recorded' : 'may end your attempt'}.</span>
				</div>
			</div>
		{:else if st.state === 'admitted' && st.countdown_deadline == null}
			<div class="state-card" in:flyIn>
				<div class="pulse-dot" aria-hidden="true"><span></span></div>
				<h1>You're in</h1>
				<p class="lead" role="status">Waiting for your teacher to start the quiz. Keep this page open.</p>
				<p class="small muted">{st.total} question{st.total === 1 ? '' : 's'} · Stay on this screen until you finish.</p>
			</div>
		{:else if st.state === 'admitted'}
			<div class="state-card" in:flyIn>
				<h1>Get ready</h1>
				<div
					class="radial-progress countdown-ring text-primary"
					style:--value={countdownPct}
					style:--size="9rem"
					style:--thickness="0.6rem"
					role="timer"
					aria-live="polite"
					aria-label="Starting in {formatDuration(countdown)}"
				>
					<span class="tabular text-3xl font-bold text-base-content">{formatDuration(countdown)}</span>
				</div>
				<p class="lead">The quiz starts automatically when the countdown ends.</p>
				<button class="btn btn-primary btn-lg btn-wide" onclick={start}><Icon name="play" size={18} />Start now</button>
				<p class="small muted">{st.total} question{st.total === 1 ? '' : 's'} · Stay on this screen until you finish.</p>
			</div>
		{:else if st.state === 'in_progress' && st.question}
			{#if fullscreenOffer && !document.fullscreenElement}
				<button class="btn btn-outline btn-primary w-full" onclick={() => enterFullscreen().then(() => (fullscreenOffer = false))}>Tap to go full screen</button>
			{/if}
			{#if st.warnings > 0 && st.violation_policy === 'warn_then_invalidate'}
				<div class="alert alert-soft alert-warning" role="alert" in:flyIn>
					<Icon name="alert" />
					<span><strong>Warning {st.warnings} of {st.allowed_warnings}:</strong> leaving the quiz was detected. Next time your attempt will end.</span>
				</div>
			{/if}
			<div class="q-meta">
				<span class="font-semibold">Question {st.index + 1} <span class="muted font-normal">of {st.total}</span></span>
				<span class="muted">· {st.question.marks} mark{st.question.marks === 1 ? '' : 's'}</span>
				{#if answeredCount != null}<span class="muted">· {answeredCount} answered</span>{/if}
				<span class="spacer"></span>
				{#if questionLeft != null}
					<span class="badge badge-soft tabular gap-1 {urgency(questionLeft, 30_000, 10_000)}"><Icon name="clock" size={13} />This question: {formatDuration(questionLeft)}</span>
				{/if}
			</div>
			{#if !st.one_way && st.answered}
				<nav class="nav-q" aria-label="Questions">
					{#each st.answered as done, i (i)}
						<button
							class="q-dot"
							class:cur={i === st.index}
							class:done
							aria-current={i === st.index ? 'step' : undefined}
							aria-label={`Question ${i + 1}, ${done ? 'answered' : 'not answered'}`}
							onclick={() => goTo(i)}>{#if done && i !== st.index}<Icon name="check" size={12} />{:else}{i + 1}{/if}</button
						>
					{/each}
				</nav>
			{/if}
			{#key st.question.id}
				<div class="card card-border question-card bg-base-100 p-4 shadow-sm sm:p-6" in:flyIn={{ x: 24, y: 0, duration: 240 }}>
					<QuestionView question={st.question} value={st.answer} onchange={onAnswer} />
				</div>
			{/key}
			{#if notice}<div class="alert alert-soft alert-warning small" role="status"><Icon name="info" />{notice}</div>{/if}
			<div class="actions">
				{#if !st.one_way}<button class="btn btn-ghost" onclick={submitNow}><Icon name="send" size={16} />Submit quiz</button>{/if}
				<span class="spacer"></span>
				<button class="btn btn-primary btn-lg min-w-36" disabled={advancing} onclick={next}>
					{#if advancing}<span class="loading loading-spinner loading-sm"></span>{/if}
					{isLast ? 'Finish' : 'Next'}
					{#if !advancing && !isLast}<Icon name="arrow-right" size={18} />{/if}
				</button>
			</div>
			{#if st.one_way}<p class="small muted center m-0"><Icon name="info" size={14} /> You can't go back to a question once you move on.</p>{/if}
		{:else if st.state === 'paused'}
			<div class="state-card" in:flyIn>
				<div class="state-icon bg-warning/15 text-warning"><Icon name="pause" size={30} /></div>
				<h1>Paused</h1>
				<p class="lead">Your teacher has paused the quiz. Your time is stopped.</p>
			</div>
		{:else if st.state === 'submitted'}
			<div class="state-card" in:flyIn>
				<div class="state-icon bg-success/15 text-success" in:scaleIn={{ start: 0.5, duration: 350 }}><Icon name="check" size={34} /></div>
				<h1>Submitted</h1>
				<p class="lead">Your answers are in. Results appear when your teacher releases them.</p>
				<a class="btn btn-primary" href="/my">My quizzes</a>
			</div>
		{:else if st.state === 'invalidated'}
			<div class="state-card" in:flyIn>
				<div class="state-icon bg-error/15 text-error"><Icon name="alert" size={30} /></div>
				<h1>Attempt ended</h1>
				<p class="lead">Your attempt was stopped because leaving the quiz was detected ({st.invalid_reason?.replace('_', ' ')}). Your teacher has been told and can reinstate it.</p>
			</div>
		{:else}
			<div class="state-card" in:flyIn>
				<h1>This session has ended</h1>
				<a class="btn btn-primary" href="/my">My quizzes</a>
			</div>
		{/if}
	</section>
</div>

<style>
	.quiz { min-height: 100dvh; display: flex; flex-direction: column; background: var(--color-base-200); }
	.quiz-bar { position: sticky; top: 0; z-index: 5; background: color-mix(in oklab, var(--color-base-100) 92%, transparent); backdrop-filter: blur(8px); border-bottom: 1px solid var(--color-base-300); }
	.quiz-bar-inner { display: flex; gap: 0.5rem; align-items: center; padding: 0.5rem 1rem; max-width: 48rem; margin: 0 auto; min-height: 3.25rem; }
	.title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
	.quiz-progress { display: block; height: 0.25rem; border-radius: 0; width: 100%; }
	.content { width: 100%; max-width: 48rem; margin: 0 auto; padding: 1rem; display: grid; gap: 0.875rem; align-content: start; flex: 1; }
	.lead { font-size: 1.075rem; color: var(--color-muted); max-width: 36rem; margin-inline: auto; }
	.state-card { text-align: center; padding: 2.5rem 0.5rem; display: grid; justify-items: center; gap: 0.75rem; }
	.state-card h1 { margin: 0; }
	.state-icon { width: 4.5rem; height: 4.5rem; border-radius: 999px; display: grid; place-items: center; }
	.countdown-ring { margin: 0.5rem 0; }
	.q-meta { display: flex; flex-wrap: wrap; gap: 0.35rem 0.5rem; align-items: center; font-size: 0.925rem; }
	.question-card :global(.qtext) { font-size: 1.2rem; line-height: 1.55; }
	.actions { position: sticky; bottom: 0; display: flex; gap: 0.75rem; align-items: center; padding: 0.75rem 0 calc(0.75rem + env(safe-area-inset-bottom)); background: linear-gradient(to top, var(--color-base-200) 75%, transparent); }

	/* Timer pill: calm by default, amber under 5 minutes, red and pulsing in the last minute. */
	.timer { display: inline-flex; align-items: center; gap: 0.35rem; min-height: 2.25rem; padding: 0 0.75rem; border-radius: 999px; font-weight: 700; font-size: 1rem; border: 1px solid var(--color-base-300); background: var(--color-base-100); color: var(--color-base-content); cursor: pointer; transition: background-color var(--motion), color var(--motion), padding var(--motion); }
	.timer.compact { padding: 0 0.6rem; color: var(--color-muted); }
	.timer.warn { background: color-mix(in oklab, var(--color-warning) 14%, var(--color-base-100)); color: var(--color-warning); border-color: transparent; }
	.timer.danger { background: var(--color-error); color: var(--color-error-content); border-color: transparent; animation: pulse 1.2s ease-in-out infinite; }
	@keyframes pulse { 50% { box-shadow: 0 0 0 6px color-mix(in oklab, var(--color-error) 20%, transparent); } }

	/* Question navigator: number, check for answered, ring for current (not colour alone). */
	.nav-q { display: flex; gap: 0.375rem; flex-wrap: wrap; }
	.q-dot { width: 2.25rem; height: 2.25rem; border-radius: 999px; display: grid; place-items: center; font-size: 0.85rem; font-weight: 600; border: 1px solid var(--color-field); background: var(--color-base-100); color: var(--color-base-content); cursor: pointer; transition: transform var(--motion-fast), background-color var(--motion-fast); }
	.q-dot:hover { transform: translateY(-1px); }
	.q-dot.done { background: color-mix(in oklab, var(--color-success) 14%, var(--color-base-100)); color: var(--color-success); border-color: color-mix(in oklab, var(--color-success) 40%, transparent); }
	.q-dot.cur { background: var(--color-primary); color: var(--color-primary-content); border-color: var(--color-primary); box-shadow: 0 0 0 3px color-mix(in oklab, var(--color-primary) 25%, transparent); }

	/* Waiting room: a gentle "live" pulse. */
	.pulse-dot { width: 4.5rem; height: 4.5rem; border-radius: 999px; display: grid; place-items: center; background: color-mix(in oklab, var(--color-primary) 12%, transparent); }
	.pulse-dot span { width: 1.25rem; height: 1.25rem; border-radius: 999px; background: var(--color-primary); animation: breathe 1.8s ease-in-out infinite; }
	@keyframes breathe { 50% { transform: scale(1.35); opacity: 0.6; } }

	.quiz :global(*) { -webkit-user-select: none; user-select: none; }
	.quiz :global(textarea), .quiz :global(input) { -webkit-user-select: text; user-select: text; }
	.tdot { width: 0.6rem; height: 0.6rem; border-radius: 999px; display: inline-block; flex: none; }
</style>
