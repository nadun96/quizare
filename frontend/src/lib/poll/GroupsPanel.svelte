<script lang="ts">
	// The teacher's groups for a poll (V2-06, D-43): make them at random or
	// from classroom categories, move people between them, pick captains.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { flyIn } from '../ui/motion';
	import { toast } from '../ui/toast.svelte';
	import type { GroupMember, GroupsView, PollSettings } from './types';

	let { pollId, settings, hasClassroom = false, participants = 0 }: { pollId: string; settings: PollSettings; hasClassroom?: boolean; participants?: number } = $props();

	let view = $state<GroupsView | null>(null);
	let count = $state(4);
	let reassign = $state(false);
	let busy = $state(false);
	let selected = $state<Set<string>>(new Set());

	async function load() {
		try {
			view = await api.get<GroupsView>('/api/teacher/polls/' + pollId + '/groups');
		} catch {
			toast('Could not load the groups', 'error');
		}
	}
	// Reload when people join or leave.
	$effect(() => {
		void participants;
		load();
	});

	async function run(f: () => Promise<unknown>, ok?: string) {
		busy = true;
		try {
			await f();
			if (ok) toast(ok);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : 'Something went wrong', 'error');
		} finally {
			busy = false;
		}
	}
	const generate = (from: 'random' | 'categories') =>
		run(() => api.post('/api/teacher/polls/' + pollId + '/groups/generate', { from, count: Number(count), reassign }), from === 'random' ? 'Groups shuffled' : 'Groups made from categories');
	const add = () => run(() => api.post('/api/teacher/polls/' + pollId + '/groups', {}), 'Group added');
	const move = (ids: string[], group: string) => run(async () => {
		await api.post('/api/teacher/polls/' + pollId + '/groups/members', { participant_ids: ids, group_id: group });
		selected = new Set();
	});
	const makeCaptain = (m: GroupMember) => run(() => api.post('/api/teacher/polls/' + pollId + '/groups/members', { participant_ids: [m.participant_id], captain: !m.captain }));
	async function rename(id: string, name: string) {
		const n = prompt('Group name', name);
		if (n?.trim() && n !== name) await run(() => api.patch('/api/teacher/poll-groups/' + id, { name: n }));
	}
	const recolour = (id: string, color: number) => run(() => api.patch('/api/teacher/poll-groups/' + id, { color }));
	async function remove(id: string, name: string) {
		if (await confirmDialog({ title: `Delete ${name}?`, body: 'Its members stay in the poll without a group. Their answers are kept.', confirm: 'Delete group', danger: true }))
			await run(() => api.del('/api/teacher/poll-groups/' + id), 'Group deleted');
	}
	function toggle(id: string, on: boolean) {
		const s = new Set(selected);
		if (on) s.add(id);
		else s.delete(id);
		selected = s;
	}
	const ACCEPT = { all: "every member's answer counts", first: "the group's first answer counts", captain: 'only captains answer', best: "the best member's answer counts" };
</script>

