<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Every tutoring recording, for the admin (D-80, TS-FR-78, PO-22, PO-24):
	// see and delete any, and set the space all recordings may use. The list
	// comes from the tutoring service, a page at a time.
	import { ApiError } from '../api';
	import { fmtBytes } from '../staff';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import RecordingsList from '../tutoring/RecordingsList.svelte';
	import type { Recording } from '../tutoring/types';
	import { toast } from '../ui/toast.svelte';

	let { meId }: { meId: string } = $props();
	const client = new TutorClient();
	let enabled = $state<boolean | null>(null);
	let list = $state<Recording[]>([]);
	let total = $state(0);
	let page = $state(1);
	let space = $state<{ used: number; limit: number } | null>(null);
	let limitGB = $state('');
	const size = 25;

	async function load() {
		const r = await client.get<{ recordings: Recording[]; total: number; space: { used: number; limit: number } }>(`/api/admin/recordings?page=${page}&size=${size}`);
		list = r.recordings;
		total = r.total;
		space = r.space;
		limitGB = (r.space.limit / 2 ** 30).toFixed(1).replace(/\.0$/, '');
	}
	$effect(() => {
		tutoringConfig().then((c) => {
			enabled = c.enabled;
			if (c.enabled) load().catch((e) => toast(e instanceof ApiError ? e.message : tr("Tutoring didn’t answer"), 'error'));
		});
	});
	async function saveLimit(e: SubmitEvent) {
		e.preventDefault();
		try {
			space = await client.put<{ used: number; limit: number }>('/api/admin/recordings/limit', { limit: Math.round(Number(limitGB) * 2 ** 30) });
			toast(tr("Limit saved"));
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("Could not save"), 'error');
		}
	}
	async function go(p: number) {
		page = p;
		await load();
	}
	const pct = $derived(space && space.limit ? Math.min(100, Math.round((space.used / space.limit) * 100)) : 0);
</script>

{#if enabled === false}
	<p class="alert alert-soft alert-info">{tr("Tutoring isn’t switched on, so there are no recordings.")}</p>
{:else if space}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3">
		<h2 class="m-0 text-lg">{tr("Tutoring recordings")}</h2>
		<p class="m-0">{tr("{0} of {1} used ({2}%).", [fmtBytes(space.used), fmtBytes(space.limit), pct])}{#if pct >= 80}{' '}<span class="text-warning font-semibold">{tr("Nearly full: new recordings stop when it is.")}</span>{/if}</p>
		<progress class="progress w-full" class:progress-warning={pct >= 80} value={pct} max="100" aria-label={tr("Space used by recordings")}></progress>
		<form class="row gap-2 flex-wrap" onsubmit={saveLimit}>
			<label class="small flex items-center gap-2">{tr("Limit")} <input class="input input-sm w-24" type="number" min="0.1" step="0.1" bind:value={limitGB} /> {tr("GB")}</label>
			<button class="btn btn-sm">{tr("Save")}</button>
		</form>
		<p class="small muted m-0">{tr("Recordings are deleted 2 days after they end. Only the presenter’s screen and voice are recorded.")}</p>
	</div>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-2">
		<RecordingsList {client} recordings={list} {meId} leadOf={() => true} showSession onchange={load} />
		{#if total > size}
			<div class="row gap-2">
				<button class="btn btn-sm" disabled={page === 1} onclick={() => go(page - 1)}>{tr("Previous")}</button>
				<span class="small tabular">{tr("Page {0} of {1}", [page, Math.ceil(total / size)])}</span>
				<button class="btn btn-sm" disabled={page * size >= total} onclick={() => go(page + 1)}>{tr("Next")}</button>
			</div>
		{/if}
	</div>
{:else}
	<p class="muted">{tr("Loading…")}</p>
{/if}
