<script lang="ts">
	// Recordings someone may watch (D-80): a student's, of sessions they were
	// in whose teacher lets students watch; a teacher's, of sessions they
	// taught. Shown only when tutoring is on and there are some.
	import { TutorClient, tutoringConfig } from './client';
	import RecordingsList from './RecordingsList.svelte';
	import type { Recording } from './types';

	let { meId, heading = 'Recordings of tutoring sessions' }: { meId: string; heading?: string } = $props();
	const client = new TutorClient();
	let list = $state<Recording[]>([]);
	async function load() {
		if (!(await tutoringConfig()).enabled) return;
		list = (await client.get<{ recordings: Recording[] }>('/api/my/recordings')).recordings;
	}
	$effect(() => {
		load().catch(() => {});
	});
</script>

{#if list.length}
	<section class="vstack gap-2" aria-labelledby="my-recs-h">
		<h2 class="m-0" id="my-recs-h">{heading}</h2>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
			<RecordingsList {client} recordings={list} {meId} leadOf={(r) => r.lead} showSession onchange={load} />
			<p class="small muted m-0 mt-2">Each recording is deleted 2 days after it ends.</p>
		</div>
	</section>
{/if}
