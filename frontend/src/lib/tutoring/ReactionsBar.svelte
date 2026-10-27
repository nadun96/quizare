<script lang="ts">
	// Quick reactions (TS-FR-26): students tap one; teachers see how many
	// people sent each in the last minute. No permission, no media.
	import type { TutorClient } from './client';
	import { REACTIONS, describe, shown, type ReactionCounts, type ReactionKind } from './reactions';

	let { client, sessionId, teacher, counts = null }: { client: TutorClient; sessionId: string; teacher: boolean; counts?: ReactionCounts | null } = $props();
	let sent = $state<ReactionKind | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function react(kind: ReactionKind) {
		sent = kind;
		clearTimeout(timer);
		timer = setTimeout(() => (sent = null), 2000);
		try {
			await client.call('POST', `/api/sessions/${sessionId}/react`, { kind });
		} catch {
			sent = null;
		}
	}
	const list = $derived(shown(counts));
</script>

{#if teacher}
	{#if list.length}
		<div class="flex flex-wrap items-center gap-1" role="status" aria-label="Reactions in the last minute">
			{#each list as r (r.kind)}
				<span class="badge badge-soft gap-1" title={describe(r)} aria-label={describe(r)}><span aria-hidden="true">{r.emoji}</span>{r.n}</span>
			{/each}
		</div>
	{/if}
{:else}
	<div class="flex items-center gap-1" role="group" aria-label="Quick reactions">
		{#each REACTIONS as r (r.kind)}
			<button class="btn btn-sm btn-ghost px-2" class:btn-active={sent === r.kind} title={r.label} aria-label={r.label} onclick={() => react(r.kind)}>
				<span aria-hidden="true">{r.emoji}</span>
			</button>
		{/each}
		{#if sent}<span class="small muted" role="status">Sent</span>{/if}
	</div>
{/if}
