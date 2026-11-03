<script lang="ts">
	// Importing questions into pools (D-123). Drop one or many CSV files; each
	// goes where its name says ("Classroom__Category.csv"), or where the
	// teacher picks. Every file is checked first and shown as it would be
	// saved; nothing is saved until the teacher approves it. Codes already in
	// the pool are renumbered (Q1 → Q1-2), so files from the same template fit.
	import CsvGuide from '../quiz/CsvGuide.svelte';
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { categoryLine, fromFileName, tidy, type ImportResult, type PoolSummary } from './pools';

	let { pools, classroomId = '', onimported }: { pools: PoolSummary[]; classroomId?: string; onimported?: () => void } = $props();

	type Entry = { key: number; file: File; target: string; category: string; preview: ImportResult | null; error: string; state: 'checking' | 'ready' | 'saving' | 'saved' | 'error'; saved?: ImportResult; open: boolean };
	let entries = $state<Entry[]>([]);
	let seq = 0;
	let over = $state(false);

	// Targets: "bank", or a classroom's id.
	const rooms = $derived(pools.filter((p) => p.kind === 'pool').map((p) => ({ id: p.classroom_id!, name: p.classroom! })));
	const targetName = (t: string) => (t === 'bank' ? 'My question bank' : (rooms.find((r) => r.id === t)?.name ?? 'Choose where'));

	async function poolId(target: string): Promise<string> {
		if (target === 'bank') return pools.find((p) => p.kind === 'bank')!.quiz_id;
		const p = pools.find((x) => x.classroom_id === target);
		if (p?.quiz_id) return p.quiz_id;
		const q = await api.get<{ id: string }>(`/api/teacher/classrooms/${target}/pool`);
		if (p) p.quiz_id = q.id;
		return q.id;
	}
	async function send(e: Entry, preview: boolean) {
		const id = await poolId(e.target);
		const form = new FormData();
		form.append('file', e.file);
		const qs = new URLSearchParams({ rename: '1', category: tidy(e.category) });
		if (preview) qs.set('preview', '1');
		return api.post<ImportResult>(`/api/teacher/quizzes/${id}/questions/import?${qs}`, form);
	}
	async function check(e: Entry) {
		if (!e.target) {
			e.state = 'error';
			e.error = 'Choose where these questions go.';
			return;
		}
		e.state = 'checking';
		e.error = '';
		try {
			e.preview = await send(e, true);
			e.state = 'ready';
		} catch (err) {
			e.state = 'error';
			e.error = err instanceof ApiError ? err.message : 'The file couldn’t be read';
		}
	}
	function add(files: FileList | File[]) {
		for (const file of files) {
			if (!/\.csv$/i.test(file.name)) {
				toast(`${file.name}: only CSV files`, 'error');
				continue;
			}
			const guess = fromFileName(file.name, rooms);
			const target = classroomId || guess.classroomId || (rooms.length ? '' : 'bank');
			const e: Entry = { key: ++seq, file, target, category: guess.category, preview: null, error: '', state: 'checking', open: false };
			entries = [...entries, e];
			check(entries[entries.length - 1]);
		}
	}
	async function approve(e: Entry) {
		e.state = 'saving';
		try {
			e.saved = await send(e, false);
			e.state = 'saved';
			toast(`${e.saved.imported} questions saved to ${targetName(e.target)}`);
			onimported?.();
		} catch (err) {
			e.state = 'error';
			e.error = err instanceof ApiError ? err.message : 'Saving failed';
		}
	}
	async function approveAll() {
		for (const e of entries.filter((x) => x.state === 'ready' && x.preview?.imported)) await approve(e);
	}
	const ready = $derived(entries.filter((e) => e.state === 'ready' && e.preview?.imported));
	function drop(ev: DragEvent) {
		ev.preventDefault();
		over = false;
		if (ev.dataTransfer?.files) add(ev.dataTransfer.files);
	}
</script>

