<script lang="ts">
	// Taking part in a poll (D-40). No login unless the poll asks for one; the
	// join screen says plainly whether answers are anonymous or named.
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { pollClient } from '$lib/poll/client';
	import { hasAnswer } from '$lib/poll/meta';
	import PollInput from '$lib/poll/PollInput.svelte';
	import Leaderboard from '$lib/poll/Leaderboard.svelte';
	import PollResults from '$lib/poll/PollResults.svelte';
	import { describeKey, fmtPoints, ordinal, secondsLeft } from '$lib/poll/scoring';
	import type { PollAnswer, PollKey, PollQuestion, PollScore, PublicPoll } from '$lib/poll/types';
	import { LiveSocket } from '$lib/socket';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn, scaleIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';

	const code = $derived((page.params.code ?? '').toUpperCase());
	let v = $state<PublicPoll | null>(null);
	let notFound = $state(false);
	let notOpen = $state(false);
	let joinError = $state('');
	let joining = $state(false);
	let answers = $state<Record<string, PollAnswer>>({});
	let saving = $state<Record<string, 'saving' | 'saved' | 'error'>>({});
	let nickname = $state('');
	// Keys and scores that came with this participant's own answers (D-42);
	// the shared live update only carries keys revealed to everyone.
	let ownKeys = $state<Record<string, PollKey>>({});
	let ownScores = $state<Record<string, PollScore>>({});
	let clockOffset = 0; // server time minus local time
	let now = $state(Date.now());
	let socket: LiveSocket | null = null;

	function personal(view: PublicPoll) {
		ownKeys = { ...(view.keys ?? {}) };
		ownScores = { ...(view.scores ?? {}) };
		clockOffset = view.server_time - Date.now();
	}
	async function refresh() {
		try {
			const view = await pollClient.view(code);
			v = view;
			answers = { ...(view.answers ?? {}) };
			personal(view);
			notOpen = notFound = false;
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) notFound = true;
			else if (e instanceof ApiError && e.code === 'poll_not_open') notOpen = true;
		}
	}

	$effect(() => {
		if (!code || !auth.loaded) return;
		refresh();
		socket = new LiveSocket('/ws/polls/' + encodeURIComponent(code));
		socket.onMessage = (m) => {
			if (m.type === 'deleted') {
				notFound = true;
				v = null;
				return;
			}
			if (m.type !== 'update') return;
			const u = m as unknown as PublicPoll;
			if (!v || notOpen || v.status !== u.status) {
				if (u.status !== 'draft') refresh(); // personal view (answers, joined) after a status change
				else notOpen = true;
				return;
			}
			const revealed = Object.keys(u.keys ?? {}).some((k) => !ownScores[k] && v?.answers?.[k]);
			v = { ...v, ...u, joined: v.joined, identified: v.identified, answers: v.answers, me: v.me, me_key: v.me_key, nickname: v.nickname, scores: v.scores };
			clockOffset = u.server_time - Date.now();
			// Own rank (outside the shared top 10) and newly revealed scores
			// come from the personal view, fetched at most every few seconds.
			if (v.scoring && v.joined && (revealed || v.leaderboard_mode === 'everyone')) scoresSoon();
		};
		socket.open();
		return () => socket?.close();
	});
	onDestroy(() => socket?.close());

	let scoreTimer: ReturnType<typeof setTimeout> | null = null;
	function scoresSoon() {
		if (scoreTimer) return;
		scoreTimer = setTimeout(async () => {
			scoreTimer = null;
			try {
				const view = await pollClient.view(code);
				if (v) v = { ...v, me: view.me, leaderboard: view.leaderboard ?? v.leaderboard, scores: view.scores };
				personal(view);
			} catch {
				/* the next update tries again */
			}
		}, 3000);
	}
	onDestroy(() => scoreTimer && clearTimeout(scoreTimer));
	// Countdown for timed presenter-led questions.
	$effect(() => {
		if (!v?.question_started_at || v.pacing !== 'presenter') return;
		const t = setInterval(() => (now = Date.now()), 250);
		return () => clearInterval(t);
	});
	const left = (q: PollQuestion) => (v?.pacing === 'presenter' ? secondsLeft(v.question_started_at, q.time_limit_sec, now + clockOffset) : null);
	const keyOf = (q: PollQuestion) => v?.keys?.[q.id] ?? ownKeys[q.id];
	const scoreOf = (q: PollQuestion) => ownScores[q.id] ?? v?.scores?.[q.id];

	async function join(identify: boolean) {
		joining = true;
		joinError = '';
		try {
			await pollClient.join(code, identify, nickname.trim());
			await refresh();
		} catch (e) {
			joinError = e instanceof ApiError ? e.message : 'Could not join';
		} finally {
			joining = false;
		}
	}

	async function save(q: PollQuestion, a: PollAnswer) {
		answers[q.id] = a;
		saving[q.id] = 'saving';
		try {
			const r = await pollClient.answer(code, q.id, a);
			answers[q.id] = r.value;
			if (r.key) ownKeys[q.id] = r.key;
			if (r.score) ownScores[q.id] = r.score;
			else delete ownScores[q.id];
			saving[q.id] = 'saved';
		} catch (e) {
			saving[q.id] = 'error';
			toast(e instanceof ApiError ? (e.fields.value ?? e.message) : 'Could not save', 'error');
		}
	}
	async function upload(q: PollQuestion, file: Blob, name: string, onProgress: (p: number) => void) {
		saving[q.id] = 'saving';
		try {
			const r = await pollClient.upload(code, q.id, file, name, onProgress);
			answers[q.id] = r.value;
			saving[q.id] = 'saved';
			toast('Uploaded');
		} catch (e) {
			saving[q.id] = 'error';
			throw e;
		}
	}

	// "After answering" is applied here, to this participant's own view.
	function visibleResult(q: PollQuestion) {
		if (!v?.results?.[q.id]) return undefined;
		if (v.show_results === 'after_answer' && v.status !== 'closed' && !hasAnswer(q, answers[q.id])) return undefined;
		if (v.show_results === 'presenter' && !(v.revealed && v.questions[0]?.id === q.id)) return undefined;
		return v.results[q.id];
	}
	const answeredCount = $derived(v ? v.questions.filter((q) => hasAnswer(q, answers[q.id])).length : 0);
	const requiredLeft = $derived(v ? v.questions.filter((q) => q.required && !hasAnswer(q, answers[q.id])).length : 0);
	const canEdit = (q: PollQuestion) => v?.status === 'open' && left(q) !== 0 && (v.allow_edit || !hasAnswer(q, v.answers?.[q.id])) && !(v.scoring && keyOf(q));
	const myPoints = $derived(Object.values({ ...(v?.scores ?? {}), ...ownScores }).reduce((s, x) => s + x.points, 0));
