<script lang="ts">
	// Storage (PL-FR-04 to PL-FR-06, D-57): the space the platform uses, its
	// limits, and clean-ups. "Storage" shows the figures and limits;
	// "Clean-up" deletes, always after a preview the admin confirms.
	import { onMount } from 'svelte';
	import { api, ApiError } from '../api';
	import { fmtBytes } from '../staff';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import GrowthChart from './GrowthChart.svelte';

	type Overview = {
		taken_at: string;
		db_bytes: number;
		areas: Record<string, number>;
		backups_bytes: number;
		recordings_bytes: number;
		disk_free: number;
		disk_total: number;
		limits: { recording_limit_bytes: number; backup_space_bytes: number };
		fixed_limits: { label: string; value: string }[];
		growth: { day: string; db_bytes: number; backups_bytes: number }[];
	};
	type Area = { id: string; label: string };
	type Freed = { items: number; bytes: number };

	let { canStorage, canCleanup }: { canStorage: boolean; canCleanup: boolean } = $props();

	const AREA_LABELS: Record<string, string> = {
		accounts: 'Accounts',
		classrooms: 'Classrooms and content',
		quizzes_sessions: 'Quizzes and live sessions',
		answers: 'Answers',
		polls: 'Polls',
		poll_uploads: 'Poll uploaded files',
		whiteboards: 'Whiteboards',
		results: 'Results and analytics',
		audit: 'Audit log',
		ai_marking: 'AI keys and marking',
		profile_pictures: 'Profile pictures',
		settings: 'Settings',
		storage: 'Storage figures and backup history',
		jobs: 'Background jobs',
		system: 'Database system tables',
		other: 'Other'
	};

	let o = $state<Overview | null>(null);
	let refreshing = $state(false);
	let gbRec = $state('');
	let gbBak = $state('');
	let limitErrors = $state<Record<string, string>>({});
	const GB = 1024 ** 3;

	async function load(refresh = false) {
		refreshing = refresh;
		try {
			o = refresh ? await api.post<Overview>('/api/admin/storage/refresh') : await api.get<Overview>('/api/admin/storage');
			gbRec = String(+(o.limits.recording_limit_bytes / GB).toFixed(2));
			gbBak = String(+(o.limits.backup_space_bytes / GB).toFixed(2));
		} finally {
			refreshing = false;
		}
	}

	const areas = $derived(o ? Object.entries(o.areas).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]) : []);
	const areaMax = $derived(Math.max(1, ...areas.map(([, v]) => v)));
	const lowDisk = $derived(!!o && o.disk_total > 0 && o.disk_free / o.disk_total < 0.1);

	async function saveLimits(e: SubmitEvent) {
		e.preventDefault();
		limitErrors = {};
		try {
			await api.put('/api/admin/storage/limits', { recording_limit_bytes: Math.round(Number(gbRec) * GB), backup_space_bytes: Math.round(Number(gbBak) * GB) });
			toast('Limits saved');
			load();
		} catch (err) {
			if (err instanceof ApiError) limitErrors = err.fields;
			else throw err;
		}
	}

	// ---------- clean-up ----------
	let cleanAreas = $state<Area[]>([]);
	let area = $state('');
	const ago = new Date(Date.now() - 90 * 864e5).toISOString().slice(0, 10);
	let before = $state(ago);
	let preview = $state<Freed | null>(null);
	let cleanError = $state('');
	let busy = $state(false);
	onMount(() => {
		if (canStorage) load();
		if (canCleanup)
			api.get<{ areas: Area[] }>('/api/admin/storage/cleanup').then((r) => {
				cleanAreas = r.areas;
				area = r.areas[0]?.id ?? '';
			});
	});
	const areaLabel = $derived(cleanAreas.find((a) => a.id === area)?.label ?? '');

	async function runPreview() {
		cleanError = '';
		preview = null;
		try {
			preview = await api.post<Freed>('/api/admin/storage/cleanup/preview', { area, before });
		} catch (err) {
			cleanError = err instanceof ApiError ? Object.values(err.fields)[0] || err.message : 'Preview failed';
		}
	}
	async function runCleanup() {
		if (!preview || !preview.items) return;
		const ok = await confirmDialog({
			title: `Delete ${preview.items} ${preview.items === 1 ? 'item' : 'items'}?`,
			body: `${areaLabel} (before ${before}). This frees about ${fmtBytes(preview.bytes)} and can't be undone.`,
			confirm: 'Delete',
			danger: true
		});
		if (!ok) return;
		busy = true;
		try {
			const f = await api.post<Freed>('/api/admin/storage/cleanup', { area, before });
			toast(`Deleted ${f.items} ${f.items === 1 ? 'item' : 'items'}, freeing ${fmtBytes(f.bytes)}`);
			preview = null;
			if (canStorage) load(true);
		} finally {
			busy = false;
		}
	}
</script>

