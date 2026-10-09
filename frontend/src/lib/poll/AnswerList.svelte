<script lang="ts">
	// Every answer to one question, for moderation, one page at a time from
	// the server (PL-FR-02). Live results show the newest 300 text answers;
	// this reaches all of them, newest first, with search and a hidden/shown
	// filter. Loads only when opened.
	import { Paged } from '../paged.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';

	type Row = { participant_id: string; name: string; real_name?: string; text: string; hidden: boolean; at: string };
	let {
		pollId,
		questionId,
		onmoderate
	}: { pollId: string; questionId: string; onmoderate: (m: { question_id: string; participant_id: string; hidden: boolean }) => Promise<void> } = $props();
	let hidden = $state('');
	const list = new Paged<Row>(() => '/api/teacher/polls/' + pollId + '/questions/' + questionId + '/answers', 'answers', {
		sort: 'time', desc: true, filters: () => ({ hidden })
	});
	let opened = $state(false);
	function toggle(e: Event) {
		opened = (e.currentTarget as HTMLDetailsElement).open;
		if (opened && !list.loaded) list.load();
	}
	async function setHidden(r: Row, h: boolean) {
		await onmoderate({ question_id: questionId, participant_id: r.participant_id, hidden: h });
		list.load();
	}
</script>

<details class="answers mt-3" ontoggle={toggle}>
	<summary class="small font-semibold">All answers, including hidden ones</summary>
	{#if opened}
		<div class="vstack mt-2">
			<div class="flex flex-wrap items-center gap-2">
				<ListSearch {list} placeholder="Search answers or names" label="Search answers" />
				<select class="select select-sm w-auto" bind:value={hidden} onchange={() => list.refilter()} aria-label="Show">
					<option value="">All</option><option value="false">Shown</option><option value="true">Hidden</option>
				</select>
			</div>
			<ul class="rows">
				{#each list.rows as r (r.participant_id)}
					<li class:off={r.hidden}>
						<span class="min-w-0 flex-1"><span class="small muted">{r.name}{r.real_name ? ` · ${r.real_name}` : ''} · {new Date(r.at).toLocaleTimeString()}</span><br /><span class="whitespace-pre-wrap">{r.text}</span></span>
						{#if r.hidden}<button class="btn btn-ghost btn-xs" onclick={() => setHidden(r, false)}>Show</button>
						{:else}<button class="btn btn-ghost btn-xs" onclick={() => setHidden(r, true)}>Hide</button>{/if}
					</li>
				{:else}
					<li class="muted small">{list.loaded ? 'No answers match.' : 'Loading…'}</li>
				{/each}
			</ul>
			<Pager {list} label="Answers" />
		</div>
	{/if}
</details>

<style>
	.answers summary { cursor: pointer; }
	.rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.3rem; }
	.rows li { display: flex; align-items: flex-start; gap: 0.5rem; padding: 0.45rem 0.6rem; border: 1px solid var(--color-base-300); border-radius: var(--radius-box); }
	.rows li.off { opacity: 0.6; }
</style>