</script>

<svelte:head><title>{v ? v.title : 'Poll'} · Classroom Quiz</title></svelte:head>

<div class="poll-page">
	{#if notFound}
		<div class="narrow"><div class="card card-border bg-base-100"><EmptyState icon="alert" title="No poll with that code">Check the code with your presenter, or <a href="/join">enter it again</a>.</EmptyState></div></div>
	{:else if notOpen}
		<div class="narrow center" in:flyIn>
			<div class="pulse-dot mx-auto mb-4" aria-hidden="true"><span></span></div>
			<h1>Not started yet</h1>
			<p class="muted">This page updates by itself when the presenter opens the poll.</p>
		</div>
	{:else if !v}
		<div class="page-container"><Skeleton lines={4} /></div>
	{:else if !v.joined}
		<div class="narrow" in:flyIn>
			<div class="card card-border vstack bg-base-100 p-5 shadow-sm sm:p-7">
				<span class="mx-auto grid size-14 place-items-center rounded-2xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="chart" size={28} /></span>
				<h1 class="center">{v.title}</h1>
				{#if v.scoring && v.status !== 'closed'}
					<div class="privacy"><Icon name="trophy" size={20} /><span><strong>Answers score points.</strong> {v.leaderboard_mode === 'everyone' ? 'Everyone sees a live leaderboard.' : v.leaderboard_mode === 'presenter' ? 'The presenter shows the leaderboard.' : 'Only the presenter sees the scores.'}</span></div>
					{#if v.leaderboard_mode !== 'off'}
						<div>
							<label for="nick">Nickname for the leaderboard <span class="muted font-normal">(optional)</span></label>
							<input id="nick" class="input w-full" maxlength="30" autocomplete="nickname" bind:value={nickname} placeholder="e.g. Quiz Whiz" />
							<p class="small muted m-0 mt-1">Keep it friendly; the presenter can change it.</p>
						</div>
					{/if}
				{/if}
				{#if v.status === 'closed'}
					<p class="alert alert-soft alert-warning m-0"><Icon name="info" />This poll has closed.</p>
				{:else if v.identity === 'anonymous'}
					<div class="privacy"><Icon name="eye-off" size={20} /><span><strong>Anonymous.</strong> Nobody, including the presenter, can see who gave which answer.</span></div>
					<button class="btn btn-primary btn-lg w-full" disabled={joining} onclick={() => join(false)}>Join the poll<Icon name="arrow-right" size={18} /></button>
				{:else if v.identity === 'identified'}
					<div class="privacy"><Icon name="user" size={20} /><span><strong>Your name is shown to the presenter</strong> next to your answers. Other participants never see it.{v.audience === 'classroom' ? ' Only students of the class can join.' : ''}</span></div>
					{#if auth.user}
						<button class="btn btn-primary btn-lg w-full" disabled={joining} onclick={() => join(true)}>Join as {auth.user.name}</button>
					{:else}
						<a class="btn btn-primary btn-lg w-full" href={loginUrl('/p/' + code)}>Log in to join</a>
					{/if}
				{:else}
					<div class="privacy"><Icon name="users" size={20} /><span><strong>Your choice.</strong> Join anonymously, or put your name to your answers for the presenter.</span></div>
					<button class="btn btn-primary btn-lg w-full" disabled={joining} onclick={() => join(false)}>Join anonymously</button>
					{#if auth.user}
						<button class="btn btn-lg w-full" disabled={joining} onclick={() => join(true)}>Join as {auth.user.name}</button>
					{:else}
						<p class="small muted center m-0">Want your name on your answers? <a href={loginUrl('/p/' + code)}>Log in first</a>.</p>
					{/if}
				{/if}
				{#if joinError}<p class="alert alert-soft alert-error m-0" role="alert"><Icon name="alert" />{joinError}</p>{/if}
			</div>
		</div>
	{:else}
		<header class="poll-bar">
			<div class="poll-bar-inner">
				<strong class="truncate">{v.title}</strong>
				<span class="spacer"></span>
				{#if v.scoring && (v.me || myPoints > 0)}
					<span class="badge badge-soft badge-primary gap-1 tabular" aria-live="polite"><Icon name="trophy" size={13} />{fmtPoints(v.me?.score ?? myPoints)} pts{#if v.me} · {ordinal(v.me.rank)}{/if}</span>
				{/if}
				<span class="badge badge-soft gap-1">{#if v.identified}<Icon name="user" size={13} />Named{:else}<Icon name="eye-off" size={13} />Anonymous{/if}</span>
				{#if v.pacing === 'self' && v.total > 0}<span class="badge badge-soft tabular">{answeredCount} / {v.total}</span>{/if}
			</div>
			{#if v.pacing === 'self' && v.total > 0}<progress class="progress progress-primary bar-progress" value={answeredCount} max={v.total} aria-label="{answeredCount} of {v.total} answered"></progress>{/if}
		</header>

		<main class="content">
			{#if v.status === 'closed'}
				<div class="alert alert-soft alert-info" in:flyIn><Icon name="info" /><span>This poll has closed. Thanks for taking part!</span></div>
			{/if}
			{#if v.pacing === 'presenter' && v.status === 'open'}
				<p class="small muted center m-0">Question {v.current_index + 1} of {v.total} · the presenter moves everyone on</p>
			{/if}
			{#each v.questions as q, i (q.id)}
				{@const result = visibleResult(q)}
				{@const secs = left(q)}
				{@const k = keyOf(q)}
				{@const sc = scoreOf(q)}
				<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6" in:flyIn={{ x: v.pacing === 'presenter' ? 30 : 0, y: v.pacing === 'presenter' ? 0 : 8, duration: 240 }}>
					<div class="mb-2 flex items-center gap-2 small">
						{#if v.pacing === 'self'}<span class="q-num">{i + 1}</span>{/if}
						{#if q.required}<span class="badge badge-soft badge-sm">required</span>{/if}
						{#if v.scoring && q.points && !k}<span class="badge badge-soft badge-sm tabular">{fmtPoints(q.points)} pts</span>{/if}
						{#if secs !== null && !k}<span class="timer tabular" class:low={secs <= 5} role="timer" aria-live={secs <= 5 ? 'assertive' : 'off'}><Icon name="clock" size={14} />{secs === 0 ? "Time's up" : secs + 's'}</span>{/if}
						<span class="spacer"></span>
						{#if saving[q.id] === 'saving'}<span class="muted flex items-center gap-1"><span class="loading loading-spinner loading-xs"></span>Saving…</span>
						{:else if saving[q.id] === 'error'}<span class="text-error flex items-center gap-1"><Icon name="alert" size={14} />Not saved</span>
						{:else if hasAnswer(q, answers[q.id])}<span class="text-success flex items-center gap-1" in:scaleIn><Icon name="check" size={14} />Saved</span>{/if}
					</div>
					<PollInput question={q} value={answers[q.id]} disabled={!canEdit(q)} onchange={(a) => save(q, a)} upload={(f, n, p) => upload(q, f, n, p)} />
					{#if !v.allow_edit && hasAnswer(q, v.answers?.[q.id]) && v.status === 'open'}<p class="small muted m-0 mt-2">Answers can't be changed in this poll.</p>{/if}
					{#if k}
						<div class="feedback" class:right={sc?.correct} class:wrong={sc && !sc.correct} in:scaleIn>
							{#if sc}
								<Icon name={sc.correct ? 'check-circle' : 'alert'} size={18} />
								<span><strong>{sc.correct ? 'Correct!' : sc.points > 0 ? 'Partly right' : 'Not quite'}</strong> +{fmtPoints(sc.points)} points{#if !sc.correct}<span class="block small">Answer: {describeKey(q, k)}</span>{/if}</span>
							{:else}
								<Icon name="info" size={18} /><span><strong>Answer:</strong> {describeKey(q, k)}</span>
							{/if}
						</div>
					{/if}
					{#if result}
						<div class="results-box" in:flyIn>
							<p class="small font-semibold m-0 mb-2 flex items-center gap-1"><Icon name="chart" size={14} />Results so far</p>
							<PollResults question={q} {result} />
						</div>
					{/if}
				</section>
			{:else}
				<div class="center py-10" in:flyIn>
					<div class="pulse-dot mx-auto mb-4" aria-hidden="true"><span></span></div>
					<p class="muted">Waiting for the presenter…</p>
				</div>
			{/each}
			{#if v.scoring && v.leaderboard_mode === 'everyone' && v.leaderboard?.length}
				<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6" aria-labelledby="lb-h">
					<h2 id="lb-h" class="m-0 mb-3 flex items-center gap-2 text-lg"><Icon name="trophy" size={18} />Leaderboard</h2>
					<Leaderboard ranks={v.leaderboard} me={v.me ?? null} meKey={v.me_key ?? ''} />
				</section>
			{/if}
			{#if v.pacing === 'self' && v.total > 0 && v.status === 'open'}
				<p class="small center m-0" class:text-success={!requiredLeft} class:muted={!!requiredLeft}>
					{#if requiredLeft}{requiredLeft} required question{requiredLeft === 1 ? '' : 's'} left · answers save as you go{:else}<Icon name="check-circle" size={14} /> Your answers are saved. You can close this page.{/if}
				</p>
			{/if}
		</main>
	{/if}
</div>

<style>
	.poll-page { min-height: 70vh; }
	.privacy { display: flex; gap: 0.75rem; align-items: flex-start; padding: 0.75rem 0.9rem; border-radius: var(--radius-box); background: var(--color-base-200); }
	.privacy :global(svg) { margin-top: 0.15rem; color: var(--color-primary); flex: none; }
	.poll-bar { position: sticky; top: 4rem; z-index: 4; background: color-mix(in oklab, var(--color-base-100) 92%, transparent); backdrop-filter: blur(8px); border-bottom: 1px solid var(--color-base-300); }
	.poll-bar-inner { display: flex; align-items: center; gap: 0.5rem; max-width: 48rem; margin: 0 auto; padding: 0.5rem 1rem; min-height: 3rem; }
	.bar-progress { display: block; height: 0.25rem; border-radius: 0; width: 100%; }
	.content { max-width: 48rem; margin: 0 auto; padding: 1rem; display: grid; gap: 1rem; }
	.q-num { width: 1.6rem; height: 1.6rem; border-radius: 999px; display: grid; place-items: center; font-weight: 700; background: var(--color-base-200); }
	.timer { display: inline-flex; align-items: center; gap: 0.25rem; font-weight: 700; padding: 0.1rem 0.5rem; border-radius: 999px; background: var(--color-base-200); }
	.timer.low { background: color-mix(in oklab, var(--color-error) 15%, transparent); color: var(--color-error); }
	.feedback { display: flex; gap: 0.6rem; align-items: flex-start; margin-top: 0.75rem; padding: 0.65rem 0.85rem; border-radius: var(--radius-box); background: var(--color-base-200); }
	.feedback :global(svg) { flex: none; margin-top: 0.1rem; }
	.feedback.right { background: color-mix(in oklab, var(--color-success) 14%, var(--color-base-100)); }
	.feedback.right :global(svg) { color: var(--color-success); }
	.feedback.wrong { background: color-mix(in oklab, var(--color-warning) 16%, var(--color-base-100)); }
	.results-box { margin-top: 1rem; padding-top: 1rem; border-top: 1px dashed var(--color-base-300); }
	.pulse-dot { width: 4rem; height: 4rem; border-radius: 999px; display: grid; place-items: center; background: color-mix(in oklab, var(--color-primary) 12%, transparent); }
	.pulse-dot span { width: 1.1rem; height: 1.1rem; border-radius: 999px; background: var(--color-primary); animation: breathe 1.8s ease-in-out infinite; }
	@keyframes breathe { 50% { transform: scale(1.35); opacity: 0.6; } }
</style>