<div class="vstack">
	{#if canStorage}
		{#if !o}
			<p class="muted">Measuring…</p>
		{:else}
			<div class="row">
				<p class="small muted m-0">Measured {new Date(o.taken_at).toLocaleString()} · updated every hour</p>
				<span class="spacer"></span>
				<button class="btn btn-sm" onclick={() => load(true)} disabled={refreshing}>{refreshing ? 'Measuring…' : 'Refresh'}</button>
			</div>
			<div class="auto-grid" role="list">
				<div class="card card-border bg-base-100 shadow-sm p-4" role="listitem"><div class="small muted">Database</div><div class="figure tabular">{fmtBytes(o.db_bytes)}</div></div>
				<div class="card card-border bg-base-100 shadow-sm p-4" role="listitem"><div class="small muted">Backups on the server</div><div class="figure tabular">{fmtBytes(o.backups_bytes)}</div><div class="small muted">of {fmtBytes(o.limits.backup_space_bytes)} kept</div></div>
				<div class="card card-border bg-base-100 shadow-sm p-4" role="listitem"><div class="small muted">Recordings</div><div class="figure tabular">{fmtBytes(o.recordings_bytes)}</div><div class="small muted">of {fmtBytes(o.limits.recording_limit_bytes)} · arrive with tutoring</div></div>
				<div class="card card-border bg-base-100 shadow-sm p-4" role="listitem">
					<div class="small muted">Free disk space</div>
					<div class="figure tabular">{fmtBytes(o.disk_free)}</div>
					<div class="small {lowDisk ? 'text-warning' : 'muted'}">{lowDisk ? 'Low: ' : ''}of {fmtBytes(o.disk_total)} on the backup folder's disk</div>
				</div>
			</div>

			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
				<h2 class="m-0 text-base">Database by area</h2>
				<ul class="bars" aria-label="Database size by area">
					{#each areas as [k, v] (k)}
						<li>
							<span class="lbl small">{AREA_LABELS[k] ?? k}</span>
							<span class="track" aria-hidden="true"><span class="bar" style="width: {Math.max(0.5, (v / areaMax) * 100)}%"></span></span>
							<span class="val small tabular">{fmtBytes(v)}</span>
						</li>
					{/each}
				</ul>
			</div>

			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
				<h2 class="m-0 text-base">Database size, last 30 days</h2>
				<GrowthChart points={o.growth} />
			</div>

			<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={saveLimits}>
				<h2 class="m-0 text-base">Limits</h2>
				<div class="limits">
					<div>
						<label for="lim-rec">Recordings, all together (GB)</label>
						<input id="lim-rec" class="input w-full" type="number" min="0.1" step="0.1" bind:value={gbRec} aria-invalid={!!limitErrors.recording_limit_bytes} aria-describedby="lim-rec-h" />
						<p id="lim-rec-h" class="small muted m-0">Tutoring recordings are kept 2 days within this limit (5 GB by default).</p>
						{#if limitErrors.recording_limit_bytes}<p class="field-error">{limitErrors.recording_limit_bytes}</p>{/if}
					</div>
					<div>
						<label for="lim-bak">Backups kept on the server (GB)</label>
						<input id="lim-bak" class="input w-full" type="number" min="0.1" step="0.1" bind:value={gbBak} aria-invalid={!!limitErrors.backup_space_bytes} aria-describedby="lim-bak-h" />
						<p id="lim-bak-h" class="small muted m-0">Past this, the oldest console backups are deleted. The newest is always kept.</p>
						{#if limitErrors.backup_space_bytes}<p class="field-error">{limitErrors.backup_space_bytes}</p>{/if}
					</div>
				</div>
				<div><button class="btn">Save limits</button></div>
				<details class="small">
					<summary>Fixed limits</summary>
					<table class="table table-sm"><tbody>{#each o.fixed_limits as f (f.label)}<tr><td>{f.label}</td><td class="tabular">{f.value}</td></tr>{/each}</tbody></table>
				</details>
			</form>
		{/if}
	{/if}

	{#if canCleanup}
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
			<h2 class="m-0 text-base">Clean up</h2>
			<p class="small muted m-0">Nothing is deleted until you've seen what will go and confirmed. Answers whose files are removed stay, marked as removed.</p>
			<div class="limits">
				<div>
					<label for="cl-area">What</label>
					<select id="cl-area" class="select w-full" bind:value={area} onchange={() => (preview = null)}>
						{#each cleanAreas as a (a.id)}<option value={a.id}>{a.label}</option>{/each}
					</select>
				</div>
				<div>
					<label for="cl-before">From before</label>
					<input id="cl-before" class="input w-full" type="date" bind:value={before} max={new Date().toISOString().slice(0, 10)} onchange={() => (preview = null)} />
				</div>
			</div>
			<div class="row">
				<button class="btn" onclick={runPreview} disabled={!area || !before}>Preview</button>
				{#if preview}
					<span class="small" aria-live="polite">{preview.items ? `${preview.items} ${preview.items === 1 ? 'item' : 'items'}, about ${fmtBytes(preview.bytes)}` : 'Nothing to delete.'}</span>
					{#if preview.items}<button class="btn btn-error btn-outline" onclick={runCleanup} disabled={busy}>Delete…</button>{/if}
				{/if}
			</div>
			{#if cleanError}<p class="field-error m-0">{cleanError}</p>{/if}
		</div>
	{/if}
</div>

<style>
	.figure { font-size: 1.5rem; font-weight: 700; }
	.bars { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.45rem; }
	.bars li { display: grid; grid-template-columns: minmax(8rem, 14rem) 1fr 5.5rem; align-items: center; gap: 0.75rem; }
	.track { height: 0.75rem; }
	.bar { display: block; height: 100%; background: var(--cat-1); border-radius: 0 4px 4px 0; }
	.val { text-align: right; }
	.limits { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr)); }
	@media (max-width: 30rem) {
		.bars li { grid-template-columns: 1fr 5rem; }
		.track { grid-column: 1 / -1; grid-row: 2; }
	}
</style>
