<script lang="ts">
	// One-by-one question authoring for all seven types (FR-QZ-02/03/04/07/09).
	import { api, ApiError } from './api';
	import { QTYPES, QTYPE_LABEL, type QType, type Question } from './types';

	let { quizId, question = null, onsaved, oncancel }: { quizId: string; question?: Question | null; onsaved: (q: Question) => void; oncancel: () => void } = $props();

	type Row = { text: string; correct: boolean; zone: string };
	// The form is seeded once; the parent re-keys this component per question.
	// svelte-ignore state_referenced_locally
	const q = question;
	let code = $state(q?.code ?? '');
	let type = $state<QType>(q?.type ?? 'SINGLE');
	let text = $state(q?.text ?? '');
	let marks = $state(q?.marks ?? 1);
	let negative = $state(q?.negative_marks ?? 0);
	let partial = $state<boolean | null>(q ? q.partial_credit : null);
	let timeLimit = $state<number | ''>((q?.settings?.question_time_limit_sec as number) ?? '');
	let evaluation = $state<string>((q?.settings?.evaluation_method as string) ?? '');
	let fbCorrect = $state(q?.feedback?.correct ?? '');
	let fbIncorrect = $state(q?.feedback?.incorrect ?? '');
	let modelAnswer = $state(q?.key?.model_answer ?? '');
	let rubric = $state(q?.key?.rubric ?? '');
	let wordLimit = $state(q?.body?.word_limit ?? 0);
	let caseSensitive = $state(q?.key?.case_sensitive ?? false);
	let tolerance = $state(q?.key?.tolerance ?? 0);
	let dragMode = $state<'order' | 'zones'>(q?.body?.zones?.length ? 'zones' : 'order');

	// Options (SINGLE/MULTI/BLANK_OPT), items (DRAG) — in authored order.
	let rows = $state<Row[]>(
		q?.body?.options?.map((o) => ({
			text: o.text,
			correct: q.key?.correct?.includes(o.id) ?? false,
			zone: q.key?.pairs?.[o.id] ? String(q.body.zones!.findIndex((z) => z.id === q.key.pairs![o.id])) : ''
		})) ?? [{ text: '', correct: true, zone: '' }, { text: '', correct: false, zone: '' }]
	);
	if (q?.type === 'DRAG' && !q.body.zones?.length && q.key.order) {
		const byId = Object.fromEntries(q.body.options!.map((o) => [o.id, o.text]));
		rows = q.key.order.map((id) => ({ text: byId[id], correct: false, zone: '' }));
	}
	let pairs = $state<{ left: string; right: string }[]>(
		q?.type === 'MATCH' ? q.body.left!.map((l) => ({ left: l.text, right: q.body.right!.find((r) => r.id === q.key.pairs?.[l.id])?.text ?? '' })) : [{ left: '', right: '' }, { left: '', right: '' }]
	);
	let zones = $state<string[]>(q?.body?.zones?.map((z) => z.text) ?? ['', '']);
	let blankKeys = $state<Record<string, string>>(
		Object.fromEntries(
			Object.entries(q?.key?.blanks ?? {}).map(([b, v]) => [b, q?.type === 'BLANK_OPT' ? String(q.body.options!.findIndex((o) => o.id === v[0])) : v.join(' | ')])
		)
	);

	const blanks = $derived([...new Set([...text.matchAll(/\[\[(\d{1,2})\]\]/g)].map((m) => m[1]))]);
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);

	function payload() {
		const opt = rows.map((r, i) => ({ id: 'o' + (i + 1), text: r.text }));
		const body: Record<string, unknown> = {};
		const key: Record<string, unknown> = {};
		switch (type) {
			case 'SINGLE':
			case 'MULTI':
				body.options = opt;
				key.correct = rows.flatMap((r, i) => (r.correct ? ['o' + (i + 1)] : []));
				break;
			case 'MATCH':
				body.left = pairs.map((p, i) => ({ id: 'l' + (i + 1), text: p.left }));
				body.right = pairs.map((p, i) => ({ id: 'r' + (i + 1), text: p.right }));
				key.pairs = Object.fromEntries(pairs.map((_, i) => ['l' + (i + 1), 'r' + (i + 1)]));
				break;
			case 'BLANK_OPT':
				body.options = opt;
				key.blanks = Object.fromEntries(blanks.map((b) => [b, blankKeys[b] !== undefined && blankKeys[b] !== '' ? ['o' + (Number(blankKeys[b]) + 1)] : []]));
				break;
			case 'BLANK_TEXT':
				key.blanks = Object.fromEntries(blanks.map((b) => [b, (blankKeys[b] ?? '').split('|').map((s) => s.trim()).filter(Boolean)]));
				key.case_sensitive = caseSensitive;
				key.tolerance = tolerance;
				if (modelAnswer) key.model_answer = modelAnswer;
				if (rubric) key.rubric = rubric;
				break;
			case 'DRAG':
				body.options = opt;
				if (dragMode === 'zones') {
					body.zones = zones.map((z, i) => ({ id: 'z' + (i + 1), text: z }));
					key.pairs = Object.fromEntries(rows.flatMap((r, i) => (r.zone !== '' ? [['o' + (i + 1), 'z' + (Number(r.zone) + 1)]] : [])));
				} else key.order = opt.map((o) => o.id);
				break;
			case 'ESSAY':
				body.word_limit = wordLimit || 0;
				key.model_answer = modelAnswer;
				key.rubric = rubric;
				break;
		}
		const settings: Record<string, unknown> = {};
		if (timeLimit !== '' && Number(timeLimit) > 0) settings.question_time_limit_sec = Number(timeLimit);
		if (evaluation) settings.evaluation_method = evaluation;
		return {
			code, type, text, body, key, marks: Number(marks), negative_marks: Number(negative) || 0,
			...(partial === null ? {} : { partial_credit: partial }),
			feedback: { correct: fbCorrect, incorrect: fbIncorrect, options: q?.feedback?.options, bands: q?.feedback?.bands },
			settings
		};
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		errors = {};
		try {
			const saved = q
				? await api.put<Question>('/api/teacher/questions/' + q.id, payload())
				: await api.post<Question>('/api/teacher/quizzes/' + quizId + '/questions', payload());
			onsaved(saved);
		} catch (err) {
			errors = err instanceof ApiError ? (Object.keys(err.fields).length ? err.fields : { _: err.message }) : { _: 'Save failed' };
		} finally {
			saving = false;
		}
	}
	const usesOptions = $derived(type === 'SINGLE' || type === 'MULTI' || type === 'BLANK_OPT' || type === 'DRAG');
	const errorList = $derived(Object.entries(errors));
