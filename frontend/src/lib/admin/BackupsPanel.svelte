<script lang="ts">
	// Backups (PL-FR-07 to PL-FR-09, D-57): export a full, encrypted backup,
	// follow it while it runs (the page can be left), and see the history of
	// console and nightly backups, one page at a time.
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '../api';
	import { Paged } from '../paged.svelte';
	import { fmtBytes } from '../staff';
	import { urlState } from '../urlstate';
	import { confirmDialog } from '../ui/dialog.svelte';
	import Pager from '../ui/Pager.svelte';
	import SortHeader from '../ui/SortHeader.svelte';
	import { toast } from '../ui/toast.svelte';

	type Backup = {
		id: string;
		kind: 'console' | 'nightly';
		name: string;
		status: 'running' | 'done' | 'failed';
		created_by?: string;
		started_at: string;
		finished_at?: string;
		size_bytes: number;
		tables_done: number;
		tables_total: number;
		rows_done: number;
		error?: string;
		on_server: boolean;
		deleted_at?: string;
	};
	type Nightly = { configured: boolean; last_run_at?: string; last_run_ok: boolean; last_success_at?: string; stale: boolean };

	let kind = $state(Paged.fromUrl(urlState, 'kind', 'bk'));
	const list = new Paged<Backup>(() => '/api/admin/backups', 'backups', { url: urlState, prefix: 'bk', sort: 'started', desc: true, filters: () => ({ kind }) });
	let nightly = $state<Nightly | null>(null);
	let pass = $state('');
	let pass2 = $state('');
	let errors = $state<Record<string, string>>({});
	let starting = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	// Follow a running backup until it ends.
	async function reload() {
		const res = await api.get<{ nightly: Nightly }>('/api/admin/backups?size=1');
		nightly = res.nightly;
		await list.load();
		clearTimeout(timer);
		if (list.rows.some((b) => b.status === 'running')) timer = setTimeout(reload, 2000);
	}
	reload();
	onDestroy(() => clearTimeout(timer));

	async function start(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		if (pass !== pass2) {
			errors = { passphrase2: "The two passphrases don't match." };
			return;
		}
		starting = true;
		try {
			await api.post('/api/admin/backups', { passphrase: pass });
			pass = pass2 = '';
			toast('Backup started; you can leave this page while it runs');
			reload();
		} catch (err) {
			if (err instanceof ApiError) errors = Object.keys(err.fields).length ? err.fields : { _: err.message };
			else throw err;
		} finally {
			starting = false;
		}
	}
	async function download(b: Backup) {
		try {
			const { url } = await api.post<{ url: string }>('/api/admin/backups/' + b.id + '/link');
			const a = document.createElement('a');
			a.href = url;
			a.download = b.name;
			document.body.append(a);
			a.click();
			a.remove();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'Download failed', 'error');
		}
	}
	async function remove(b: Backup) {
		if (!(await confirmDialog({ title: 'Delete this backup?', body: `${b.name} (${fmtBytes(b.size_bytes)}) is removed from the server. Copies you downloaded are not affected.`, confirm: 'Delete', danger: true }))) return;
		try {
			await api.del('/api/admin/backups/' + b.id);
			list.load();
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'Delete failed', 'error');
		}
	}
	const when = (s?: string) => (s ? new Date(s).toLocaleString() : '');
	const pct = (b: Backup) => (b.tables_total ? Math.round((b.tables_done / b.tables_total) * 100) : 0);
</script>