{#snippet member(m: GroupMember, groupId: string)}
	<li class="member">
		<input type="checkbox" class="checkbox checkbox-xs" aria-label="Select {m.name}" checked={selected.has(m.participant_id)} onchange={(e) => toggle(m.participant_id, e.currentTarget.checked)} />
		<span class="min-w-0 flex-1">
			<span class="block truncate">{m.name}</span>
			{#if m.real_name && m.real_name !== m.name}<span class="small muted block truncate">{m.real_name}</span>{/if}
		</span>
		{#if groupId}
			<button type="button" class="btn btn-ghost btn-xs btn-square" class:text-warning={m.captain} aria-pressed={m.captain} aria-label={m.captain ? `${m.name} is captain` : `Make ${m.name} captain`} title={m.captain ? 'Captain' : 'Make captain'} disabled={busy} onclick={() => makeCaptain(m)}>
				<Icon name="star" size={14} />
			</button>
		{/if}
		<select class="select select-xs w-28" aria-label="Move {m.name} to" value={groupId} disabled={busy} onchange={(e) => move([m.participant_id], e.currentTarget.value)}>
			<option value="">No group</option>
			{#each view?.groups ?? [] as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
		</select>
	</li>
{/snippet}

<div class="vstack">
	<div class="card card-border bg-base-100 p-4 shadow-sm">
		<div class="flex flex-wrap items-end gap-2">
			<div class="w-24"><label for="gp-n">Groups</label><input id="gp-n" class="input input-sm w-full" type="number" min="2" max="50" bind:value={count} /></div>
			<button class="btn btn-sm btn-primary" disabled={busy} onclick={() => generate('random')}><Icon name="shuffle" size={14} />Make random groups</button>
			{#if hasClassroom && settings.identity !== 'anonymous'}<button class="btn btn-sm" disabled={busy} onclick={() => generate('categories')}><Icon name="users" size={14} />From categories</button>{/if}
			<button class="btn btn-sm" disabled={busy} onclick={add}><Icon name="plus" size={14} />Add a group</button>
			<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={reassign} />Also move people who have a group</label>
		</div>
		<p class="small muted m-0 mt-2">Groups {settings.groups === 'self' ? 'are chosen by participants when they join' : settings.groups === 'random' ? 'fill up at random as people join' : settings.groups === 'categories' ? "follow students' classroom categories" : 'are up to you'}; {ACCEPT[settings.group_acceptance]}.</p>
		{#if selected.size}
			<div class="mt-3 flex flex-wrap items-center gap-2" in:flyIn>
				<span class="small font-semibold">{selected.size} selected</span>
				<select class="select select-sm w-40" aria-label="Move selected to" disabled={busy} onchange={(e) => e.currentTarget.value !== '-' && move([...selected], e.currentTarget.value)}>
					<option value="-">Move to…</option>
					<option value="">No group</option>
					{#each view?.groups ?? [] as g (g.id)}<option value={g.id}>{g.name}</option>{/each}
				</select>
				<button class="btn btn-ghost btn-sm" onclick={() => (selected = new Set())}>Clear</button>
			</div>
		{/if}
	</div>

	{#if !view}
		<span class="loading loading-dots"></span>
	{:else}
		<div class="groups">
			{#each view.groups as g (g.id)}
				<section class="card card-border bg-base-100 p-3 shadow-sm" aria-labelledby="g-{g.id}" in:flyIn>
					<div class="flex items-center gap-2">
						<span class="dot" style:background="var(--cat-{g.color})" aria-hidden="true"></span>
						<h3 id="g-{g.id}" class="m-0 min-w-0 flex-1 truncate text-base">{g.name}</h3>
						<span class="badge badge-soft badge-sm tabular">{g.members?.length ?? 0}</span>
						<div class="dropdown dropdown-end">
							<button type="button" class="btn btn-ghost btn-xs btn-square" aria-label="Options for {g.name}"><Icon name="pencil" size={14} /></button>
							<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
							<div tabindex="0" class="dropdown-content z-10 w-52 rounded-box bg-base-100 p-2 shadow-lg">
								<button class="btn btn-ghost btn-sm w-full justify-start" onclick={() => rename(g.id, g.name)}>Rename</button>
								<p class="small muted m-0 px-3 pt-1">Colour</p>
								<div class="flex flex-wrap gap-1 px-2 py-1">
									{#each [1, 2, 3, 4, 5, 6, 7, 8] as c (c)}
										<button type="button" class="swatch" class:on={g.color === c} style:background="var(--cat-{c})" aria-label="Colour {c}" aria-pressed={g.color === c} onclick={() => recolour(g.id, c)}></button>
									{/each}
								</div>
								<button class="btn btn-ghost btn-sm w-full justify-start text-error" onclick={() => remove(g.id, g.name)}>Delete group</button>
							</div>
						</div>
					</div>
					<ul class="members">
						{#each g.members ?? [] as m (m.participant_id)}{@render member(m, g.id)}{:else}<li class="small muted py-1">Nobody yet</li>{/each}
					</ul>
				</section>
			{/each}
		</div>
		{#if !view.groups.length}<p class="muted m-0">No groups yet. Make random groups or add one.</p>{/if}
		{#if view.ungrouped.length}
			<section class="card card-border bg-base-100 p-3 shadow-sm" aria-labelledby="g-none">
				<h3 id="g-none" class="m-0 text-base">Not in a group <span class="badge badge-soft badge-sm tabular">{view.ungrouped.length}</span></h3>
				<p class="small muted m-0">They answer for themselves only.</p>
				<ul class="members">{#each view.ungrouped as m (m.participant_id)}{@render member(m, '')}{/each}</ul>
			</section>
		{/if}
	{/if}
</div>

<style>
	.groups { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(min(17rem, 100%), 1fr)); }
	.dot { width: 0.85rem; height: 0.85rem; border-radius: 999px; flex: none; }
	.members { list-style: none; margin: 0.5rem 0 0; padding: 0; display: grid; gap: 0.15rem; }
	.member { display: flex; align-items: center; gap: 0.5rem; padding: 0.25rem 0; border-top: 1px solid var(--color-base-200); }
	.swatch { width: 1.5rem; height: 1.5rem; border-radius: 999px; border: 2px solid transparent; cursor: pointer; }
	.swatch.on { border-color: var(--color-base-content); }
</style>