<div class="vstack gap-3">
	<label class="drop" class:over ondragover={(ev) => { ev.preventDefault(); over = true; }} ondragleave={() => (over = false)} ondrop={drop}>
		<Icon name="upload" size={22} />
		<span><strong>Drop CSV files here</strong> or choose them. Each is checked and shown before anything is saved.</span>
		<span class="small muted">{classroomId ? 'Name a file after its category, like “Algebra.csv”, or give a category column.' : 'Name each file “Classroom__Category.csv” (or “Classroom - Category.csv”) and it goes to that classroom’s pool, in that category. A category column in the file wins.'}</span>
		<input type="file" accept=".csv,text/csv" multiple class="sr-only" onchange={(ev) => { add(ev.currentTarget.files ?? []); ev.currentTarget.value = ''; }} />
	</label>
	<p class="m-0 small muted">Use the <a href="/api/teacher/quiz-template.csv" download>CSV template</a>: one row per question, with a <code>category</code> column (or leave it out and name the file after the category).</p>
	<CsvGuide />

	{#each entries as e (e.key)}
		<div class="card card-border bg-base-100 p-3 vstack gap-2" class:saved={e.state === 'saved'}>
			<div class="flex flex-wrap items-center gap-2">
				<Icon name="table" size={16} />
				<strong class="truncate grow">{e.file.name}</strong>
				{#if e.state === 'checking' || e.state === 'saving'}<span class="loading loading-spinner loading-xs"></span>{/if}
				{#if e.state === 'saved'}<span class="badge badge-soft ok">Saved · {e.saved?.imported}</span>{/if}
				<button class="btn btn-ghost btn-xs" aria-label="Remove {e.file.name} from the list" onclick={() => (entries = entries.filter((x) => x.key !== e.key))}><Icon name="x" size={14} /></button>
			</div>
			{#if e.state !== 'saved'}
				<div class="flex flex-wrap items-end gap-2">
					{#if !classroomId}
						<label class="vstack gap-1 small">Goes to
							<select class="select select-sm" bind:value={e.target} onchange={() => check(e)}>
								<option value="" disabled>Choose…</option>
								{#each rooms as r (r.id)}<option value={r.id}>{r.name}: question pool</option>{/each}
								<option value="bank">My question bank</option>
							</select>
						</label>
					{/if}
					<label class="vstack gap-1 small">Category for rows without one
						<input class="input input-sm" bind:value={e.category} placeholder="e.g. Algebra" onchange={() => check(e)} maxlength="60" />
					</label>
				</div>
			{/if}
			{#if e.error}<p class="m-0 small text-error">{e.error}</p>{/if}
			{#if e.preview && e.state !== 'saved'}
				<p class="m-0 small">
					<strong>{e.preview.imported}</strong> ready for <strong>{targetName(e.target)}</strong>{#if e.preview.imported}: {categoryLine(e.preview.categories)}{/if}.
					{#if e.preview.renamed}{e.preview.renamed} codes already used are renumbered.{/if}
					{#if e.preview.rejected.length}<span class="text-error">{e.preview.rejected.length} {e.preview.rejected.length === 1 ? 'row has' : 'rows have'} problems and won’t be saved.</span>{/if}
				</p>
				<details bind:open={e.open}>
					<summary class="small">Show the {e.preview.rows.length} rows</summary>
					<div class="table-wrap mt-2 rows">
						<table class="table table-xs">
							<thead><tr><th>Row</th><th>Code</th><th>Type</th><th>Question</th><th>Category</th><th class="text-right">Marks</th><th>Problem</th></tr></thead>
							<tbody>
								{#each e.preview.rows as r (r.row)}
									<tr class:bad={r.errors}>
										<td class="tabular">{r.row}</td>
										<td class="tabular">{r.renamed ? `${r.code} → ${r.renamed}` : r.code}</td>
										<td>{r.type}</td>
										<td>{r.text}</td>
										<td>{r.category || '—'}</td>
										<td class="tabular text-right">{r.marks}</td>
										<td class="text-error">{r.errors ? Object.values(r.errors).join('; ') : ''}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</details>
				<div class="flex gap-2">
					<button class="btn btn-sm btn-primary" onclick={() => approve(e)} disabled={!e.preview.imported || e.state === 'saving'}>Approve and save {e.preview.imported}</button>
				</div>
			{/if}
		</div>
	{/each}
	{#if ready.length > 1}
		<div><button class="btn btn-primary" onclick={approveAll}>Approve all {ready.length} files ({ready.reduce((n, e) => n + (e.preview?.imported ?? 0), 0)} questions)</button></div>
	{/if}
</div>

<style>
	.drop { display: flex; flex-direction: column; align-items: center; gap: 0.35rem; padding: 1.25rem; text-align: center; border: 2px dashed var(--color-base-300); background: var(--color-base-100); cursor: pointer; }
	.drop.over, .drop:hover { border-color: var(--color-primary); }
	.drop:focus-within { outline: 2px solid var(--color-primary); }
	.rows { max-height: 20rem; overflow: auto; }
	tr.bad td { background: color-mix(in oklab, var(--color-error) 8%, transparent); }
	.saved { opacity: 0.8; }
</style>
