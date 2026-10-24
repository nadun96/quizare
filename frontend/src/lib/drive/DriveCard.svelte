<script lang="ts">
	// A teacher's Google Drive connection (D-71): connect, save results after
	// each session, and what was saved lately.
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '../api';
	import { connectUrl, driveError, driveStatus, type DriveExport, type DriveStatus } from '../drive';
	import Icon from '../ui/Icon.svelte';
	import OptionGroup from '../ui/OptionGroup.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';

	let st = $state<DriveStatus | null>(null);
	let exports = $state<DriveExport[]>([]);

	async function load() {
		st = await driveStatus(true);
		if (st.connected) exports = (await api.get<{ exports: DriveExport[] }>('/api/teacher/drive/exports')).exports;
	}
	$effect(() => {
		load();
	});
	// Coming back from Google: once, not as an effect (a toast changes state an effect would track).
	onMount(() => {
		const q = page.url.searchParams;
		if (q.get('drive') === 'connected') toast('Google Drive connected');
		else if (q.get('drive_error')) toast(driveError(q.get('drive_error')), 'error');
		if (q.has('drive') || q.has('drive_error')) history.replaceState(history.state, '', page.url.pathname);
	});

	async function setAuto(v: string) {
		try {
			st = await api.put<DriveStatus>('/api/teacher/drive', { auto_backup: v === 'on' });
			toast(st.auto_backup ? 'Each session’s results will be saved to Drive when it ends' : 'Results are saved only when you ask');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not save', 'error');
		}
	}
	async function disconnect() {
		if (!(await confirmDialog({ title: 'Disconnect Google Drive?', body: 'Files attached to classrooms stay as links, but the platform stops keeping their sharing up to date, and results are no longer saved to Drive. Nothing is deleted from your Drive.', confirm: 'Disconnect' }))) return;
		await api.del('/api/teacher/drive');
		toast('Google Drive disconnected');
		await load();
	}
	const when = (s: string) => new Date(s).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
</script>

{#if st?.available}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
		<h2 class="mt-0 flex items-center gap-2"><Icon name="cloud" size={20} />Google Drive</h2>
		{#if !st.connected}
			<p class="m-0">Attach Drive files to classrooms and questions, import questions from a Google Sheet, and save results to Drive.</p>
			<p class="small muted m-0">The platform only sees the files you choose and the ones it makes, never the rest of your Drive.</p>
			<div><a class="btn btn-primary" href={connectUrl('/t/settings')} data-sveltekit-reload><Icon name="link" size={16} />Connect Google Drive</a></div>
		{:else}
			<div class="flex flex-wrap items-center gap-3">
				<span class="badge {st.reconnect ? 'badge-warning' : 'badge-soft ok'}">{st.reconnect ? 'Needs reconnecting' : 'Connected'}</span>
				<span class="min-w-0 flex-1 truncate">{st.email}</span>
				{#if st.reconnect}<a class="btn btn-sm btn-primary" href={connectUrl('/t/settings')} data-sveltekit-reload>Reconnect</a>{/if}
				<button class="btn btn-sm" onclick={disconnect}>Disconnect</button>
			</div>
			{#if st.reconnect}<p class="alert alert-soft alert-warning m-0">Google no longer accepts the connection (it was removed in your Google account, or expired). Reconnect to keep using Drive.</p>{/if}
			<div class="vstack gap-1">
				<span class="font-semibold">Results</span>
				<OptionGroup label="Save results to Drive" value={st.auto_backup ? 'on' : 'off'} onchange={setAuto} options={[
					{ v: 'on', label: 'After each session', icon: 'check', hint: 'When a session ends, its results are saved as a Google Sheet.' },
					{ v: 'off', label: 'When I ask', icon: 'cloud', hint: 'Use “Save to Drive” on a session’s or poll’s results.' }
				]} />
				{#if st.folder_link}<p class="small m-0"><a href={st.folder_link} target="_blank" rel="noopener noreferrer">Open the results folder</a> <span class="muted">(“Classroom Quiz results” in your Drive)</span></p>{/if}
			</div>
			{#if exports.length}
				<details>
					<summary class="small">Saved lately ({exports.length})</summary>
					<ul class="exports">
						{#each exports as e (e.id)}
							<li>
								<Icon name={e.status === 'done' ? 'check-circle' : 'alert'} size={15} />
								<span class="min-w-0 flex-1">
									{#if e.web_link}<a href={e.web_link} target="_blank" rel="noopener noreferrer">{e.name}</a>{:else}{e.name}{/if}
									<span class="small muted">· {when(e.created_at)}{e.automatic ? ' · automatic' : ''}</span>
									{#if e.error}<br /><span class="small text-error">{e.error}</span>{/if}
								</span>
							</li>
						{/each}
					</ul>
				</details>
			{/if}
		{/if}
	</div>
{/if}

<style>
	.exports { list-style: none; margin: 0.5rem 0 0; padding: 0; display: grid; gap: 0.35rem; }
	.exports li { display: flex; gap: 0.5rem; align-items: flex-start; }
	.exports li :global(svg) { flex: none; margin-top: 0.2rem; color: var(--color-success); }
</style>
