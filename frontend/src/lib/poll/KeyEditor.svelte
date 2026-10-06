<script lang="ts">
	// The correct answer of a scored poll question (D-42), in the shape the
	// question type needs. Participants never see it until it is revealed.
	import type { Choice, PollBody, PollKey, PollType } from './types';

	let { type, body, blanks = [], key = $bindable() }: { type: PollType; body: PollBody; blanks?: string[]; key: PollKey } = $props();

	const letter = (i: number) => String.fromCharCode(65 + i);
	const label = (c: Choice, i: number) => c.text.trim() || `Option ${letter(i)}`;
	const opts = $derived(body.options ?? []);

	function toggle(id: string, on: boolean) {
		const cur = key.correct ?? [];
		key.correct = type === 'SINGLE' ? [id] : on ? [...cur.filter((x) => x !== id), id] : cur.filter((x) => x !== id);
	}
	function setPair(from: string, to: string) {
		const pairs = { ...(key.pairs ?? {}) };
		if (to) pairs[from] = to;
		else delete pairs[from];
		key.pairs = pairs;
	}
	function setBlank(b: string, vs: string[]) {
		const out = { ...(key.blanks ?? {}) };
		if (vs.length) out[b] = vs;
		else delete out[b];
		key.blanks = out;
	}
	const words = (s: string) => s.split(/[,\n]/).map((x) => x.trim()).filter(Boolean);
	const num = (v: string) => (v === '' ? undefined : Number(v));
	const rankedAsListed = $derived(!!key.order?.length && key.order.join() === opts.map((o) => o.id).join());
</script>

<fieldset class="key m-0 vstack">
	<legend class="font-semibold">Correct answer</legend>
	{#if type === 'SINGLE' || type === 'MULTI'}
		<div class="grid gap-1">
			{#each opts as o, i (o.id || i)}
				<label class="m-0 flex items-center gap-2 font-normal">
					<input type={type === 'SINGLE' ? 'radio' : 'checkbox'} class={type === 'SINGLE' ? 'radio radio-sm radio-success' : 'checkbox checkbox-sm checkbox-success'} name="key-correct"
						checked={key.correct?.includes(o.id)} disabled={!o.id} onchange={(e) => toggle(o.id, e.currentTarget.checked)} />
					<span class="letter" aria-hidden="true">{letter(i)}</span>{label(o, i)}
				</label>
			{/each}
		</div>
		{#if type === 'MULTI'}<p class="small muted m-0">Partial credit: each right choice earns its share, each wrong one takes a share away (never below 0).</p>{/if}
	{:else if type === 'MATCH' || (type === 'DRAG' && body.zones?.length)}
		{@const froms = type === 'MATCH' ? (body.left ?? []) : opts}
		{@const tos = type === 'MATCH' ? (body.right ?? []) : (body.zones ?? [])}
		<div class="pairs">
			{#each froms as f, i (f.id || i)}
				<label class="m-0 font-normal" for="key-p-{i}">{f.text || `Item ${i + 1}`}</label>
				<select id="key-p-{i}" class="select select-sm w-full" disabled={!f.id} value={key.pairs?.[f.id] ?? ''} onchange={(e) => setPair(f.id, e.currentTarget.value)}>
					<option value="">Not scored</option>
					{#each tos as t, j (t.id || j)}{#if t.id}<option value={t.id}>{t.text || `Choice ${j + 1}`}</option>{/if}{/each}
				</select>
			{/each}
		</div>
		<p class="small muted m-0">Each right {type === 'MATCH' ? 'match' : 'box'} earns its share of the points.</p>
	{:else if type === 'DRAG'}
		<label class="m-0 flex items-center gap-2 font-normal">
			<input type="checkbox" class="checkbox checkbox-sm checkbox-success" checked={rankedAsListed} onchange={(e) => (key.order = e.currentTarget.checked ? opts.map((o) => o.id) : undefined)} />
			The order listed above is the correct ranking
		</label>
		<p class="small muted m-0">Each item in its right place earns its share. Reorder the items above to change the key.</p>
	{:else if type === 'BLANK_OPT' || type === 'BLANK_TEXT'}
		{#if !blanks.length}<p class="small muted m-0">Add blanks to the question first.</p>{/if}
		<div class="pairs">
			{#each blanks as b (b)}
				<label class="m-0 font-normal" for="key-b-{b}">Blank {b}</label>
				{#if type === 'BLANK_OPT'}
					<select id="key-b-{b}" class="select select-sm w-full" value={key.blanks?.[b]?.[0] ?? ''} onchange={(e) => setBlank(b, e.currentTarget.value ? [e.currentTarget.value] : [])}>
						<option value="">Not scored</option>
						{#each opts as o, i (o.id || i)}{#if o.id}<option value={o.id}>{o.text}</option>{/if}{/each}
					</select>
				{:else}
					<input id="key-b-{b}" class="input input-sm w-full" value={(key.blanks?.[b] ?? []).join(', ')} placeholder="Accepted words, separated by commas" onchange={(e) => setBlank(b, words(e.currentTarget.value))} />
				{/if}
			{/each}
		</div>
		{#if type === 'BLANK_TEXT'}<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="toggle toggle-xs" bind:checked={key.case_sensitive} />Capital letters must match</label>{/if}
	{:else if type === 'SHORT_TEXT'}
		<label class="m-0" for="key-acc">Accepted answers <span class="muted font-normal">(one per line)</span></label>
		<textarea id="key-acc" class="textarea textarea-sm w-full" rows="3" value={(key.accepted ?? []).join('\n')} onchange={(e) => (key.accepted = words(e.currentTarget.value))}></textarea>
		<p class="small muted m-0">Extra spaces and punctuation at the ends are ignored.</p>
		<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="toggle toggle-xs" bind:checked={key.case_sensitive} />Capital letters must match</label>
	{:else if type === 'NUMBER' || type === 'SLIDER'}
		<div class="grid max-w-md grid-cols-2 gap-2">
			<div><label for="key-v">Correct value</label><input id="key-v" class="input input-sm w-full" type="number" step="any" value={key.value ?? ''} oninput={(e) => (key.value = num(e.currentTarget.value))} /></div>
			<div><label for="key-t">Allowed ± <span class="muted font-normal">{body.unit ?? ''}</span></label><input id="key-t" class="input input-sm w-full" type="number" min="0" step="any" value={key.tolerance ?? ''} oninput={(e) => (key.tolerance = num(e.currentTarget.value))} placeholder="0" /></div>
		</div>
	{:else if type === 'DATE' || type === 'TIME'}
		<div class="max-w-xs"><label for="key-d">Correct {type === 'DATE' ? 'date' : 'time'}</label>
			<input id="key-d" class="input input-sm w-full" type={type === 'DATE' ? 'date' : 'time'} value={key.accepted?.[0] ?? ''} onchange={(e) => (key.accepted = e.currentTarget.value ? [e.currentTarget.value] : [])} /></div>
	{/if}
</fieldset>

<style>
	.key { border: 0; padding: 0; }
	.key legend { padding: 0; margin-bottom: 0.35rem; }
	.letter { width: 1.4rem; height: 1.4rem; border-radius: 0.4rem; display: inline-grid; place-items: center; font-size: 0.75rem; font-weight: 700; background: var(--color-base-200); }
	.pairs { display: grid; gap: 0.4rem 0.75rem; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr); align-items: center; }
</style>
