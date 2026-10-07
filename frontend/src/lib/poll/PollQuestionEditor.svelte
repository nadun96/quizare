<script lang="ts">
	// Create or edit one poll question of any type, with a live preview that
	// uses the participant's real input (D-40).
	import { api, ApiError } from '../api';
	import RichTextEditor from '../richtext/RichTextEditor.svelte';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import ChoiceList from './ChoiceList.svelte';
	import KeyEditor from './KeyEditor.svelte';
	import { TYPE_META, TYPES_BY_GROUP, withIds } from './meta';
	import PollInput from './PollInput.svelte';
	import { cleanKey, SCORABLE } from './scoring';
	import type { PollAnswer, PollBody, PollKey, PollQuestion, PollType } from './types';

	let { pollId, question = null, scoring = false, pacing = 'self', extra = {}, onsaved, oncancel }: {
		pollId: string;
		question?: PollQuestion | null;
		/** The poll scores answers: offer a key and points (D-42). */
		scoring?: boolean;
		pacing?: 'self' | 'presenter';
		/** Sent with a new question, e.g. live placement (V2-05). */
		extra?: Record<string, unknown>;
		onsaved: (q: PollQuestion) => void;
		oncancel: () => void;
	} = $props();

	// svelte-ignore state_referenced_locally
	const initial = question;
	let type = $state<PollType | null>(initial?.type ?? null);
	let text = $state(initial?.text ?? '');
	let required = $state(initial?.required ?? false);
	let body = $state<PollBody>(structuredClone($state.snapshot(initial?.body) ?? {}));
	const format = initial?.body?.format ?? '';
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	let previewValue = $state<PollAnswer>({});
	let key = $state<PollKey>(structuredClone($state.snapshot(initial?.key) ?? {}));
	let points = $state(initial?.points ?? 100);
	let timeLimit = $state<number | null>(initial?.time_limit_sec ?? null);

	function choose(t: PollType) {
		type = t;
		key = {};
		body = TYPE_META[t].body();
		if (!text.trim()) text = TYPE_META[t].text;
		previewValue = {};
	}

	const LIKERT_PRESETS: Record<string, string[]> = {
		'Agreement (5)': ['Strongly disagree', 'Disagree', 'Neutral', 'Agree', 'Strongly agree'],
		'Agreement (7)': ['Strongly disagree', 'Disagree', 'Somewhat disagree', 'Neutral', 'Somewhat agree', 'Agree', 'Strongly agree'],
		'Satisfaction (5)': ['Very dissatisfied', 'Dissatisfied', 'Neutral', 'Satisfied', 'Very satisfied'],
		'Frequency (5)': ['Never', 'Rarely', 'Sometimes', 'Often', 'Always'],
		'Difficulty (5)': ['Very easy', 'Easy', 'OK', 'Hard', 'Very hard']
	};
	const num = (v: string) => (v === '' ? undefined : Number(v));
	const blanks = $derived([...new Set([...text.matchAll(/\[\[(\d{1,2})\]\]/g)].map((m) => m[1]))]);
	const preview = $derived<PollQuestion | null>(type ? { id: 'preview', poll_id: pollId, position: 0, type, text: text || '…', required, body: { ...withIds($state.snapshot(body) as PollBody), format: 'markdown', blanks } } : null);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!type) return;
		saving = true;
		errors = {};
		const b = { ...$state.snapshot(body), format: 'markdown' } as PollBody;
		const payload = {
			type, text, required, body: b,
			key: cleanKey(type, { ...withIds(b), blanks }, $state.snapshot(key) as PollKey),
			points: Number(points) || 0,
			time_limit_sec: pacing === 'presenter' && timeLimit ? Number(timeLimit) : null
		};
		try {
			const q = initial
				? await api.put<PollQuestion>('/api/teacher/poll-questions/' + initial.id, payload)
				: await api.post<PollQuestion>('/api/teacher/polls/' + pollId + '/questions', { ...payload, ...extra });
			toast(initial ? 'Question saved' : 'Question added');
			onsaved(q);
		} catch (err) {
			errors = err instanceof ApiError ? (Object.keys(err.fields).length ? err.fields : { _: err.message }) : { _: 'Save failed' };
		} finally {
			saving = false;
		}
	}
	const errorList = $derived(Object.entries(errors));
</script>

