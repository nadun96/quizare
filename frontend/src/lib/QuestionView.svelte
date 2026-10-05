<script lang="ts">
	// Renders one question of any of the seven types (FR-QZ-03) and reports
	// every change, so answers save as they are given (NFR-11).
	import { onMount } from 'svelte';
	import Sortable from 'sortablejs';
	import type { Response, StudentQuestion } from './types';
	import RichText from './richtext/RichText.svelte';

	let {
		question,
		value = null,
		disabled = false,
		onchange
	}: { question: StudentQuestion; value?: Response | null; disabled?: boolean; onchange: (r: Response) => void } = $props();

	// Local working copy, reset when the question changes.
	let r = $state<Response>({});
	let qid = '';
	$effect.pre(() => {
		if (question.id !== qid) {
			qid = question.id;
			r = structuredClone($state.snapshot(value) ?? {}) as Response;
			if (question.type === 'DRAG' && !question.body.zones?.length && !r.order?.length) {
				r.order = (question.body.options ?? []).map((o) => o.id);
			}
		}
	});

	function emit() {
		onchange(structuredClone($state.snapshot(r)) as Response);
	}

	const opts = $derived(question.body.options ?? []);
	const optText = $derived(Object.fromEntries(opts.map((o) => [o.id, o.text])));
	const stemImages = $derived(question.resources.filter((x) => x.role === 'Q'));
	const optionImage = (i: number) => question.resources.find((x) => x.role === 'O' && x.n === i + 1);

	const isBlank = $derived(question.type === 'BLANK_OPT' || question.type === 'BLANK_TEXT');

	function toggle(id: string) {
		if (question.type === 'SINGLE') r.selected = [id];
		else {
			const s = new Set(r.selected ?? []);
			if (s.has(id)) s.delete(id);
			else s.add(id);
			r.selected = opts.map((o) => o.id).filter((x) => s.has(x));
		}
		emit();
	}

	function setPair(k: string, v: string) {
		r.pairs = { ...(r.pairs ?? {}), [k]: v };
		if (!v) delete r.pairs[k];
		emit();
	}

	function setBlank(b: string, v: string) {
		r.blanks = { ...(r.blanks ?? {}), [b]: v };
		emit();
	}

	let textTimer: ReturnType<typeof setTimeout> | null = null;
	function setText(v: string) {
		r.text = v;
		if (textTimer) clearTimeout(textTimer);
		textTimer = setTimeout(emit, 600); // essays: debounce keystrokes
	}
	const words = $derived((r.text ?? '').trim() ? (r.text ?? '').trim().split(/\s+/).length : 0);

	// Drag and drop (SortableJS: best mobile touch support, ADR-11).
	let orderList = $state<HTMLElement>();
	let zoneEls: Record<string, HTMLElement> = $state({});
	let poolEl = $state<HTMLElement>();
	let sortables: Sortable[] = [];

	function mountSortables() {
		for (const s of sortables) s.destroy();
		sortables = [];
		if (question.type !== 'DRAG' || disabled) return;
		if (!question.body.zones?.length && orderList) {
			sortables.push(
				Sortable.create(orderList, {
					animation: 150,
					onEnd: () => {
						r.order = [...orderList!.querySelectorAll<HTMLElement>('[data-id]')].map((el) => el.dataset.id!);
						emit();
					}
				})
			);
			return;
		}
		const lists = [poolEl, ...Object.values(zoneEls)].filter(Boolean) as HTMLElement[];
		for (const el of lists) {
			sortables.push(
				Sortable.create(el, {
					group: 'drag-' + question.id,
					animation: 150,
					onAdd: (ev) => {
						const id = (ev.item as HTMLElement).dataset.id!;
						const zone = (ev.to as HTMLElement).dataset.zone ?? '';
						setPair(id, zone);
					}
				})
			);
		}
	}
	onMount(() => () => sortables.forEach((s) => s.destroy()));
	$effect(() => {
		void question.id;
		queueMicrotask(mountSortables);
	});

	const placed = (zone: string) => opts.filter((o) => r.pairs?.[o.id] === zone);
	const unplaced = $derived(opts.filter((o) => !r.pairs?.[o.id]));
</script>

