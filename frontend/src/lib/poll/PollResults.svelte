<script lang="ts">
	// Live results for one poll question, by type (D-40). Charts follow the
	// dataviz rules: one hue for single-series bars, a validated diverging
	// palette for Likert, a one-hue ramp for grids, thin rounded bars, labels
	// in text colours, hover/focus tooltips, and a table view for every chart.
	import { flip } from 'svelte/animate';
	import Icon from '../ui/Icon.svelte';
	import { fadeIn, flipMs } from '../ui/motion';
	import { fmtSize } from './meta';
	import type { Choice, PollQuestion, PollResult, TextItem, WordCount } from './types';
	import WordCloud from './WordCloud.svelte';

	let {
		question,
		result,
		teacher = false,
		big = false,
		pollId = '',
		onmoderate
	}: {
		question: PollQuestion;
		result?: PollResult;
		teacher?: boolean;
		big?: boolean;
		pollId?: string;
		onmoderate?: (m: { question_id: string; participant_id?: string; word?: string; hidden: boolean }) => void;
	} = $props();

	const b = $derived(question.body);
	const r = $derived(result ?? ({ question_id: question.id, type: question.type, responses: 0 } as PollResult));
	let table = $state(false);
	const pct = (n: number, of: number) => (of ? Math.round((n / of) * 100) : 0);
	const nameOf = (cs: Choice[] | undefined, id: string) => cs?.find((c) => c.id === id)?.text ?? id;

	// Bars for option counts (SINGLE, MULTI): share of respondents.
	const optionBars = $derived(
		(b.options ?? []).map((o) => ({ id: o.id, label: o.text, n: r.counts?.[o.id] ?? 0 }))
	);
	const maxBar = $derived(Math.max(1, ...optionBars.map((x) => x.n)));

	// Grids: rows × columns of counts.
	const grid = $derived.by(() => {
		switch (question.type) {
			case 'MATCH':
				return { rows: b.left ?? [], cols: b.right ?? [] };
			case 'BLANK_OPT':
				return { rows: (b.blanks ?? []).map((x) => ({ id: x, text: `Blank ${x}` })), cols: b.options ?? [] };
			case 'DRAG':
				return b.zones?.length ? { rows: b.options ?? [], cols: b.zones } : null;
			case 'MATRIX':
				return b.mode === 'text' ? null : { rows: b.rows ?? [], cols: b.columns ?? [] };
		}
		return null;
	});
	const rowTotal = (row: string) => Object.values(r.grid?.[row] ?? {}).reduce((s, n) => s + n, 0);

	// Likert: diverging stacked bar, centred on the neutral point.
	const scale = $derived(b.scale ?? []);
	const likertColors = $derived(scale.length === 7 ? 'l7' : 'l5');
	function likertRow(row: string) {
		const counts = scale.map((_, i) => r.grid?.[row]?.[String(i + 1)] ?? 0);
		const total = counts.reduce((s, n) => s + n, 0);
		const shares = counts.map((n) => (total ? (n / total) * 100 : 0));
		const mid = (scale.length - 1) / 2;
		const left = shares.slice(0, Math.floor(mid)).reduce((s, x) => s + x, 0) + (scale.length % 2 ? shares[mid] / 2 : 0);
		return { counts, shares, total, offset: 50 - left };
	}

	const stats = $derived(r.stats);
	const maxBucket = $derived(Math.max(1, ...(stats?.buckets ?? []).map((x) => x.count)));
	const maxValue = $derived(Math.max(1, ...(r.values ?? []).map((x) => x.count)));
	const ranks = $derived(r.ranks ?? []);
	const maxFirst = $derived(Math.max(1, ...ranks.map((x) => x.first)));
	const texts = $derived(r.texts ?? []);
	const cellName = (cell?: string) => {
		if (!cell) return '';
		const [row, col] = cell.split('|');
		return `${nameOf(b.rows, row)} · ${nameOf(b.columns, col)}`;
	};
	const fileURL = (id: string) => `/api/teacher/polls/${pollId}/files/${id}`;
	const fmtDate = (d: string) => {
		const [y, m, day] = d.split('-').map(Number);
		return new Date(y, m - 1, day).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' });
	};

	function hideText(t: TextItem) {
		if (t.participant_id) onmoderate?.({ question_id: question.id, participant_id: t.participant_id, hidden: !t.hidden });
	}
	function hideWord(w: WordCount) {
		onmoderate?.({ question_id: question.id, word: w.word, hidden: !w.hidden });
	}
	const hasTable = $derived(!['ESSAY', 'SHORT_TEXT', 'CODE', 'FILE', 'AUDIO', 'VIDEO'].includes(question.type) && !(question.type === 'MATRIX' && b.mode === 'text'));
