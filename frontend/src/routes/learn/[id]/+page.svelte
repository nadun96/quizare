<script lang="ts">
	// A student working through a tutorial (D-74): one page at a time, with
	// reading, questions, checks and feedback as the teacher set them.
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { describeKey } from '$lib/answerText';
	import QuestionView from '$lib/QuestionView.svelte';
	import { auth, loginUrl } from '$lib/session.svelte';
	import Blocks from '$lib/tutorial/Blocks.svelte';
	import { changed, checkpointNote, type RunView } from '$lib/tutorial/tutorial';
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
		drafts = Object.fromEntries((next.page?.questions ?? []).map((q) => [q.id, q.response ? $state.snapshot(q.response) : undefined]));
		if (typeof window !== 'undefined') window.scrollTo({ top: 0 });
	}
	$effect(() => {
		if (!auth.loaded || v || problem) return;
		if (!auth.user) {
			goto(loginUrl('/learn/' + id), { replaceState: true });
			return;
		}
		api.post<RunView>('/api/tutorials/' + id + '/open').then(show, (e) => (problem = e instanceof ApiError ? e.message : 'Could not open the tutorial'));
	});

	async function act(f: () => Promise<RunView>) {
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
	const note = $derived(p ? checkpointNote(p) : '');
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
			<h1 class="m-0">Can’t open this tutorial</h1>
			<p class="muted m-0">{problem}</p>
			<a class="btn mx-auto" href="/my">Back to my page</a>
		</div>
	{:else if !v || !p}
		{#if v && !v.pages.length}<p class="muted">This tutorial has no pages yet.</p>{:else}<Skeleton lines={8} />{/if}
	{:else}
		<header class="vstack gap-2">
			<p class="small m-0"><a href="/my">← My page</a></p>
			<div class="flex flex-wrap items-baseline gap-3">
				<h1 class="m-0">{v.tutorial.title}</h1>
				{#if v.score}<span class="badge badge-soft tabular">{v.score.score} / {v.score.max}{v.score.pending ? ` · ${v.score.pending} waiting for your teacher` : ''}</span>{/if}
			</div>
			<div class="progress-row">
				<progress class="progress progress-primary flex-1" value={pct} max="100" aria-label="Progress: {pct}%"></progress>
				<span class="small muted tabular">{v.finished ? 'Finished' : `Page ${p.index + 1} of ${v.pages.length}`}</span>
			</div>
			<nav class="steps-nav" aria-label="Pages">
				{#each v.pages as s, i (i)}
					<button class="dot" class:here={i === p.index} class:done={s.done && s.questions} disabled={!s.open || busy} aria-current={i === p.index ? 'step' : undefined}
						title={(s.title || `Page ${i + 1}`) + (s.open ? '' : ' (not open yet)')} onclick={() => visit(i)}>{i + 1}</button>
				{/each}
			</nav>
			{#if v.past_due && !v.finished}<p class="alert alert-soft alert-warning m-0">This tutorial is past its due date: you can read it, but not answer.</p>{/if}
			{#if v.finished}
				<p class="alert alert-soft alert-success m-0"><Icon name="check-circle" size={18} />
					{v.score ? `Finished. Your score: ${v.score.score} out of ${v.score.max}.` : v.tutorial.feedback === 'release' ? 'Finished. Your teacher will release your marks.' : 'Finished.'}
					{#if v.score?.pending} {count(v.score.pending, 'answer')} still waiting for your teacher.{/if}</p>
			{/if}
		</header>

		<article class="card card-border bg-base-100 shadow-sm p-4 sm:p-7 vstack gap-5">
			{#if p.title}<h2 class="m-0">{p.title}</h2>{/if}
			{#if p.blocks.length}<Blocks blocks={p.blocks} />{/if}
			{#each p.questions as q, i (q.id)}
				<section class="question" aria-labelledby="q-{q.id}">
					<p class="small muted m-0" id="q-{q.id}">Question {i + 1} · {count(q.marks, 'mark')}{q.tries > 1 ? ` · try ${q.tries}` : ''}</p>
					<QuestionView question={q} value={drafts[q.id] ?? null} disabled={q.locked || busy} onchange={(r) => (drafts[q.id] = r)} />
					{#if q.mark}
						<div class="mark" class:right={q.mark.correct} class:pending={q.mark.pending} role="status">
							<Icon name={q.mark.pending ? 'clock' : q.mark.correct ? 'check-circle' : 'alert'} size={18} />
							<div class="min-w-0">
								<strong>{q.mark.pending ? 'Your teacher will mark this' : q.mark.correct ? 'Right' : q.locked ? 'Not quite' : 'Not quite: try again'}</strong>
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

		<div class="controls">
			{#if p.index > 0 && (v.tutorial.allow_back || v.read_only)}
				<button class="btn" disabled={busy} onclick={() => (v!.read_only ? read(p.index - 1) : go(p.index - 1))}><Icon name="arrow-left" size={16} />Back</button>
			{/if}
			<span class="spacer"></span>
			{#if note && !v.read_only}<span class="small muted">{note}</span>{/if}
			{#if !v.read_only && p.questions.some((q) => !q.locked) && unchecked > 0}
				<button class="btn btn-primary" disabled={busy} onclick={check}><Icon name="check" size={16} />{v.tutorial.feedback === 'page' ? 'Check answers' : 'Save answers'}</button>
			{:else if !last}
				<button class="btn btn-primary" disabled={busy || !canGoOn} onclick={() => (v!.read_only ? read(p.index + 1) : go(p.index + 1))}>Next<Icon name="arrow-right" size={16} /></button>
			{:else if !v.finished && !v.read_only}
				<button class="btn btn-primary" disabled={busy || !canGoOn} onclick={finish}><Icon name="flag" size={16} />Finish</button>
			{/if}
		</div>
	{/if}
</div>

<style>
	.learn { max-width: 860px; display: grid; gap: 1rem; }
	.progress-row { display: flex; align-items: center; gap: 0.75rem; }
	.steps-nav { display: flex; flex-wrap: wrap; gap: 0.35rem; }
	.dot { width: 2rem; height: 2rem; border-radius: 999px; border: 1px solid var(--color-base-300); background: var(--color-base-100); font-size: 0.8rem; font-weight: 700; cursor: pointer; }
	.dot.done { background: color-mix(in oklab, var(--color-success) 18%, var(--color-base-100)); border-color: var(--color-success); }
	.dot.here { background: var(--color-primary); color: var(--color-primary-content); border-color: var(--color-primary); }
	.dot:disabled { opacity: 0.45; cursor: not-allowed; }
	.question { display: grid; gap: 0.6rem; padding-top: 1rem; border-top: 1px solid var(--color-base-300); }
	.mark { display: flex; gap: 0.6rem; align-items: flex-start; padding: 0.75rem 0.9rem; border-radius: 0.75rem; background: color-mix(in oklab, var(--color-error) 10%, var(--color-base-100)); }
	.mark.right { background: color-mix(in oklab, var(--color-success) 12%, var(--color-base-100)); }
	.mark.pending { background: var(--color-base-200); }
	.mark :global(svg) { flex: none; margin-top: 0.15rem; }
	.controls { position: sticky; bottom: 0.5rem; display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; padding: 0.6rem 0.75rem; border-radius: 1rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); box-shadow: 0 4px 16px rgb(0 0 0 / 0.08); }
</style>