<div class="question vstack">
	{#if !isBlank}
		<RichText class="qtext" text={question.text} format={question.body.format} />
	{/if}
	{#each stemImages as img (img.id)}
		<img class="res" src={img.url} alt={img.alt_text} loading="eager" referrerpolicy="no-referrer" />
	{/each}

	{#if question.type === 'SINGLE' || question.type === 'MULTI'}
		<p class="muted small">{question.type === 'SINGLE' ? 'Choose one answer.' : 'Choose all that apply.'}</p>
		<div class="choices" role={question.type === 'SINGLE' ? 'radiogroup' : 'group'}>
			{#each opts as o, i (o.id)}
				<label class="choice" class:on={r.selected?.includes(o.id)}>
					<input
						class="sr-only"
						type={question.type === 'SINGLE' ? 'radio' : 'checkbox'}
						name={'q' + question.id}
						checked={r.selected?.includes(o.id) ?? false}
						{disabled}
						onchange={() => toggle(o.id)}
					/>
					<span class="letter" class:square={question.type === 'MULTI'} aria-hidden="true">{#if r.selected?.includes(o.id)}<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5" /></svg>{:else}{String.fromCharCode(65 + i)}{/if}</span>
					<span class="choice-text">{o.text}</span>
					{#if optionImage(i)}<img class="res small-res" src={optionImage(i)!.url} alt={optionImage(i)!.alt_text} referrerpolicy="no-referrer" />{/if}
				</label>
			{/each}
		</div>
	{:else if question.type === 'MATCH'}
		<p class="muted small">Match each item on the left with one on the right.</p>
		{#each question.body.left ?? [] as l (l.id)}
			<div class="match">
				<span>{l.text}</span>
				<select class="select w-full" aria-label={'Match for ' + l.text} value={r.pairs?.[l.id] ?? ''} {disabled} onchange={(e) => setPair(l.id, e.currentTarget.value)}>
					<option value="">Choose…</option>
					{#each question.body.right ?? [] as rt (rt.id)}<option value={rt.id}>{rt.text}</option>{/each}
				</select>
			</div>
		{/each}
	{:else if isBlank}
		<RichText class="qtext blanks" text={question.text} format={question.body.format}>
			{#snippet blank(b)}
				{#if question.type === 'BLANK_OPT'}
					<select class="select" aria-label={'Blank ' + b} value={r.blanks?.[b] ?? ''} {disabled} onchange={(e) => setBlank(b, e.currentTarget.value)}>
						<option value="">…</option>
						{#each opts as o (o.id)}<option value={o.id}>{o.text}</option>{/each}
					</select>
				{:else}
					<input class="blank" aria-label={'Blank ' + b} value={r.blanks?.[b] ?? ''} {disabled}
						autocomplete="off" autocapitalize="off" spellcheck="false"
						oninput={(e) => setBlank(b, e.currentTarget.value)} />
				{/if}
			{/snippet}
		</RichText>
	{:else if question.type === 'DRAG'}
		{#if !question.body.zones?.length}
			<p class="muted small">Drag the items into the right order.</p>
			<ol class="drag-list" bind:this={orderList}>
				{#each r.order ?? [] as id (id)}<li class="drag-item" data-id={id}>☰ {optText[id]}</li>{/each}
			</ol>
		{:else}
			<p class="muted small">Drag each item into the right box.</p>
			<div class="drag-pool" bind:this={poolEl} data-zone="">
				{#each unplaced as o (o.id)}<div class="drag-item" data-id={o.id}>☰ {o.text}</div>{/each}
			</div>
			<div class="zones">
				{#each question.body.zones as z (z.id)}
					<div class="zone">
						<strong>{z.text}</strong>
						<div class="zone-drop" bind:this={zoneEls[z.id]} data-zone={z.id}>
							{#each placed(z.id) as o (o.id)}<div class="drag-item" data-id={o.id}>☰ {o.text}</div>{/each}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	{:else if question.type === 'ESSAY'}
		<textarea class="textarea w-full essay" aria-label="Your answer" value={r.text ?? ''} {disabled} oninput={(e) => setText(e.currentTarget.value)} onblur={emit} rows="8"></textarea>
		<div class="flex items-center gap-3 small muted" class:over={!!question.body.word_limit && words > question.body.word_limit}>
			<span class="tabular" aria-live="polite">{words} word{words === 1 ? '' : 's'}{question.body.word_limit ? ` of ${question.body.word_limit}` : ''}</span>
			{#if question.body.word_limit}<progress class="progress flex-1" class:progress-error={words > question.body.word_limit} class:progress-primary={words <= question.body.word_limit} value={Math.min(words, question.body.word_limit)} max={question.body.word_limit} aria-hidden="true"></progress>{/if}
		</div>
	{/if}
</div>

<style>
	.question :global(.qtext) { font-size: 1.1rem; }
	.question :global(.blanks) { line-height: 2.6; }
	.question :global(.blanks select), .question :global(.blank) { width: auto; min-width: 7rem; max-width: 100%; display: inline-block; margin: 0 0.25rem; line-height: 1.3; padding: 0.35rem 0.6rem; min-height: 2.4rem; vertical-align: middle; }
	.res { max-width: 100%; max-height: 50vh; border-radius: 8px; }
	.small-res { max-height: 80px; }
	/* Answer tiles: 56 px targets, a letter marker (A, B, C…) so options can be
	   named aloud and told apart without colour, and a check when selected. */
	.choices { display: grid; gap: 0.625rem; }
	.choice { display: flex; gap: 0.875rem; align-items: center; font-weight: 400; font-size: 1.05rem; line-height: 1.45; padding: 0.75rem 1rem; border: 1.5px solid var(--color-base-300); border-radius: var(--radius-box); background: var(--color-base-100); min-height: 3.5rem; margin: 0; cursor: pointer; transition: border-color var(--motion-fast), background-color var(--motion-fast), transform var(--motion-fast) var(--ease-out), box-shadow var(--motion-fast); }
	.choice:hover { border-color: color-mix(in oklab, var(--color-primary) 45%, var(--color-base-300)); }
	.choice:active { transform: scale(0.99); }
	.choice:has(input:focus-visible) { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.choice:has(input:disabled) { cursor: default; opacity: 0.7; }
	.choice.on { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 7%, var(--color-base-100)); box-shadow: 0 0 0 1px var(--color-primary); }
	.letter { flex: none; width: 2rem; height: 2rem; display: grid; place-items: center; border-radius: 999px; font-weight: 700; font-size: 0.875rem; border: 1.5px solid var(--color-field); color: var(--color-muted); transition: background-color var(--motion-fast), color var(--motion-fast), transform var(--motion) var(--ease-out); }
	.letter.square { border-radius: 0.45rem; }
	.on .letter { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-primary-content); animation: pop var(--motion) var(--ease-out); }
	@keyframes pop { 0% { transform: scale(0.7); } 70% { transform: scale(1.12); } 100% { transform: scale(1); } }
	.choice-text { flex: 1; min-width: 0; overflow-wrap: anywhere; }
	.match { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 0.5rem 0.75rem; align-items: center; margin-bottom: 0.625rem; }
	@media (max-width: 480px) { .match { grid-template-columns: 1fr; gap: 0.25rem; margin-bottom: 1rem; } }
	.drag-list { padding: 0; list-style: none; display: grid; gap: 0.5rem; }
	.drag-item { padding: 0.75rem 1rem; min-height: 3rem; display: flex; align-items: center; border: 1.5px solid var(--color-base-300); border-radius: var(--radius-field); background: var(--color-base-100); cursor: grab; touch-action: none; user-select: none; transition: box-shadow var(--motion-fast), border-color var(--motion-fast); }
	.drag-item:hover { border-color: color-mix(in oklab, var(--color-primary) 45%, var(--color-base-300)); }
	:global(.sortable-chosen) { box-shadow: 0 8px 24px -8px color-mix(in oklab, var(--color-base-content) 35%, transparent); border-color: var(--color-primary) !important; }
	.drag-pool, .zone-drop { min-height: 3.5rem; padding: 0.5rem; border: 2px dashed var(--color-field); border-radius: var(--radius-box); display: grid; gap: 0.5rem; transition: background-color var(--motion-fast); }
	.zone-drop:has(:global(.sortable-ghost)) { background: color-mix(in oklab, var(--color-primary) 6%, transparent); }
	.essay { font-size: 1.05rem; line-height: 1.6; }
	.zones { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); }
	.over { color: var(--color-error); }
	:global(.sortable-ghost) { opacity: 0.4; }
</style>