{#if !type}
	<div class="vstack">
		<div class="flex items-center gap-2">
			<h2 class="m-0 text-lg">Choose a question type</h2>
			<span class="spacer"></span>
			<button type="button" class="btn btn-ghost btn-sm" onclick={oncancel}>Cancel</button>
		</div>
		{#each TYPES_BY_GROUP as g (g.group)}
			<section>
				<h3 class="small muted mb-2 mt-3 uppercase tracking-wide">{g.group}</h3>
				<div class="type-grid">
					{#each g.types as t (t)}
						<button type="button" class="type-card" onclick={() => choose(t)}>
							<span class="type-icon" aria-hidden="true"><Icon name={TYPE_META[t].icon} size={18} /></span>
							<span class="min-w-0 text-left">
								<span class="block font-semibold">{TYPE_META[t].label}</span>
								<span class="small muted block">{TYPE_META[t].hint}</span>
							</span>
						</button>
					{/each}
				</div>
			</section>
		{/each}
	</div>
{:else}
	<form class="editor" onsubmit={save}>
		<div class="vstack min-w-0">
			<div class="flex flex-wrap items-center gap-2">
				<span class="badge badge-soft badge-primary gap-1"><Icon name={TYPE_META[type].icon} size={13} />{TYPE_META[type].label}</span>
				{#if !initial}<button type="button" class="btn btn-ghost btn-xs" onclick={() => (type = null)}>Change type</button>{/if}
				<span class="spacer"></span>
				<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={required} /> Required</label>
			</div>
			<div>
				<label for="pq-text">Question</label>
				<RichTextEditor id="pq-text" label="Question" bind:value={text} {format} compact={false} blanks={type === 'BLANK_OPT' || type === 'BLANK_TEXT'} />
				{#if type === 'BLANK_OPT' || type === 'BLANK_TEXT'}<p class="small muted m-0 mt-1">Add blanks with <strong>+ Blank</strong> or by typing [[1]].</p>{/if}
			</div>

			{#if type === 'SINGLE' || type === 'MULTI' || type === 'BLANK_OPT'}
				<ChoiceList bind:items={body.options} label={type === 'BLANK_OPT' ? 'Word choices (shared by all blanks)' : 'Options'} letters={type !== 'BLANK_OPT'} />
				{#if type === 'SINGLE'}
					<div class="join">
						<button type="button" class="btn btn-sm join-item" class:btn-primary={body.display !== 'dropdown'} onclick={() => (body.display = 'radio')}>Radio buttons</button>
						<button type="button" class="btn btn-sm join-item" class:btn-primary={body.display === 'dropdown'} onclick={() => (body.display = 'dropdown')}>Dropdown</button>
					</div>
				{/if}
				{#if type === 'MULTI'}
					<div class="max-w-xs"><label for="pq-max">Most choices allowed (0 = any)</label><input id="pq-max" class="input input-sm w-full" type="number" min="0" max={body.options?.length ?? 0} bind:value={body.max_choices} /></div>
				{/if}
			{:else if type === 'MATCH'}
				<div class="grid gap-4 sm:grid-cols-2">
					<ChoiceList bind:items={body.left} label="Left items" noun="item" />
					<ChoiceList bind:items={body.right} label="Right items" noun="item" />
				</div>
			{:else if type === 'DRAG'}
				<div class="join">
					<button type="button" class="btn btn-sm join-item" class:btn-primary={!body.zones} onclick={() => (body.zones = undefined)}>Rank in order</button>
					<button type="button" class="btn btn-sm join-item" class:btn-primary={!!body.zones} onclick={() => (body.zones = body.zones ?? [{ id: '', text: 'Box 1' }, { id: '', text: 'Box 2' }])}>Sort into boxes</button>
				</div>
				<ChoiceList bind:items={body.options} label="Items" noun="item" />
				{#if body.zones}<ChoiceList bind:items={body.zones} label="Boxes" noun="box" max={10} />{/if}
			{:else if type === 'LIKERT'}
				<ChoiceList bind:items={body.rows} label="Statements" noun="statement" min={1} max={20} />
				<div>
					<p class="small font-semibold m-0 mb-1">Scale</p>
					<div class="flex flex-wrap gap-1">
						{#each Object.entries(LIKERT_PRESETS) as [name, labels] (name)}
							<button type="button" class="btn btn-xs" class:btn-primary={JSON.stringify(body.scale) === JSON.stringify(labels)} onclick={() => (body.scale = [...labels])}>{name}</button>
						{/each}
					</div>
					<div class="mt-2 grid gap-1 sm:grid-cols-2">
						{#each body.scale ?? [] as _, i (i)}<input class="input input-sm w-full" bind:value={body.scale![i]} aria-label="Scale point {i + 1}" maxlength="40" />{/each}
					</div>
				</div>
			{:else if type === 'MATRIX'}
				<div class="join">
					{#each [['single', 'One per row'], ['multi', 'Several per row'], ['text', 'Text boxes']] as [m, l] (m)}
						<button type="button" class="btn btn-sm join-item" class:btn-primary={body.mode === m} onclick={() => (body.mode = m as PollBody['mode'])}>{l}</button>
					{/each}
				</div>
				<div class="grid gap-4 sm:grid-cols-2">
					<ChoiceList bind:items={body.rows} label="Rows" noun="row" min={1} max={20} />
					<ChoiceList bind:items={body.columns} label="Columns" noun="column" max={10} />
				</div>
			{:else if type === 'RATING'}
				<div class="max-w-xs"><label for="pq-pts">Stars</label>
					<select id="pq-pts" class="select select-sm w-full" bind:value={body.points}>{#each [3, 4, 5, 6, 7, 8, 9, 10] as n (n)}<option value={n}>{n}</option>{/each}</select></div>
			{:else if type === 'SLIDER' || type === 'NUMBER'}
				<div class="grid grid-cols-3 gap-2">
					<div><label for="pq-min">Minimum</label><input id="pq-min" class="input input-sm w-full" type="number" value={body.min ?? ''} oninput={(e) => (body.min = num(e.currentTarget.value))} placeholder={type === 'NUMBER' ? 'none' : ''} /></div>
					<div><label for="pq-maxv">Maximum</label><input id="pq-maxv" class="input input-sm w-full" type="number" value={body.max ?? ''} oninput={(e) => (body.max = num(e.currentTarget.value))} placeholder={type === 'NUMBER' ? 'none' : ''} /></div>
					<div><label for="pq-step">Step</label><input id="pq-step" class="input input-sm w-full" type="number" min="0" step="any" value={body.step ?? ''} oninput={(e) => (body.step = num(e.currentTarget.value))} placeholder={type === 'NUMBER' ? 'any' : '1'} /></div>
				</div>
				{#if type === 'SLIDER'}
					<div class="grid grid-cols-2 gap-2">
						<div><label for="pq-minl">Left label</label><input id="pq-minl" class="input input-sm w-full" bind:value={body.min_label} maxlength="60" /></div>
						<div><label for="pq-maxl">Right label</label><input id="pq-maxl" class="input input-sm w-full" bind:value={body.max_label} maxlength="60" /></div>
					</div>
				{:else}
					<div class="max-w-xs"><label for="pq-unit">Unit (optional)</label><input id="pq-unit" class="input input-sm w-full" bind:value={body.unit} maxlength="20" placeholder="e.g. kg, °C, minutes" /></div>
				{/if}
			{:else if type === 'DATE' || type === 'TIME'}
				<div class="grid grid-cols-2 gap-2">
					<div><label for="pq-from">Earliest (optional)</label><input id="pq-from" class="input input-sm w-full" type={type === 'DATE' ? 'date' : 'time'} bind:value={body.from} /></div>
					<div><label for="pq-to">Latest (optional)</label><input id="pq-to" class="input input-sm w-full" type={type === 'DATE' ? 'date' : 'time'} bind:value={body.to} /></div>
				</div>
			{:else if type === 'SHORT_TEXT' || type === 'CODE'}
				<div class="grid grid-cols-2 gap-2">
					{#if type === 'CODE'}<div><label for="pq-lang">Language label</label><input id="pq-lang" class="input input-sm w-full" bind:value={body.language} maxlength="40" /></div>{/if}
					<div><label for="pq-len">Maximum characters</label><input id="pq-len" class="input input-sm w-full" type="number" min="1" max={type === 'CODE' ? 20000 : 500} bind:value={body.max_length} /></div>
				</div>
			{:else if type === 'ESSAY'}
				<div class="max-w-xs"><label for="pq-words">Word limit (0 = none)</label><input id="pq-words" class="input input-sm w-full" type="number" min="0" max="5000" bind:value={body.max_words} /></div>
			{:else if type === 'WORD_CLOUD'}
				<div class="max-w-xs"><label for="pq-ent">Words per participant</label>
					<select id="pq-ent" class="select select-sm w-full" bind:value={body.max_entries}>{#each [1, 2, 3, 4, 5] as n (n)}<option value={n}>{n}</option>{/each}</select></div>
			{:else if type === 'FILE'}
				<fieldset class="m-0 border-0 p-0">
					<legend class="small font-semibold">Accepted files</legend>
					<div class="flex flex-wrap gap-3">
						{#each [['image', 'Images'], ['pdf', 'PDF'], ['document', 'Documents'], ['any', 'Any of these']] as [k, l] (k)}
							<label class="m-0 flex items-center gap-2 font-normal"><input type="checkbox" class="checkbox checkbox-sm" checked={body.accept?.includes(k as 'image')} onchange={(e) => (body.accept = e.currentTarget.checked ? [...(body.accept ?? []), k as 'image'] : (body.accept ?? []).filter((x) => x !== k))} />{l}</label>
						{/each}
					</div>
				</fieldset>
				<div class="max-w-xs"><label for="pq-mb">Largest file (MB, up to 5)</label><input id="pq-mb" class="input input-sm w-full" type="number" min="1" max="5" bind:value={body.max_mb} /></div>
			{:else if type === 'AUDIO' || type === 'VIDEO'}
				<div class="max-w-xs"><label for="pq-sec">Longest recording (seconds, up to {type === 'AUDIO' ? 60 : 30})</label><input id="pq-sec" class="input input-sm w-full" type="number" min="5" max={type === 'AUDIO' ? 60 : 30} bind:value={body.max_seconds} /></div>
				<p class="small muted m-0">Recording needs HTTPS (or localhost) and the participant's permission.</p>
			{/if}

			{#if scoring || pacing === 'presenter'}
				<section class="scoring vstack" aria-label="Scoring">
					{#if scoring && SCORABLE.has(type)}
						<KeyEditor {type} body={withIds($state.snapshot(body) as PollBody)} {blanks} bind:key />
					{:else if scoring}
						<p class="small muted m-0 flex items-center gap-1"><Icon name="info" size={14} />{TYPE_META[type].label} answers are opinions, so they aren't scored.</p>
					{/if}
					<div class="flex flex-wrap gap-3">
						{#if scoring && SCORABLE.has(type)}
							<div class="w-32"><label for="pq-points">Points</label><input id="pq-points" class="input input-sm w-full" type="number" min="0" max="10000" step="10" bind:value={points} /></div>
						{/if}
						{#if pacing === 'presenter'}
							<div class="w-40"><label for="pq-limit">Time limit (seconds)</label><input id="pq-limit" class="input input-sm w-full" type="number" min="5" max="3600" placeholder="none" value={timeLimit ?? ''} oninput={(e) => (timeLimit = e.currentTarget.value === '' ? null : Number(e.currentTarget.value))} /></div>
						{/if}
					</div>
					{#if pacing === 'presenter'}<p class="small muted m-0">With a time limit, answers close when the countdown ends{scoring ? ', and the speed bonus (if on) rewards quick right answers' : ''}.</p>{/if}
				</section>
			{/if}

			{#if errorList.length}
				<div class="alert alert-soft alert-error small" role="alert"><Icon name="alert" /><ul class="m-0 pl-4">{#each errorList as [k, v] (k)}<li>{k === '_' ? '' : k.replace('body.', '') + ': '}{v}</li>{/each}</ul></div>
			{/if}
			<div class="flex gap-2">
				<button class="btn btn-primary" disabled={saving}>{#if saving}<span class="loading loading-spinner loading-sm"></span>{/if}{initial ? 'Save question' : 'Add question'}</button>
				<button type="button" class="btn" onclick={oncancel}>Cancel</button>
			</div>
		</div>
		<aside class="preview" aria-label="Preview">
			<p class="small muted m-0 mb-2 flex items-center gap-1"><Icon name="eye" size={14} />What participants see</p>
			{#if preview}
				{#key type}
					<PollInput question={preview} value={previewValue} onchange={(a) => (previewValue = a)} upload={async () => { toast('Uploads work once the poll is open', 'info'); }} />
				{/key}
			{/if}
		</aside>
	</form>
{/if}

<style>
	.type-grid { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(min(15rem, 100%), 1fr)); }
	.type-card { display: flex; gap: 0.75rem; align-items: flex-start; padding: 0.75rem; border-radius: var(--radius-box); border: 1px solid var(--color-base-300); background: var(--color-base-100); cursor: pointer; transition: border-color var(--motion-fast), transform var(--motion-fast) var(--ease-out), box-shadow var(--motion-fast); }
	.type-card:hover { border-color: color-mix(in oklab, var(--color-primary) 50%, var(--color-base-300)); transform: translateY(-1px); box-shadow: 0 6px 16px -10px color-mix(in oklab, var(--color-base-content) 40%, transparent); }
	.type-icon { width: 2.25rem; height: 2.25rem; flex: none; display: grid; place-items: center; border-radius: 0.6rem; background: color-mix(in oklab, var(--color-primary) 12%, transparent); color: var(--color-primary); }
	.editor { display: grid; gap: 1.25rem; }
	@media (min-width: 1024px) { .editor { grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr); align-items: start; } .preview { position: sticky; top: 5rem; } }
	.scoring { padding: 0.9rem 1rem; border-radius: var(--radius-box); border: 1px solid color-mix(in oklab, var(--color-success) 35%, var(--color-base-300)); background: color-mix(in oklab, var(--color-success) 5%, var(--color-base-100)); }
	.preview { padding: 1rem; border-radius: var(--radius-box); background: var(--color-base-200); border: 1px dashed var(--color-field); }
</style>