</script>

<form class="stack" onsubmit={save}>
	<div class="row">
		<div style="width:9rem"><label for="code">Code</label><input id="code" bind:value={code} required placeholder="Q001" /></div>
		<div style="flex:1;min-width:12rem"><label for="type">Type</label>
			<select id="type" bind:value={type} disabled={!!q}>{#each QTYPES as t (t)}<option value={t}>{QTYPE_LABEL[t]}</option>{/each}</select></div>
		<div style="width:7rem"><label for="marks">Marks</label><input id="marks" type="number" min="0.25" step="0.25" bind:value={marks} /></div>
	</div>
	<div>
		<label for="text">Question text</label>
		<textarea id="text" bind:value={text} rows="3" required></textarea>
		{#if type === 'BLANK_OPT' || type === 'BLANK_TEXT'}<p class="small muted">Mark blanks as [[1]], [[2]] … in the text.</p>{/if}
	</div>

	{#if usesOptions}
		<fieldset class="stack">
			<legend><strong>{type === 'DRAG' ? (dragMode === 'order' ? 'Items, in the correct order' : 'Items') : 'Options'}</strong></legend>
			{#if type === 'DRAG'}
				<div class="row small">
					<label class="row" style="font-weight:400"><input type="radio" bind:group={dragMode} value="order" /> Put in order</label>
					<label class="row" style="font-weight:400"><input type="radio" bind:group={dragMode} value="zones" /> Sort into boxes</label>
				</div>
			{/if}
			{#each rows as r, i (i)}
				<div class="row">
					{#if type === 'SINGLE'}<input type="radio" name="correct" aria-label="Correct" checked={r.correct} onchange={() => rows.forEach((x, j) => (x.correct = j === i))} />{/if}
					{#if type === 'MULTI'}<input type="checkbox" aria-label="Correct" bind:checked={r.correct} />{/if}
					<input style="flex:1" aria-label={'Option ' + (i + 1)} bind:value={r.text} required />
					{#if type === 'DRAG' && dragMode === 'zones'}
						<select style="width:10rem" aria-label="Box" bind:value={r.zone}>
							<option value="">Box…</option>
							{#each zones as z, zi (zi)}<option value={String(zi)}>{z || 'Box ' + (zi + 1)}</option>{/each}
						</select>
					{/if}
					<button type="button" class="small" onclick={() => rows.splice(i, 1)} disabled={rows.length <= 2}>✕</button>
				</div>
			{/each}
			<button type="button" class="small" onclick={() => rows.push({ text: '', correct: false, zone: '' })}>Add {type === 'DRAG' ? 'item' : 'option'}</button>
			{#if type === 'DRAG' && dragMode === 'zones'}
				<strong>Boxes</strong>
				{#each zones as _z, i (i)}
					<div class="row"><input style="flex:1" aria-label={'Box ' + (i + 1)} bind:value={zones[i]} required /><button type="button" class="small" onclick={() => zones.splice(i, 1)} disabled={zones.length <= 1}>✕</button></div>
				{/each}
				<button type="button" class="small" onclick={() => zones.push('')}>Add box</button>
			{/if}
		</fieldset>
	{/if}

	{#if type === 'MATCH'}
		<fieldset class="stack">
			<legend><strong>Correct pairs</strong> <span class="small muted">(the right side is shuffled for students)</span></legend>
			{#each pairs as p, i (i)}
				<div class="row"><input style="flex:1" aria-label="Left" bind:value={p.left} required /> → <input style="flex:1" aria-label="Right" bind:value={p.right} required />
					<button type="button" class="small" onclick={() => pairs.splice(i, 1)} disabled={pairs.length <= 2}>✕</button></div>
			{/each}
			<button type="button" class="small" onclick={() => pairs.push({ left: '', right: '' })}>Add pair</button>
		</fieldset>
	{/if}

	{#if (type === 'BLANK_OPT' || type === 'BLANK_TEXT') && blanks.length}
		<fieldset class="stack">
			<legend><strong>Answers per blank</strong></legend>
			{#each blanks as b (b)}
				<div class="row">
					<span style="width:4rem">[[{b}]]</span>
					{#if type === 'BLANK_OPT'}
						<select style="flex:1" bind:value={blankKeys[b]} aria-label={'Answer for blank ' + b}>
							<option value="">Choose…</option>
							{#each rows as r, i (i)}<option value={String(i)}>{r.text || 'Option ' + (i + 1)}</option>{/each}
						</select>
					{:else}
						<input style="flex:1" bind:value={blankKeys[b]} placeholder="accepted answers, separated by |" aria-label={'Answers for blank ' + b} />
					{/if}
				</div>
			{/each}
			{#if type === 'BLANK_TEXT'}
				<div class="row">
					<label class="row" style="font-weight:400"><input type="checkbox" bind:checked={caseSensitive} /> Case-sensitive</label>
					<label for="tol" style="font-weight:400">Spelling tolerance</label>
					<select id="tol" style="width:6rem" bind:value={tolerance}><option value={0}>0</option><option value={1}>1</option><option value={2}>2</option><option value={3}>3</option></select>
				</div>
			{/if}
		</fieldset>
	{/if}

	{#if type === 'ESSAY' || (type === 'BLANK_TEXT' && evaluation === 'llm')}
		<div><label for="model">Model answer</label><textarea id="model" bind:value={modelAnswer} rows="3"></textarea></div>
		<div><label for="rubric">Rubric (for LLM marking)</label><textarea id="rubric" bind:value={rubric} rows="3"></textarea></div>
		{#if type === 'ESSAY'}<div style="width:10rem"><label for="wl">Word limit (0 = none)</label><input id="wl" type="number" min="0" bind:value={wordLimit} /></div>{/if}
	{/if}

	<details>
		<summary>Timing, marking and feedback</summary>
		<div class="grid" style="margin-top:0.75rem">
			<div><label for="tl">Time limit (seconds)</label><input id="tl" type="number" min="0" bind:value={timeLimit} placeholder="Inherit from quiz" /></div>
			<div><label for="ev">Marking</label>
				<select id="ev" bind:value={evaluation}>
					<option value="">Default for the type</option>
					{#if type !== 'ESSAY'}<option value="key">Answer key</option>{/if}
					{#if type === 'ESSAY' || type === 'BLANK_TEXT'}<option value="llm">LLM</option>{/if}
					<option value="manual">Manual</option>
				</select></div>
			<div><label for="neg">Negative marks</label><input id="neg" type="number" min="0" step="0.25" bind:value={negative} /></div>
			<div><label for="pc">Partial credit</label>
				<select id="pc" value={partial === null ? '' : String(partial)} onchange={(e) => (partial = e.currentTarget.value === '' ? null : e.currentTarget.value === 'true')}>
					<option value="">Default for the type</option><option value="true">Yes</option><option value="false">No</option>
				</select></div>
		</div>
		<div class="grid" style="margin-top:0.75rem">
			<div><label for="fc">Feedback when correct</label><input id="fc" bind:value={fbCorrect} /></div>
			<div><label for="fi">Feedback when incorrect</label><input id="fi" bind:value={fbIncorrect} /></div>
		</div>
	</details>

	{#if errorList.length}
		<div class="alert danger"><ul style="margin:0;padding-left:1.2rem">{#each errorList as [k, v] (k)}<li>{k === '_' ? '' : k + ': '}{v}</li>{/each}</ul></div>
	{/if}
	<div class="row">
		<button class="primary" disabled={saving}>{q ? 'Save question' : 'Add question'}</button>
		<button type="button" onclick={oncancel}>Cancel</button>
	</div>
</form>