<div class="vstack">
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={start}>
		<h2 class="m-0 text-base">Export a backup</h2>
		<p class="small m-0">A backup holds the whole database: accounts, classrooms, quizzes, answers, results, polls with their uploaded files, and the audit log. It is compressed and encrypted with the passphrase you choose.</p>
		<ul class="small muted m-0 ps-5 list-disc">
			<li><strong>The passphrase isn't stored anywhere.</strong> Without it the backup can't be opened, by you or anyone.</li>
			<li><strong>Not included:</strong> the master key, which encrypts teachers' AI keys. Keep a copy of it safe, separately; restoring without it means teachers re-enter their keys. Recordings aren't included either (they're kept 2 days).</li>
			<li>Restore on the server with <code>server restore-backup</code> into a new, empty database (see the deployment guide).</li>
		</ul>
		<div class="pp">
			<div>
				<label for="bk-pass">Passphrase</label>
				<input id="bk-pass" class="input w-full" type="password" autocomplete="new-password" minlength="12" required bind:value={pass} aria-invalid={!!errors.passphrase} aria-describedby="bk-pass-h" />
				<p id="bk-pass-h" class="small muted m-0">At least 12 characters. A few unrelated words work well.</p>
				{#if errors.passphrase}<p class="field-error">{errors.passphrase}</p>{/if}
			</div>
			<div>
				<label for="bk-pass2">Passphrase again</label>
				<input id="bk-pass2" class="input w-full" type="password" autocomplete="new-password" required bind:value={pass2} aria-invalid={!!errors.passphrase2} />
				{#if errors.passphrase2}<p class="field-error">{errors.passphrase2}</p>{/if}
			</div>
		</div>
		{#if errors._}<p class="alert alert-soft alert-error small m-0" role="alert">{errors._}</p>{/if}
		<div><button class="btn btn-primary" disabled={starting || list.rows.some((b) => b.status === 'running')}>Start backup</button></div>
	</form>

	{#if nightly?.configured}
		<div class="alert alert-soft {nightly.stale ? 'alert-warning' : 'alert-info'} small" role={nightly.stale ? 'alert' : undefined}>
			<span>
				<strong>Nightly backup:</strong>
				{#if nightly.last_run_at}last ran {when(nightly.last_run_at)}, {nightly.last_run_ok ? 'succeeded' : 'failed'}.{:else}no run recorded yet.{/if}
				{#if nightly.stale}It hasn't succeeded for 2 days{nightly.last_success_at ? ` (last success ${when(nightly.last_success_at)})` : ''}; check the server's backup job.{/if}
			</span>
		</div>
	{/if}

	<div class="row">
		<h2 class="m-0 text-base">History</h2>
		<span class="spacer"></span>
		<select class="select select-sm w-auto" bind:value={kind} onchange={() => list.refilter()} aria-label="Kind of backup"><option value="">All backups</option><option value="console">From the console</option><option value="nightly">Nightly</option></select>
	</div>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
		<table class="table">
			<thead><tr><th><SortHeader {list} key="started" label="Started" desc /></th><th>Made by</th><th>Status</th><th><SortHeader {list} key="size" label="Size" desc /></th><th><span class="sr-only">Actions</span></th></tr></thead>
			<tbody>
				{#each list.rows as b (b.id)}
					<tr>
						<td class="small tabular">{when(b.started_at)}<br /><span class="muted">{b.name}</span></td>
						<td class="small">{b.kind === 'nightly' ? 'Nightly job' : b.created_by || '—'}</td>
						<td class="small">
							{#if b.status === 'running'}
								<span class="badge badge-soft warn">running</span>
								<progress class="progress w-24 align-middle" value={pct(b)} max="100" aria-label="Backup progress">{pct(b)}%</progress>
								<span class="muted tabular">{b.tables_done}/{b.tables_total || '…'} tables</span>
							{:else if b.status === 'failed'}
								<span class="badge badge-soft danger">failed</span> <span class="muted">{b.error}</span>
							{:else if b.on_server}
								<span class="badge badge-soft ok">on the server</span>
							{:else}
								<span class="badge badge-soft">deleted</span> <span class="muted">{when(b.deleted_at)}</span>
							{/if}
						</td>
						<td class="small tabular">{b.status === 'done' ? fmtBytes(b.size_bytes) : ''}</td>
						<td>
							<div class="flex flex-wrap gap-1">
								{#if b.status === 'done' && b.on_server}
									<button class="btn btn-sm" onclick={() => download(b)}>Download</button>
									{#if b.kind === 'console'}<button class="btn btn-sm btn-error btn-outline" onclick={() => remove(b)}>Delete</button>{/if}
								{/if}
							</div>
						</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">{list.loaded ? 'No backups yet.' : 'Loading…'}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
	<Pager {list} label="Backups" />
</div>

<style>
	.pp { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr)); }
</style>
