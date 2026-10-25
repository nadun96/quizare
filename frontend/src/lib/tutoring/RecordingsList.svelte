<script lang="ts">
	// Recordings of tutoring sessions (D-80, TS-FR-78, TS-FR-79): watch them
	// in the browser; the lead teacher downloads, chooses who may watch and
	// deletes. Each is deleted 2 days after it ends; the list says when.
	import { ApiError } from '../api';
	import { fmtBytes } from '../staff';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import type { TutorClient } from './client';
	import { deletedIn, recordingLength, type Recording } from './types';

	let {
		client,
		recordings,
		meId,
		leadOf,
		showSession = false,
		onchange
	}: {
		client: TutorClient;
		recordings: Recording[];
		meId: string;
		/** Whether this person leads the recording's session (downloads, who watches, delete). */
		leadOf: (r: Recording) => boolean;
		showSession?: boolean;
		onchange?: () => void;
	} = $props();
	let playing = $state<{ r: Recording; url: string } | null>(null);

	async function link(r: Recording, download: boolean): Promise<string | null> {
		try {
			const { url } = await client.post<{ url: string }>(`/api/recordings/${r.id}/link`, { download });
			return client.base + url;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not open the recording', 'error');
			return null;
		}
	}
	async function watch(r: Recording) {
		const url = await link(r, false);
		if (url) playing = { r, url };
	}
	async function download(r: Recording) {
		const url = await link(r, true);
		if (url) location.assign(url);
	}
	async function viewers(r: Recording, v: Recording['viewers']) {
		await client.put(`/api/sessions/${r.session_id}/recordings/${r.id}/viewers`, { viewers: v }).catch(() => toast('Could not change it', 'error'));
		onchange?.();
	}
	async function remove(r: Recording) {
		if (!(await confirmDialog({ title: 'Delete this recording?', body: 'It’s gone for everyone, at once.', confirm: 'Delete', danger: true }))) return;
		await client.del(`/api/recordings/${r.id}`).catch(() => toast('Could not delete it', 'error'));
		onchange?.();
	}
	const when = (iso: string) => new Date(iso).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
</script>

<ul class="recs" aria-label="Recordings">
	{#each recordings as r (r.id)}
		<li>
			<div class="min-w-0 flex-1">
				<p class="m-0 font-semibold truncate">{showSession && r.session_title ? r.session_title : when(r.started_at)}</p>
				<p class="small muted m-0">
					{#if showSession}{when(r.started_at)} · {/if}{recordingLength(r)} · {fmtBytes(r.bytes)}{#if r.status !== 'stopped'}{' · '}<span class="text-error">recording now</span>{:else if r.expires_at}{' · deleted '}{deletedIn(r.expires_at)}{/if}
					{#if r.started_by !== meId}{' · '}by {r.started_by_name}{/if}
				</p>
			</div>
			{#if r.status === 'stopped'}<button class="btn btn-xs btn-primary" onclick={() => watch(r)}>Watch</button>{/if}
			{#if leadOf(r)}
				{#if r.status === 'stopped'}<button class="btn btn-xs" onclick={() => download(r)}>Download</button>{/if}
				<select class="select select-xs w-auto" aria-label="Who may watch" value={r.viewers} onchange={(e) => viewers(r, e.currentTarget.value as Recording['viewers'])}>
					<option value="teachers">Teachers only</option>
					<option value="students">Students too</option>
				</select>
				<button class="btn btn-xs btn-ghost" onclick={() => remove(r)}>Delete</button>
			{/if}
		</li>
	{:else}
		<li class="small muted">No recordings.</li>
	{/each}
</ul>

<dialog class="modal" open={!!playing} aria-label="Recording">
	{#if playing}
		<div class="modal-box max-w-4xl vstack">
			<p class="m-0 font-semibold">{playing.r.session_title ?? 'Recording'} · {when(playing.r.started_at)}</p>
			<!-- svelte-ignore a11y_media_has_caption -->
			<video src={playing.url} controls autoplay class="w-full rounded-lg bg-black"></video>
			<p class="small muted m-0">The presenter’s screen and voice. The link works for 5 minutes; open it again from the list after that.</p>
			<div class="modal-action"><button class="btn" onclick={() => (playing = null)}>Close</button></div>
		</div>
	{/if}
</dialog>

<style>
	.recs { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
	.recs li { display: flex; flex-wrap: wrap; align-items: center; gap: 0.4rem; padding: 0.45rem 0; border-top: 1px solid var(--color-base-300); }
	.recs li:first-child { border-top: 0; }
</style>
