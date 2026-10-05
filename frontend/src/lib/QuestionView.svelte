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

<div class="question stack">
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
						type={question.type === 'SINGLE' ? 'radio' : 'checkbox'}
						name={'q' + question.id}
						checked={r.selected?.includes(o.id) ?? false}
						{disabled}
						onchange={() => toggle(o.id)}
					/>
					<span>{o.text}</span>
					{#if optionImage(i)}<img class="res small-res" src={optionImage(i)!.url} alt={optionImage(i)!.alt_text} referrerpolicy="no-referrer" />{/if}
				</label>
			{/each}
		</div>
	{:else if question.type === 'MATCH'}
		<p class="muted small">Match each item on the left with one on the right.</p>
		{#each question.body.left ?? [] as l (l.id)}
			<div class="match">
				<span>{l.text}</span>
				<select aria-label={'Match for ' + l.text} value={r.pairs?.[l.id] ?? ''} {disabled} onchange={(e) => setPair(l.id, e.currentTarget.value)}>
					<option value="">Choose…</option>
					{#each question.body.right ?? [] as rt (rt.id)}<option value={rt.id}>{rt.text}</option>{/each}
				</select>
			</div>
		{/each}
	{:else if isBlank}
		<RichText class="qtext blanks" text={question.text} format={question.body.format}>
			{#snippet blank(b)}
				{#if question.type === 'BLANK_OPT'}
					<select aria-label={'Blank ' + b} value={r.blanks?.[b] ?? ''} {disabled} onchange={(e) => setBlank(b, e.currentTarget.value)}>
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
		<textarea aria-label="Your answer" value={r.text ?? ''} {disabled} oninput={(e) => setText(e.currentTarget.value)} onblur={emit} rows="10"></textarea>
		<p class="small muted" class:over={!!question.body.word_limit && words > question.body.word_limit}>
			{words} words{question.body.word_limit ? ` of ${question.body.word_limit}` : ''}
		</p>
	{/if}
</div>

<style>
	.question :global(.qtext) { font-size: 1.1rem; }
	.question :global(.blanks) { line-height: 2.6; }
	.question :global(.blanks select), .question :global(.blank) { width: auto; min-width: 7rem; max-width: 100%; display: inline-block; margin: 0 0.25rem; line-height: 1.3; padding: 0.35rem 0.6rem; min-height: 2.4rem; vertical-align: middle; }
	.res { max-width: 100%; max-height: 50vh; border-radius: 8px; }
	.small-res { max-height: 80px; }
	.choices { display: grid; gap: 0.5rem; }
	.choice { display: flex; gap: 0.75rem; align-items: center; font-weight: 400; padding: 0.75rem; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); min-height: 48px; }
	.choice.on { border-color: var(--primary); box-shadow: inset 0 0 0 1px var(--primary); }
	.match { display: grid; grid-template-columns: 1fr 1fr; gap: 0.5rem; align-items: center; margin-bottom: 0.5rem; }
	.drag-list { padding: 0; list-style: none; display: grid; gap: 0.5rem; }
	.drag-item { padding: 0.75rem; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); cursor: grab; touch-action: none; user-select: none; }
	.drag-pool, .zone-drop { min-height: 56px; padding: 0.5rem; border: 2px dashed var(--border); border-radius: 8px; display: grid; gap: 0.5rem; }
	.zones { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); }
	.over { color: var(--danger); }
	:global(.sortable-ghost) { opacity: 0.4; }
</style>
