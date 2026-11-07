<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// A poll's participants, one page at a time from the server (PL-FR-02):
	// search by name or group, sort by join order, name, answers or points,
	// and rename a participant.
	import { api } from '../api';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import { promptDialog } from '../ui/dialog.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import SortHeader from '../ui/SortHeader.svelte';
	import { toast } from '../ui/toast.svelte';
	import { fmtPoints } from './scoring';

	type Row = { participant_id: string; number: number; name: string; nickname?: string; real_name?: string; group?: string; answered: number; score: number; joined_at: string };
	let { pollId, scoring = false, groups = false }: { pollId: string; scoring?: boolean; groups?: boolean } = $props();
	const list = new Paged<Row>(() => '/api/teacher/polls/' + pollId + '/participants', 'participants', { url: urlState, prefix: 'pp', sort: 'joined' });
	$effect(() => {
		if (pollId) list.load();
	});

	async function rename(r: Row) {
		const n = await promptDialog({ title: tr("Rename participant"), body: tr("Shown on the leaderboard. Leave it empty for \"Participant N\"."), label: tr("Nickname"), value: r.nickname ?? '', allowEmpty: true, maxlength: 30 });
		if (n === null) return;
		await api.post('/api/teacher/polls/' + pollId + '/moderation', { participant_id: r.participant_id, nickname: n });
		toast(tr("Renamed"), 'info');
		list.load();
	}
</script>

<div class="vstack">
	<ListSearch {list} placeholder={tr("Search name{0}", [groups ? tr(" or group") : ''])} label={tr("Search participants")} />
	<div class="card card-border bg-base-100 p-0 shadow-sm table-wrap">
		<table class="table">
			<thead>
				<tr>
					<th><SortHeader {list} key="joined" label="#" /></th>
					<th><SortHeader {list} key="name" label={tr("Name")} /></th>
					{#if groups}<th>{tr("Group")}</th>{/if}
					<th><SortHeader {list} key="answered" label={tr("Answers")} desc /></th>
					{#if scoring}<th><SortHeader {list} key="score" label={tr("Points")} desc /></th>{/if}
					<th>{tr("Joined")}</th>
					<th><span class="sr-only">{tr("Actions")}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each list.rows as r (r.participant_id)}
					<tr>
						<td class="tabular muted">{r.number}</td>
						<td>{r.name}{#if r.real_name}<br /><span class="small muted">{r.real_name}</span>{/if}</td>
						{#if groups}<td class="small">{r.group || '—'}</td>{/if}
						<td class="tabular">{r.answered}</td>
						{#if scoring}<td class="tabular">{fmtPoints(r.score)}</td>{/if}
						<td class="small tabular">{new Date(r.joined_at).toLocaleTimeString(localeTag())}</td>
						<td><button class="btn btn-ghost btn-xs" onclick={() => rename(r)}>{tr("Rename")}</button></td>
					</tr>
				{:else}
					<tr><td colspan="7" class="muted">{!list.loaded ? tr("Loading…") : list.q ? tr("Nobody matches.") : tr("Nobody has joined yet.")}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
	<Pager {list} label={tr("Participants")} />
</div>
