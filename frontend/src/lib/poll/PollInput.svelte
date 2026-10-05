<script lang="ts">
	// The participant's input for any of the 20 poll question types (D-40).
	// Quiz-style types reuse QuestionView; the rest have their own inputs.
	// Every change is reported; text inputs debounce.
	import QuestionView from '../QuestionView.svelte';
	import RichText from '../richtext/RichText.svelte';
	import type { Response, StudentQuestion } from '../types';
	import CodeInput from './inputs/CodeInput.svelte';
	import FileInput from './inputs/FileInput.svelte';
	import LikertInput from './inputs/LikertInput.svelte';
	import MatrixInput from './inputs/MatrixInput.svelte';
	import RatingInput from './inputs/RatingInput.svelte';
	import RecorderInput from './inputs/RecorderInput.svelte';
	import SliderInput from './inputs/SliderInput.svelte';
	import WordCloudInput from './inputs/WordCloudInput.svelte';
	import type { PollAnswer, PollQuestion } from './types';

	let {
		question,
		value,
		disabled = false,
		onchange,
		upload
	}: {
		question: PollQuestion;
		value?: PollAnswer;
		disabled?: boolean;
		onchange: (a: PollAnswer) => void;
		upload?: (file: Blob, name: string, onProgress: (p: number) => void) => Promise<void>;
	} = $props();

	const q = $derived(question);
	const b = $derived(q.body);
	const quizStyle = $derived(['MATCH', 'BLANK_OPT', 'BLANK_TEXT', 'DRAG'].includes(q.type) || ((q.type === 'SINGLE' && b.display !== 'dropdown') || q.type === 'MULTI'));
	const asStudent = $derived({ id: q.id, code: '', type: q.type, text: q.text, body: { ...b, blanks: b.blanks }, marks: 0, resources: [] } as unknown as StudentQuestion);
	const label = $derived(q.text.replace(/[*_`~[\]#>+$]/g, '').slice(0, 120));

	let timer: ReturnType<typeof setTimeout> | null = null;
	function later(a: PollAnswer) {
		if (timer) clearTimeout(timer);
		timer = setTimeout(() => onchange(a), 600);
	}
	let local = $state('');
	let localQ = '';
	$effect.pre(() => {
		if (q.id !== localQ) {
			localQ = q.id;
			local = value?.text ?? '';
		}
	});
	const words = $derived(local.trim() ? local.trim().split(/\s+/).length : 0);
</script>

<div class="poll-input vstack">
	{#if quizStyle}
		<QuestionView question={asStudent} value={(value ?? null) as Response | null} {disabled} onchange={(r) => onchange(r as PollAnswer)} />
		{#if q.type === 'MULTI' && b.max_choices}<p class="small muted m-0">Choose up to {b.max_choices}.</p>{/if}
	{:else}
		<RichText class="qtext" text={q.text} format={b.format} />
		{#if q.type === 'SINGLE'}
			<select class="select w-full" {disabled} aria-label={label} value={value?.selected?.[0] ?? ''} onchange={(e) => onchange(e.currentTarget.value ? { selected: [e.currentTarget.value] } : {})}>
				<option value="">Choose…</option>
				{#each b.options ?? [] as o (o.id)}<option value={o.id}>{o.text}</option>{/each}
			</select>
		{:else if q.type === 'SHORT_TEXT'}
			<input class="input w-full" {disabled} aria-label={label} maxlength={b.max_length ?? 200} value={local} oninput={(e) => { local = e.currentTarget.value; later({ text: local }); }} onblur={() => onchange({ text: local })} />
			<p class="small muted m-0 text-right tabular">{local.length} / {b.max_length ?? 200}</p>
		{:else if q.type === 'ESSAY'}
			<textarea class="textarea w-full" rows="6" {disabled} aria-label={label} value={local} oninput={(e) => { local = e.currentTarget.value; later({ text: local }); }} onblur={() => onchange({ text: local })}></textarea>
			<p class="small m-0 text-right tabular" class:text-error={!!b.max_words && words > b.max_words} class:muted={!b.max_words || words <= b.max_words}>{words} word{words === 1 ? '' : 's'}{b.max_words ? ` of ${b.max_words}` : ''}</p>
		{:else if q.type === 'CODE'}
			<CodeInput value={value?.text ?? ''} language={b.language} maxLength={b.max_length ?? 10000} {disabled} {label} onchange={(t) => onchange({ text: t })} />
		{:else if q.type === 'WORD_CLOUD'}
			<WordCloudInput max={b.max_entries ?? 3} value={value?.words ?? []} {disabled} {label} onchange={(w) => onchange({ words: w })} />
		{:else if q.type === 'NUMBER'}
			<div class="join w-full max-w-xs">
				<input
					class="input join-item w-full"
					type="number"
					inputmode="decimal"
					{disabled}
					aria-label={label}
					min={b.min}
					max={b.max}
					step={b.step ?? 'any'}
					value={value?.number ?? ''}
					oninput={(e) => { const v = e.currentTarget.value; later(v === '' ? {} : { number: Number(v) }); }}
				/>
				{#if b.unit}<span class="join-item flex items-center border border-[var(--color-field)] bg-base-200 px-3">{b.unit}</span>{/if}
			</div>
			{#if b.min !== undefined || b.max !== undefined}<p class="small muted m-0">{b.min !== undefined && b.max !== undefined ? `Between ${b.min} and ${b.max}` : b.min !== undefined ? `At least ${b.min}` : `At most ${b.max}`}</p>{/if}
		{:else if q.type === 'DATE'}
			<input class="input w-full max-w-xs" type="date" {disabled} aria-label={label} min={b.from} max={b.to} value={value?.date ?? ''} onchange={(e) => onchange(e.currentTarget.value ? { date: e.currentTarget.value } : {})} />
		{:else if q.type === 'TIME'}
			<input class="input w-full max-w-xs" type="time" {disabled} aria-label={label} min={b.from} max={b.to} value={value?.time ?? ''} onchange={(e) => onchange(e.currentTarget.value ? { time: e.currentTarget.value } : {})} />
		{:else if q.type === 'RATING'}
			<RatingInput points={b.points ?? 5} value={value?.number} {disabled} {label} onchange={(n) => onchange(n === undefined ? {} : { number: n })} />
		{:else if q.type === 'SLIDER'}
			<SliderInput min={b.min ?? 0} max={b.max ?? 100} step={b.step ?? 1} minLabel={b.min_label} maxLabel={b.max_label} value={value?.number} {disabled} {label} onchange={(n) => later(n === undefined ? {} : { number: n })} />
		{:else if q.type === 'LIKERT'}
			<LikertInput rows={b.rows ?? []} scale={b.scale ?? []} value={value?.rows ?? {}} {disabled} name={'lk' + q.id} onchange={(r) => onchange({ rows: r })} />
		{:else if q.type === 'MATRIX'}
			<MatrixInput rows={b.rows ?? []} columns={b.columns ?? []} mode={b.mode} value={value ?? {}} {disabled} name={'mx' + q.id} {onchange} />
		{:else if q.type === 'FILE'}
			<FileInput accept={b.accept} maxMB={b.max_mb ?? 5} value={value?.file} {disabled} {label} upload={(f, p) => upload!(f, f.name, p)} />
		{:else if q.type === 'AUDIO' || q.type === 'VIDEO'}
			<RecorderInput kind={q.type === 'AUDIO' ? 'audio' : 'video'} maxSeconds={b.max_seconds ?? 30} value={value?.file} {disabled} {label} upload={(blob, name, p) => upload!(blob, name, p)} />
		{/if}
	{/if}
</div>

<style>
	.poll-input :global(.qtext) { font-size: 1.15rem; line-height: 1.5; }
</style>
