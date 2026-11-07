<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// What each column of the questions CSV means, and how to write each
	// question type (D-135), next to the template. Opens on demand.
	import { saveFile } from '../tutoring/types';
	import Icon from '../ui/Icon.svelte';
	import { COLUMNS, guideCSV, TYPES } from './csvGuide';

	let { open = false }: { open?: boolean } = $props();
	let view = $state<'columns' | 'types'>('columns');
	// Kept here, so switching views doesn't close it (as D-129).
	// svelte-ignore state_referenced_locally
	let shown = $state(open);
</script>

<details class="guide card card-border bg-base-100" open={shown} ontoggle={(e) => (shown = e.currentTarget.open)}>
	<summary class="flex flex-wrap items-center gap-2 p-3 sm:px-4 cursor-pointer">
		<Icon name="info" size={16} />
		<strong>{tr("What goes in each column")}</strong>
		<span class="small muted grow">{tr("{0} required, {1} optional; how to write each question type", [COLUMNS.filter((c) => c.required).length, COLUMNS.length - COLUMNS.filter((c) => c.required).length])}</span>
	</summary>
	<div class="px-3 pb-4 sm:px-4 vstack gap-3">
		<div class="flex flex-wrap items-center gap-2">
			<div class="join" role="tablist" aria-label={tr("Guide")}>
				<button class="btn btn-sm join-item" role="tab" aria-selected={view === 'columns'} class:btn-primary={view === 'columns'} onclick={() => (view = 'columns')}>{tr("Columns")}</button>
				<button class="btn btn-sm join-item" role="tab" aria-selected={view === 'types'} class:btn-primary={view === 'types'} onclick={() => (view = 'types')}>{tr("Question types")}</button>
			</div>
			<span class="grow"></span>
			<a class="btn btn-sm" href="/api/teacher/quiz-template.csv" download><Icon name="download" size={14} />{tr("Template")}</a>
			<button class="btn btn-sm btn-ghost" onclick={() => saveFile('questions-csv-guide.csv', guideCSV())}><Icon name="download" size={14} />{tr("This guide")}</button>
		</div>
		<p class="m-0 small muted">{tr("One row per question; the first row holds the column names. Columns can be in any order, and optional ones can be left out. Text with a comma goes in \"double quotes\". Save as CSV (UTF-8) from Excel or Google Sheets.")}</p>

		{#if view === 'columns'}
			<div class="table-wrap">
				<table class="table table-sm">
					<thead><tr><th>{tr("Column")}</th><th>{tr("What it means")}</th><th>{tr("How to write it")}</th><th>{tr("Example")}</th></tr></thead>
					<tbody>
						{#each COLUMNS as c (c.name)}
							<tr>
								<td class="whitespace-nowrap align-top"><code>{c.name}</code>{#if c.required}<br /><span class="badge badge-soft badge-primary badge-xs">{tr("required")}</span>{/if}</td>
								<td class="align-top">{c.means}</td>
								<td class="align-top small">{c.format}</td>
								<td class="align-top"><code class="ex">{c.example}</code></td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<div class="types">
				{#each TYPES as t (t.type)}
					<section class="type">
						<div class="flex flex-wrap items-baseline gap-2"><code class="font-semibold">{t.type}</code><span class="small muted">{tr(t.label)}</span></div>
						<dl>
							<dt>{tr("options")}</dt><dd>{t.options}</dd>
							<dt>{tr("correct_answer")}</dt><dd>{t.answer}</dd>
						</dl>
						<div class="small flex flex-col gap-0.5 ex-box">
							<span class="muted">{tr("Example")}</span>
							{#if t.example.text}<span><span class="muted">{tr("question_text")}</span> <code class="ex">{t.example.text}</code></span>{/if}
							{#if t.example.options}<span><span class="muted">{tr("options")}</span> <code class="ex">{t.example.options}</code></span>{/if}
							{#if t.example.answer}<span><span class="muted">{tr("correct_answer")}</span> <code class="ex">{t.example.answer}</code></span>{/if}
						</div>
					</section>
				{/each}
			</div>
		{/if}
	</div>
</details>

<style>
	.guide > summary::-webkit-details-marker { display: none; }
	.guide > summary { list-style: none; }
	.ex { white-space: pre-wrap; word-break: break-word; font-size: 0.8rem; }
	.types { display: grid; gap: 0.6rem; grid-template-columns: repeat(auto-fill, minmax(min(100%, 18rem), 1fr)); }
	.type { display: flex; flex-direction: column; gap: 0.4rem; padding: 0.7rem 0.8rem; border: 1px solid var(--color-base-300); }
	dl { display: grid; grid-template-columns: auto 1fr; gap: 0.15rem 0.6rem; margin: 0; font-size: 0.85rem; }
	dt { color: var(--color-base-content); opacity: 0.6; font-family: ui-monospace, monospace; font-size: 0.78rem; }
	dd { margin: 0; }
	.ex-box { margin-top: auto; padding-top: 0.4rem; border-top: 1px dashed var(--color-base-300); }
</style>
