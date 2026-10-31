<script lang="ts">
	// A student working through a tutorial (D-74): one page at a time, with
	// reading, questions, checks and feedback as the teacher set them. Time
	// on each page and leaving the app are recorded as the page says (D-75).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { describeKey } from '$lib/answerText';
	import QuestionView from '$lib/QuestionView.svelte';
	import { auth, loginUrl } from '$lib/session.svelte';
	import Blocks from '$lib/tutorial/Blocks.svelte';
	import { changed, checkpointNote, leavingNote, readingMinutes, type RunView } from '$lib/tutorial/tutorial';
	import { cascade } from '$lib/ui/motion';
	import { i18n, t } from '$lib/i18n/i18n.svelte';
	import { PageWatch } from '$lib/tutorial/watch';
	import type { Response } from '$lib/types';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { count } from '$lib/words';

	const id = $derived(appPage.params.id ?? '');
	let v = $state<RunView | null>(null);
	let problem = $state('');
	let busy = $state(false);
	let drafts = $state<Record<string, Response | undefined>>({});

	function show(next: RunView) {
		v = next;
		polls = 0;
		drafts = Object.fromEntries((next.page?.questions ?? []).map((q) => [q.id, q.response ? $state.snapshot(q.response) : undefined]));
		if (typeof window !== 'undefined') window.scrollTo({ top: 0 });
	}
	// refresh takes the server's state without losing answers being written.
	function refresh(next: RunView) {
		const keep = drafts;
		v = next;
		drafts = Object.fromEntries((next.page?.questions ?? []).map((q) => [q.id, keep[q.id] ?? (q.response ? $state.snapshot(q.response) : undefined)]));
	}

	// ---- time on the page, and leaving the app ----
	let watch: PageWatch | null = null;
	function sendTime(seconds: number, keepalive: boolean) {
		if (!v?.page) return;
		fetch(`/api/tutorials/${id}/time`, {
			method: 'POST', keepalive, credentials: 'same-origin',
			headers: { 'X-Requested-With': 'fetch', 'Content-Type': 'application/json' },
			body: JSON.stringify({ page: v.page.index, seconds })
		}).catch(() => {});
	}
	async function left(away: number, viaLink: boolean) {
		const before = v?.page;
		if (!before) return;
		try {
			const next = await api.post<RunView>(`/api/tutorials/${id}/leave`, { page: before.index, away_seconds: away, via_link: viaLink });
			refresh(next);
			const p = next.page;
			if (!p || viaLink || p.leaving === 'allowed' || p.index !== before.index) return;
			if (p.locked && !before.locked) toast('You left the app too often: this page is locked until your teacher unlocks it', 'error');
			else if (p.leaving === 'blocked' && p.lock_after && !p.locked) toast(`You left the app: ${p.leaves} of ${p.lock_after} times before this page locks`, 'error');
			else if (!p.locked) toast('You left the app: your teacher will see it', 'info');
		} catch {
			/* recorded next time, or not at all: it's evidence, not a gate */
		}
	}
	onMount(() => {
		watch = new PageWatch({ time: sendTime, leave: left, active: () => !!v?.page && !v.read_only });
		watch.start();
		return () => watch?.stop();
	});

	// ---- answers the AI is marking: look again for a couple of minutes ----
	let polls = 0;
	$effect(() => {
		const p = v?.page;
		if (!p || !p.questions.some((q) => q.mark?.marking) || polls >= 30) return;
		const t = setTimeout(() => {
			polls++;
			api.get<RunView>(`/api/tutorials/${id}/pages/${p.index}`).then(refresh, () => {});
		}, 4000);
		return () => clearTimeout(t);
	});
	$effect(() => {
		if (!auth.loaded || v || problem) return;
		if (!auth.user) {
			goto(loginUrl('/learn/' + id), { replaceState: true });
			return;
		}
		api.post<RunView>('/api/tutorials/' + id + '/open').then(show, (e) => (problem = e instanceof ApiError ? e.message : 'Could not open the tutorial'));
	});

	async function act(f: () => Promise<RunView>) {
		watch?.flush(); // the time so far belongs to the page being left
		busy = true;
		try {
			show(await f());
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Something went wrong; try again', 'error');
		} finally {
			busy = false;
		}
	}
	const go = (to: number) => act(() => api.post<RunView>('/api/tutorials/' + id + '/go', { to }));
	const read = (n: number) => act(() => api.get<RunView>(`/api/tutorials/${id}/pages/${n}`));
	const finish = () => act(() => api.post<RunView>('/api/tutorials/' + id + '/finish'));
	async function check() {
		if (!v?.page) return;
		const responses = changed(v.page.questions, drafts);
		if (!Object.keys(responses).length) return toast('Answer a question first', 'info');
		await act(() => api.post<RunView>('/api/tutorials/' + id + '/answers', { page: v!.page!.index, responses }));
	}

	const p = $derived(v?.page);
	const last = $derived(!!v && !!p && p.index === v.pages.length - 1);
	const unchecked = $derived(p ? Object.keys(changed(p.questions, drafts)).length : 0);
	const note = $derived(p && !p.locked ? checkpointNote(p) : '');
	const leaveNote = $derived(p && v && !v.read_only ? leavingNote(p) : '');
	const canGoOn = $derived(!!p && (v!.read_only || (p.passed && unchecked === 0) || (p.passed && p.checkpoint === 'none')));
	const pct = $derived(v ? Math.round(((v.finished ? v.pages.length : v.current) / Math.max(1, v.pages.length)) * 100) : 0);
	// A page dot: reading only once nothing more can be answered; otherwise a move.
	function visit(n: number) {
		if (!v || !p || n === p.index) return;
		return v.read_only ? read(n) : go(n);
	}