</script>

<div class="results" class:big>
	<div class="res-head">
		<span class="small muted"><strong class="tabular text-base-content">{r.responses}</strong> {r.responses === 1 ? 'response' : 'responses'}</span>
		<span class="spacer"></span>
		{#if hasTable && r.responses > 0}
			<button type="button" class="btn btn-ghost btn-xs" aria-pressed={table} onclick={() => (table = !table)}>{table ? 'Show chart' : 'Show table'}</button>
		{/if}
	</div>

	{#if r.responses === 0 && !texts.length}
		<p class="waiting muted small"><span class="loading loading-dots loading-sm text-primary"></span> Waiting for answers…</p>
	{:else if question.type === 'SINGLE' || question.type === 'MULTI'}
		{#if table}
			<table class="table table-sm"><thead><tr><th>Option</th><th class="text-right">People</th><th class="text-right">Share</th></tr></thead>
				<tbody>{#each optionBars as o (o.id)}<tr><td>{o.label}</td><td class="text-right tabular">{o.n}</td><td class="text-right tabular">{pct(o.n, r.responses)}%</td></tr>{/each}</tbody></table>
		{:else}
			<ul class="bars">
				{#each optionBars as o, i (o.id)}
					<!-- Chart marks take focus so keyboard users get the same tooltip as mouse users. -->
					<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
					<li class="bar-row" tabindex="0" aria-label="{o.label}: {o.n} ({pct(o.n, r.responses)}%)">
						<span class="bar-label"><span class="letter-chip" aria-hidden="true">{String.fromCharCode(65 + i)}</span>{o.label}</span>
						<span class="bar-track"><span class="bar" style:width="{(o.n / maxBar) * 100}%"></span></span>
						<span class="bar-value tabular">{pct(o.n, r.responses)}% <span class="muted">({o.n})</span></span>
						<span class="tip" role="tooltip">{o.label}: {o.n} {o.n === 1 ? 'person' : 'people'}, {pct(o.n, r.responses)}%</span>
					</li>
				{/each}
			</ul>
			{#if question.type === 'MULTI'}<p class="small muted m-0">Percentages are of respondents; people could choose more than one.</p>{/if}
		{/if}
	{:else if question.type === 'WORD_CLOUD'}
		{#if table}
			<table class="table table-sm"><thead><tr><th>Word</th><th class="text-right">Mentions</th></tr></thead>
				<tbody>{#each r.words ?? [] as w (w.word)}<tr class:opacity-50={w.hidden}><td>{w.word}{w.hidden ? ' (hidden)' : ''}</td><td class="text-right tabular">{w.count}</td></tr>{/each}</tbody></table>
		{:else}
			<WordCloud words={r.words ?? []} {teacher} {big} onhide={hideWord} />
			{#if teacher}<p class="small muted m-0">Click a word to hide it from participants (and click again to bring it back).</p>{/if}
		{/if}
	{:else if question.type === 'RATING' || question.type === 'SLIDER' || question.type === 'NUMBER'}
		<div class="tiles">
			<div class="tile"><span class="tile-label">Average</span><span class="tile-value">{stats?.mean ?? '–'}{question.type === 'RATING' ? ` / ${b.points ?? 5}` : b.unit ? ` ${b.unit}` : ''}</span></div>
			<div class="tile"><span class="tile-label">Median</span><span class="tile-value">{stats?.median ?? '–'}</span></div>
			<div class="tile"><span class="tile-label">Range</span><span class="tile-value">{stats ? `${stats.min}–${stats.max}` : '–'}</span></div>
		</div>
		{#if table}
			<table class="table table-sm"><thead><tr><th>{question.type === 'RATING' ? 'Stars' : 'Value'}</th><th class="text-right">People</th></tr></thead>
				<tbody>{#each stats?.buckets ?? [] as bk (bk.label)}<tr><td>{bk.label}</td><td class="text-right tabular">{bk.count}</td></tr>{/each}</tbody></table>
		{:else}
			<div class="cols" role="list" style:--n={stats?.buckets.length ?? 1}>
				{#each stats?.buckets ?? [] as bk (bk.label)}
					<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
					<div class="col" role="listitem" tabindex="0" aria-label="{bk.label}: {bk.count}">
						<span class="col-value tabular">{bk.count || ''}</span>
						<span class="col-track"><span class="col-bar" style:height="{(bk.count / maxBucket) * 100}%"></span></span>
						<span class="col-label">{question.type === 'RATING' ? '★'.repeat(Number(bk.label)) : bk.label}</span>
						<span class="tip" role="tooltip">{bk.label}{question.type === 'RATING' ? ' stars' : ''}: {bk.count}</span>
					</div>
				{/each}
			</div>
		{/if}
	{:else if question.type === 'LIKERT'}
		{#if table}
			<div class="table-wrap"><table class="table table-sm"><thead><tr><th>Statement</th>{#each scale as l (l)}<th class="text-right">{l}</th>{/each}<th class="text-right">Mean</th></tr></thead>
				<tbody>{#each b.rows ?? [] as row (row.id)}{@const lr = likertRow(row.id)}<tr><td>{row.text}</td>{#each lr.counts as n, i (i)}<td class="text-right tabular">{n}</td>{/each}<td class="text-right tabular">{r.row_means?.[row.id] ?? '–'}</td></tr>{/each}</tbody></table></div>
		{:else}
			<div class="likert {likertColors}">
				<ul class="legend" aria-hidden="true">
					{#each scale as l, i (l)}<li><span class="sw s{i + 1}"></span>{l}</li>{/each}
				</ul>
				{#each b.rows ?? [] as row (row.id)}
					{@const lr = likertRow(row.id)}
					<div class="lk-row">
						<span class="lk-label">{row.text} <span class="small muted ml-1">· mean {r.row_means?.[row.id] ?? '–'}</span></span>
						<div class="lk-track" aria-hidden="true">
							<span class="lk-mid"></span>
							<div class="lk-bar" style:margin-left="{lr.offset}%">
								{#each lr.shares as s, i (i)}
									{#if s > 0}
										<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
										<span class="seg s{i + 1}" style:width="{s}%" tabindex="0" aria-label="{scale[i]}: {lr.counts[i]}">
											{#if s >= 9}<span class="seg-label">{Math.round(s)}%</span>{/if}
											<span class="tip" role="tooltip">{scale[i]}: {lr.counts[i]} ({Math.round(s)}%)</span>
										</span>
									{/if}
								{/each}
							</div>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	{:else if grid}
		{#if table}
			<div class="table-wrap"><table class="table table-sm"><thead><tr><th></th>{#each grid.cols as c (c.id)}<th class="text-right">{c.text}</th>{/each}</tr></thead>
				<tbody>{#each grid.rows as row (row.id)}<tr><td>{row.text}</td>{#each grid.cols as c (c.id)}<td class="text-right tabular">{r.grid?.[row.id]?.[c.id] ?? 0}</td>{/each}</tr>{/each}</tbody></table></div>
		{:else}
			<div class="table-wrap">
				<div class="heat" style:--cols={grid.cols.length} role="table" aria-label="Answers by row and column">
					<span role="columnheader"></span>
					{#each grid.cols as c (c.id)}<span class="hh" role="columnheader">{c.text}</span>{/each}
					{#each grid.rows as row (row.id)}
						{@const tot = rowTotal(row.id)}
						<span class="hr" role="rowheader">{row.text}</span>
						{#each grid.cols as c (c.id)}
							{@const n = r.grid?.[row.id]?.[c.id] ?? 0}
							{@const t = tot ? n / tot : 0}
							<span class="cell" class:ink={t > 0.55} style:--t={t} role="cell" tabindex="0" aria-label="{row.text}, {c.text}: {n}">
								<span class="tabular">{n ? `${Math.round(t * 100)}%` : '–'}</span>
								<span class="tip" role="tooltip">{row.text} → {c.text}: {n} of {tot}</span>
							</span>
						{/each}
					{/each}
				</div>
			</div>
			<p class="small muted m-0">Shading shows each row's share; darker means more people.</p>
		{/if}
	{:else if question.type === 'DRAG'}
		{#if table}
			<table class="table table-sm"><thead><tr><th>Item</th><th class="text-right">Average rank</th><th class="text-right">Ranked first</th></tr></thead>
				<tbody>{#each ranks as x (x.id)}<tr><td>{nameOf(b.options, x.id)}</td><td class="text-right tabular">{x.avg_rank}</td><td class="text-right tabular">{x.first}</td></tr>{/each}</tbody></table>
		{:else}
			<ol class="ranks">
				{#each ranks as x, i (x.id)}
					<li animate:flip={{ duration: flipMs() }}>
						<span class="rank-n">{i + 1}</span>
						<span class="rank-label">{nameOf(b.options, x.id)}</span>
						<span class="small muted tabular">avg {x.avg_rank}</span>
						<span class="bar-track mini" title="Ranked first by {x.first}"><span class="bar" style:width="{(x.first / maxFirst) * 100}%"></span></span>
						<span class="small muted tabular w-16 text-right">{x.first} first</span>
					</li>
				{/each}
			</ol>
		{/if}
	{:else if question.type === 'BLANK_TEXT'}
		{#each b.blanks ?? [] as bl (bl)}
			<div class="vstack">
				<p class="m-0 font-semibold">Blank {bl}</p>
				{#if table}
					<table class="table table-sm"><tbody>{#each r.blank_words?.[bl] ?? [] as w (w.word)}<tr><td>{w.word}</td><td class="text-right tabular">{w.count}</td></tr>{/each}</tbody></table>
				{:else}
					<WordCloud words={r.blank_words?.[bl] ?? []} {teacher} onhide={hideWord} />
				{/if}
			</div>
		{/each}
	{:else if question.type === 'DATE' || question.type === 'TIME'}
		{#if table}
			<table class="table table-sm"><tbody>{#each r.values ?? [] as v (v.value)}<tr><td>{question.type === 'DATE' ? fmtDate(v.value) : v.value}</td><td class="text-right tabular">{v.count}</td></tr>{/each}</tbody></table>
		{:else}
			<ul class="bars">
				{#each r.values ?? [] as v (v.value)}
					<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
					<li class="bar-row" tabindex="0" aria-label="{v.value}: {v.count}">
						<span class="bar-label">{question.type === 'DATE' ? fmtDate(v.value) : v.value}</span>
						<span class="bar-track"><span class="bar" style:width="{(v.count / maxValue) * 100}%"></span></span>
						<span class="bar-value tabular">{v.count}</span>
						<span class="tip" role="tooltip">{v.value}: {v.count}</span>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}

	{#if texts.length && (question.type === 'ESSAY' || question.type === 'CODE' || question.type === 'SHORT_TEXT' || (question.type === 'MATRIX' && b.mode === 'text'))}
		<ul class="texts" class:code={question.type === 'CODE'}>
			{#each texts as t (t.at + (t.participant_id ?? '') + (t.cell ?? ''))}
				<li class="text-card" class:is-hidden={t.hidden} in:fadeIn animate:flip={{ duration: flipMs() }}>
					{#if t.cell}<p class="small muted m-0 mb-1">{cellName(t.cell)}</p>{/if}
					{#if question.type === 'CODE'}<pre class="m-0"><code>{t.text}</code></pre>{:else}<p class="m-0">{t.text}</p>{/if}
					{#if teacher}
						<div class="mt-2 flex items-center gap-2">
							<span class="small muted">{t.name || 'Anonymous'}</span>
							<span class="spacer"></span>
							{#if t.hidden}<span class="badge badge-soft badge-sm">hidden</span>{/if}
							{#if t.participant_id && !t.cell}<button type="button" class="btn btn-ghost btn-xs" onclick={() => hideText(t)}><Icon name={t.hidden ? 'eye' : 'eye-off'} size={14} />{t.hidden ? 'Show' : 'Hide'}</button>{/if}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	{#if question.type === 'FILE' || question.type === 'AUDIO' || question.type === 'VIDEO'}
		{#if teacher}
			<ul class="files">
				{#each r.files ?? [] as f (f.file.id)}
					<li class="file-card" class:is-hidden={f.hidden} in:fadeIn>
						{#if question.type === 'AUDIO'}
							<audio controls preload="none" src={fileURL(f.file.id)} class="w-full"></audio>
						{:else if question.type === 'VIDEO'}
							<!-- svelte-ignore a11y_media_has_caption -->
							<video controls preload="none" src={fileURL(f.file.id)} class="w-full rounded-lg bg-black"></video>
						{:else if f.file.content_type.startsWith('image/')}
							<a href={fileURL(f.file.id)} target="_blank" rel="noopener"><img src={fileURL(f.file.id)} alt="Upload from {f.name || 'a participant'}" class="thumb" loading="lazy" /></a>
						{/if}
						<div class="mt-2 flex items-center gap-2">
							<div class="min-w-0 flex-1">
								<p class="m-0 truncate font-semibold small">{f.file.name}</p>
								<p class="small muted m-0">{f.name || 'Anonymous'} · {fmtSize(f.file.size)}</p>
							</div>
							<a class="btn btn-ghost btn-xs" href={fileURL(f.file.id)} download={f.file.name}>Download</a>
							<button type="button" class="btn btn-ghost btn-xs" onclick={() => onmoderate?.({ question_id: question.id, participant_id: f.participant_id, hidden: !f.hidden })}>{f.hidden ? 'Unhide' : 'Hide'}</button>
						</div>
					</li>
				{/each}
			</ul>
		{:else}
			<p class="small muted m-0">Only the presenter can open uploaded files.</p>
		{/if}
	{/if}
</div>

<style>
	.results { display: grid; gap: 0.75rem; }
	.res-head { display: flex; align-items: center; gap: 0.5rem; }
	.waiting { display: flex; align-items: center; gap: 0.5rem; padding: 1rem 0; }

	/* Horizontal bars: ≤ 24 px, rounded data end, one hue, value at the tip. */
	.bars { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.6rem; }
	.bar-row { position: relative; display: grid; grid-template-columns: minmax(6rem, 14rem) minmax(0, 1fr) auto; gap: 0.75rem; align-items: center; outline: none; }
	.big .bar-row { grid-template-columns: minmax(8rem, 22rem) minmax(0, 1fr) auto; font-size: 1.25rem; }
	@media (max-width: 520px) { .bar-row { grid-template-columns: minmax(0, 1fr) auto; } .bar-track { grid-column: 1 / -1; grid-row: 2; } }
	.bar-label { display: flex; align-items: center; gap: 0.5rem; min-width: 0; overflow-wrap: anywhere; }
	.letter-chip { flex: none; width: 1.5rem; height: 1.5rem; border-radius: 999px; display: grid; place-items: center; font-size: 0.75rem; font-weight: 700; border: 1.5px solid var(--color-field); color: var(--color-muted); }
	.bar-track { height: 1.25rem; display: block; border-radius: 0 4px 4px 0; background: color-mix(in oklab, var(--color-base-content) 6%, transparent); }
	.big .bar-track { height: 1.5rem; }
	.bar-track.mini { height: 0.6rem; width: 6rem; }
	.bar { display: block; height: 100%; border-radius: 0 4px 4px 0; background: var(--color-primary); transition: width 500ms var(--ease-out); min-width: 2px; }
	.bar-value { font-weight: 700; white-space: nowrap; }

	/* Tooltips on hover and keyboard focus. */
	.tip { position: absolute; z-index: 5; left: 50%; bottom: calc(100% + 6px); transform: translateX(-50%); white-space: nowrap; padding: 0.3rem 0.55rem; border-radius: 0.4rem; font-size: 0.8rem; background: var(--color-neutral); color: var(--color-neutral-content); pointer-events: none; opacity: 0; transition: opacity var(--motion-fast); }
	:is(.bar-row, .col, .seg, .cell):is(:hover, :focus-visible) > .tip { opacity: 1; }
	:is(.bar-row, .col, .seg, .cell):focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; border-radius: 4px; }

	/* Stat tiles. */
	.tiles { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fit, minmax(7rem, 1fr)); }
	.tile { display: grid; padding: 0.6rem 0.8rem; border-radius: var(--radius-field); background: var(--color-base-200); }
	.tile-label { font-size: 0.8rem; color: var(--color-muted); font-weight: 600; }
	.tile-value { font-size: 1.4rem; font-weight: 700; }
	.big .tile-value { font-size: 2.2rem; }

	/* Columns (histograms): one baseline, rounded tops. */
	.cols { display: grid; grid-template-columns: repeat(var(--n), minmax(0, 1fr)); gap: 2px; align-items: end; height: 12rem; }
	.big .cols { height: 18rem; }
	.col { position: relative; display: grid; grid-template-rows: auto 1fr auto; height: 100%; text-align: center; outline: none; }
	.col-value { font-size: 0.8rem; font-weight: 700; min-height: 1.1rem; }
	.col-track { display: flex; align-items: flex-end; justify-content: center; }
	.col-bar { display: block; width: min(100%, 24px); min-height: 1px; border-radius: 4px 4px 0 0; background: var(--color-primary); transition: height 500ms var(--ease-out); }
	.col-label { font-size: 0.72rem; color: var(--color-muted); padding-top: 0.25rem; border-top: 1px solid var(--color-base-300); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

	/* Likert: validated diverging palettes (disagree red ↔ agree blue, grey neutral). */
	.likert { display: grid; gap: 0.75rem; }
	.legend { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 0.35rem 0.9rem; font-size: 0.8rem; color: var(--color-muted); }
	.legend li { display: inline-flex; align-items: center; gap: 0.35rem; }
	.sw { width: 0.8rem; height: 0.8rem; border-radius: 3px; }
	.lk-row { display: grid; gap: 0.3rem; }
	.lk-label { font-weight: 600; }
	.lk-track { position: relative; height: 1.75rem; }
	.lk-mid { position: absolute; left: 50%; top: -3px; bottom: -3px; width: 1px; background: var(--color-field); }
	.lk-bar { display: flex; gap: 2px; height: 100%; transition: margin-left 500ms var(--ease-out); }
	.seg { position: relative; display: grid; place-items: center; height: 100%; min-width: 3px; transition: width 500ms var(--ease-out); outline: none; }
	.seg:first-child { border-radius: 4px 0 0 4px; }
	.seg:last-child { border-radius: 0 4px 4px 0; }
	.seg-label { font-size: 0.72rem; font-weight: 700; }
	.l5 { --s1: #c03b3a; --s2: #f0a09f; --s3: #e3e1dc; --s4: #86b6ef; --s5: #1c5cab; --i1: #fff; --i2: #17202b; --i3: #17202b; --i4: #17202b; --i5: #fff; }
	.l7 { --s1: #6e1b1a; --s2: #c63f3d; --s3: #f29c9a; --s4: #e8e6e1; --s5: #8fbcf3; --s6: #2f73cc; --s7: #123a70; --i1: #fff; --i2: #fff; --i3: #17202b; --i4: #17202b; --i5: #17202b; --i6: #fff; --i7: #fff; }
	:global([data-theme='quiz-dark']) .l5 { --s1: #fcc6c5; --s2: #d84846; --s3: #808187; --s4: #3b82e0; --s5: #bcd9fb; --i1: #17202b; --i2: #fff; --i3: #fff; --i4: #fff; --i5: #17202b; }
	:global([data-theme='quiz-dark']) .l7 { --s1: #fdd0cf; --s2: #f2807f; --s3: #b23736; --s4: #808187; --s5: #2459ab; --s6: #5a9cf0; --s7: #c4defc; --i1: #17202b; --i2: #17202b; --i3: #fff; --i4: #fff; --i5: #fff; --i6: #17202b; --i7: #17202b; }
	@media (prefers-color-scheme: dark) {
		:global(:root:not([data-theme='quiz'])) .l5 { --s1: #fcc6c5; --s2: #d84846; --s3: #808187; --s4: #3b82e0; --s5: #bcd9fb; --i1: #17202b; --i2: #fff; --i3: #fff; --i4: #fff; --i5: #17202b; }
		:global(:root:not([data-theme='quiz'])) .l7 { --s1: #fdd0cf; --s2: #f2807f; --s3: #b23736; --s4: #808187; --s5: #2459ab; --s6: #5a9cf0; --s7: #c4defc; --i1: #17202b; --i2: #17202b; --i3: #fff; --i4: #fff; --i5: #fff; --i6: #17202b; --i7: #17202b; }
	}
	.s1 { background: var(--s1); color: var(--i1); } .s2 { background: var(--s2); color: var(--i2); } .s3 { background: var(--s3); color: var(--i3); }
	.s4 { background: var(--s4); color: var(--i4); } .s5 { background: var(--s5); color: var(--i5); } .s6 { background: var(--s6); color: var(--i6); } .s7 { background: var(--s7); color: var(--i7); }

	/* Heat grid: one-hue ramp; ink flips to stay readable on dark cells. */
	.heat { display: grid; grid-template-columns: minmax(5rem, 1.2fr) repeat(var(--cols), minmax(2.75rem, 1fr)); gap: 2px; }
	@media (min-width: 640px) { .heat { grid-template-columns: minmax(7rem, 1.2fr) repeat(var(--cols), minmax(3.5rem, 1fr)); } }
	.hh { font-size: 0.78rem; font-weight: 600; color: var(--color-muted); text-align: center; padding: 0.25rem; }
	.hr { font-weight: 600; padding: 0.45rem 0.5rem 0.45rem 0; font-size: 0.9rem; }
	.cell { position: relative; display: grid; place-items: center; min-height: 2.5rem; border-radius: 4px; font-size: 0.85rem; font-weight: 700; color: var(--color-base-content); background: color-mix(in oklab, var(--color-primary) calc(var(--t) * 92%), var(--color-base-200)); transition: background-color 500ms var(--ease-out); outline: none; }
	.cell.ink { color: var(--color-primary-content); }

	/* Ranking. */
	.ranks { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.4rem; }
	.ranks li { display: flex; align-items: center; gap: 0.75rem; padding: 0.5rem 0.75rem; border: 1px solid var(--color-base-300); border-radius: var(--radius-field); background: var(--color-base-100); }
	.rank-n { width: 1.75rem; height: 1.75rem; border-radius: 999px; display: grid; place-items: center; font-weight: 700; background: var(--color-primary); color: var(--color-primary-content); flex: none; }
	.rank-label { flex: 1; font-weight: 600; min-width: 0; overflow-wrap: anywhere; }

	/* Answer cards. */
	.texts { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.6rem; grid-template-columns: repeat(auto-fill, minmax(min(16rem, 100%), 1fr)); }
	.texts.code { grid-template-columns: minmax(0, 1fr); }
	.text-card { padding: 0.75rem 0.9rem; border-radius: var(--radius-box); background: var(--color-base-100); border: 1px solid var(--color-base-300); overflow-wrap: anywhere; white-space: pre-wrap; }
	.big .text-card { font-size: 1.15rem; }
	.text-card pre { white-space: pre; overflow-x: auto; font-size: 0.85rem; background: var(--color-base-200); padding: 0.5rem; border-radius: 0.4rem; }
	.is-hidden { opacity: 0.5; border-style: dashed; }
	.files { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(min(18rem, 100%), 1fr)); }
	.file-card { padding: 0.75rem; border-radius: var(--radius-box); border: 1px solid var(--color-base-300); background: var(--color-base-100); }
	.thumb { width: 100%; max-height: 12rem; object-fit: contain; border-radius: 0.5rem; background: var(--color-base-200); }
</style>