</script>

<svelte:head><title>{v?.tutorial.title ?? 'Tutorial'}</title></svelte:head>

<div class="page-container learn">
	{#if problem}
		<div class="card card-border bg-base-100 shadow-sm p-6 text-center vstack" role="alert">
			<h1 class="m-0">{t('read.cantOpen')}</h1>
			<p class="muted m-0">{problem}</p>
			<a class="btn mx-auto" href="/my">{t('read.backToMy')}</a>
		</div>
	{:else if !v || !p}
		{#if v && !v.pages.length}<p class="muted">{t('read.noPages')}</p>{:else}<Skeleton lines={8} />{/if}
	{:else}
		<header class="vstack gap-2">
			<p class="small m-0"><a href="/my">{t('read.myPage')}</a></p>
			<!-- A newspaper's masthead (D-107): kicker, headline, standfirst, byline, double rule. -->
			<div class="masthead">
				{#if v.tutorial.classroom || v.tutorial.topic}<p class="kicker">{[v.tutorial.classroom, v.tutorial.topic].filter(Boolean).join(' · ')}</p>{/if}
				<h1 class="headline">{v.tutorial.title}</h1>
				{#if v.tutorial.description}<p class="standfirst">{v.tutorial.description}</p>{/if}
				<p class="byline">
					{#if v.tutorial.teacher}<span>{t('read.by', { name: v.tutorial.teacher })}</span>{/if}
					{#if v.tutorial.published_at}<time datetime={v.tutorial.published_at}>{new Date(v.tutorial.published_at).toLocaleDateString(i18n.tag, { day: 'numeric', month: 'long', year: 'numeric' })}</time>{/if}
					<span>{t('read.minutes', { n: readingMinutes(p.blocks) })}</span>
					{#if v.score}<span class="tabular">{t('read.score', { score: v.score.score, max: v.score.max })}{v.score.pending ? ` · ${v.score.pending} waiting for your teacher` : ''}</span>{/if}
				</p>
			</div>
			<div class="progress-row">
				<progress class="progress progress-primary flex-1" value={pct} max="100" aria-label="Progress: {pct}%"></progress>
				<span class="small muted tabular">{v.finished ? t('read.finished') : t('read.pageOf', { page: p.index + 1, pages: v.pages.length })}</span>
			</div>
			<nav class="steps-nav" aria-label={t('read.pages')}>
				{#each v.pages as s, i (i)}
					<button class="dot" class:here={i === p.index} class:done={s.done && s.questions} disabled={!s.open || busy} aria-current={i === p.index ? 'step' : undefined}
						title={(s.title || `Page ${i + 1}`) + (s.open ? '' : ' (not open yet)')} onclick={() => visit(i)}>{i + 1}</button>
				{/each}
			</nav>
			{#if v.past_due && !v.finished}<p class="alert alert-soft alert-warning m-0">{t('read.pastDue')}</p>{/if}
			{#if v.finished}
				<p class="alert alert-soft alert-success m-0"><Icon name="check-circle" size={18} />
					{v.score ? `Finished. Your score: ${v.score.score} out of ${v.score.max}.` : v.tutorial.feedback === 'release' ? 'Finished. Your teacher will release your marks.' : 'Finished.'}
					{#if v.score?.pending} {count(v.score.pending, 'answer')} still waiting for your teacher.{/if}</p>
			{/if}
		</header>

		{#key p.index}
		<article class="paper vstack gap-5" use:cascade={{ y: 12, stagger: 0.05 }}>
			{#if p.title}<h2 class="page-title">{p.title}</h2>{/if}
			{#if leaveNote}
				<p class="alert alert-soft m-0" class:alert-error={p.locked} class:alert-warning={!p.locked && p.leaving === 'blocked'} class:alert-info={!p.locked && p.leaving === 'recorded'} role={p.locked ? 'alert' : undefined}>
					<Icon name={p.locked || p.leaving === 'blocked' ? 'alert' : 'eye'} size={18} />{leaveNote}</p>
			{/if}
			{#if p.blocks.length}<Blocks blocks={p.blocks} onlink={() => watch?.viaLink()} />{/if}
			{#each p.questions as q, i (q.id)}
				<section class="question" aria-labelledby="q-{q.id}">
					<p class="small muted m-0" id="q-{q.id}">Question {i + 1} · {count(q.marks, 'mark')}{q.tries > 1 ? ` · try ${q.tries}` : ''}</p>
					<QuestionView question={q} value={drafts[q.id] ?? null} disabled={q.locked || busy} onchange={(r) => (drafts[q.id] = r)} />
					{#if q.mark}
						<div class="mark" class:right={q.mark.correct} class:pending={q.mark.pending} role="status">
							<Icon name={q.mark.pending ? 'clock' : q.mark.correct ? 'check-circle' : 'alert'} size={18} />
							<div class="min-w-0">
								<strong>{q.mark.marking ? 'Being marked…' : q.mark.pending ? 'Your teacher will mark this' : q.mark.correct ? 'Right' : q.locked ? 'Not quite' : 'Not quite: try again'}</strong>
								{#if !q.mark.pending}<span class="small tabular"> · {q.mark.score} / {q.mark.max}</span>{/if}
								{#if q.mark.feedback}<p class="m-0 mt-1">{q.mark.feedback}</p>{/if}
								{#if q.mark.key && !q.mark.correct}<p class="small m-0 mt-1">The right answer: {describeKey(q.type, q.body, q.mark.key)}</p>{/if}
								{#if q.mark.comment}<p class="small m-0 mt-1"><strong>Your teacher:</strong> {q.mark.comment}</p>{/if}
							</div>
						</div>
					{:else if q.checked && !v.read_only}
						<p class="small muted m-0"><Icon name="check" size={14} /> Answer saved{v.tutorial.feedback === 'end' ? '; you’ll see how you did at the end' : v.tutorial.feedback === 'release' ? '; your teacher will release the marks' : ''}.</p>
					{/if}
				</section>
			{/each}
		</article>
		{/key}

		<div class="controls">
			{#if p.index > 0 && (v.tutorial.allow_back || v.read_only)}
				<button class="btn" disabled={busy} onclick={() => (v!.read_only ? read(p.index - 1) : go(p.index - 1))}><Icon name="arrow-left" size={16} />{t('read.back')}</button>
			{/if}
			<span class="spacer"></span>
			{#if note && !v.read_only}<span class="small muted">{note}</span>{/if}
			{#if !v.read_only && p.questions.some((q) => !q.locked) && unchecked > 0}
				<button class="btn btn-primary" disabled={busy} onclick={check}><Icon name="check" size={16} />{v.tutorial.feedback === 'page' ? t('read.check') : t('read.saveAnswers')}</button>
			{:else if !last}
				<button class="btn btn-primary" disabled={busy || !canGoOn} onclick={() => (v!.read_only ? read(p.index + 1) : go(p.index + 1))}>{t('read.next')}<Icon name="arrow-right" size={16} /></button>
			{:else if !v.finished && !v.read_only}
				<button class="btn btn-primary" disabled={busy || !canGoOn} onclick={finish}><Icon name="flag" size={16} />{t('read.finish')}</button>
			{/if}
		</div>
	{/if}
</div>

<style>
	.learn { max-width: 1040px; display: grid; gap: 1rem; }
	/* Newspaper style (D-107): serif type, a masthead, ruled columns for long
	   text on wide screens, a drop cap, square everything. Sinhala and Tamil
	   fall back to their own serif faces. */
	.learn { --paper-serif: 'Iowan Old Style', 'Charter', 'Georgia', 'Cambria', 'Noto Serif Sinhala', 'Iskoola Pota', 'Noto Serif Tamil', 'Latha', serif; }
	.masthead { border-top: 4px solid var(--color-base-content); border-bottom: 1px solid var(--color-base-content); box-shadow: 0 3px 0 var(--color-base-200), 0 4px 0 var(--color-base-content); padding: 0.9rem 0 0.8rem; margin-bottom: 0.4rem; display: grid; gap: 0.45rem; }
	.kicker { margin: 0; font-size: 0.75rem; font-weight: 700; letter-spacing: 0.14em; text-transform: uppercase; color: var(--color-primary); }
	.headline { margin: 0; font-family: var(--paper-serif); font-size: clamp(1.9rem, 4.2vw, 3.1rem); line-height: 1.08; font-weight: 700; letter-spacing: -0.015em; text-wrap: balance; }
	.standfirst { margin: 0; font-family: var(--paper-serif); font-size: clamp(1.05rem, 1.8vw, 1.3rem); line-height: 1.45; font-style: italic; color: color-mix(in oklab, var(--color-base-content) 78%, transparent); max-width: 46rem; text-wrap: pretty; }
	.byline { margin: 0; display: flex; flex-wrap: wrap; gap: 0 1rem; font-size: 0.82rem; color: color-mix(in oklab, var(--color-base-content) 70%, transparent); text-transform: uppercase; letter-spacing: 0.06em; }
	.byline > :global(* + *)::before { content: '·'; margin-right: 1rem; opacity: 0.6; }
	.paper { background: var(--color-base-100); border: 1px solid var(--color-base-300); padding: clamp(1rem, 3vw, 2.5rem); }
	.page-title { margin: 0; font-family: var(--paper-serif); font-size: clamp(1.4rem, 2.6vw, 1.9rem); line-height: 1.2; padding-bottom: 0.5rem; border-bottom: 1px solid var(--color-base-300); }
	.paper :global(.text .rich) { font-family: var(--paper-serif); font-size: 1.12rem; line-height: 1.72; hyphens: auto; text-align: start; text-wrap: pretty; }
	.paper :global(.text .rich :is(h1, h2, h3, h4)) { font-family: var(--paper-serif); line-height: 1.2; text-align: left; break-after: avoid; }
	.paper :global(.text .rich h2) { font-size: 1.45rem; border-top: 2px solid var(--color-base-content); padding-top: 0.4rem; }
	.paper :global(.text .rich h3) { font-size: 1.2rem; font-variant: small-caps; letter-spacing: 0.03em; }
	.paper :global(.text .rich :is(ul, ol, pre, table, img, figure)) { break-inside: avoid; text-align: left; }
	.paper :global(.text .rich blockquote) { margin: 1.2rem 0; padding: 0.8rem 0; border: 0; border-top: 3px solid var(--color-base-content); border-bottom: 1px solid var(--color-base-content); font-size: 1.35rem; line-height: 1.4; font-style: italic; text-align: left; break-inside: avoid; }
	/* The drop cap: the page's first paragraph. */
	.paper :global(.blocks > .text:first-child .rich > p:first-child::first-letter) { float: left; font-family: var(--paper-serif); font-size: 3.6em; line-height: 0.82; font-weight: 700; padding: 0.06em 0.12em 0 0; color: var(--color-primary); }
	@media (min-width: 900px) {
		.paper :global(.text.long .rich) { column-count: 2; column-gap: 2.5rem; column-rule: 1px solid var(--color-base-300); }
	}
	.paper :global(figcaption) { text-align: left; font-family: var(--paper-serif); font-style: italic; font-size: 0.9rem; border-bottom: 1px solid var(--color-base-300); padding-bottom: 0.35rem; }
	.progress-row { display: flex; align-items: center; gap: 0.75rem; }
	.steps-nav { display: flex; flex-wrap: wrap; gap: 0.35rem; }
	.dot { width: 2rem; height: 2rem; border: 1px solid var(--color-base-300); background: var(--color-base-100); font-size: 0.8rem; font-weight: 700; cursor: pointer; }
	.dot.done { background: color-mix(in oklab, var(--color-success) 18%, var(--color-base-100)); border-color: var(--color-success); }
	.dot.here { background: var(--color-primary); color: var(--color-primary-content); border-color: var(--color-primary); }
	.dot:disabled { opacity: 0.45; cursor: not-allowed; }
	.question { display: grid; gap: 0.6rem; padding-top: 1rem; border-top: 2px solid var(--color-base-content); }
	.mark { display: flex; gap: 0.6rem; align-items: flex-start; padding: 0.75rem 0.9rem; border-radius: 0.75rem; background: color-mix(in oklab, var(--color-error) 10%, var(--color-base-100)); }
	.mark.right { background: color-mix(in oklab, var(--color-success) 12%, var(--color-base-100)); }
	.mark.pending { background: var(--color-base-200); }
	.mark :global(svg) { flex: none; margin-top: 0.15rem; }
	.controls { position: sticky; bottom: 0.5rem; display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; padding: 0.6rem 0.75rem; border-radius: 1rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); box-shadow: 0 4px 16px rgb(0 0 0 / 0.08); }
</style>
