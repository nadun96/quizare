<script lang="ts">
	// Breakout groups for teachers (D-79, TS-FR-50 to TS-FR-54): make them,
	// open and close them, visit one, answer calls for help, move students,
	// speak to all groups, and decide what students may do in groups.
	// Making, opening, closing and the settings are the lead teacher's.
	import { onMount } from 'svelte';
	import { ApiError } from '../api';
	import { confirmDialog, promptDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import { count } from '../words';
	import type { TutorClient } from './client';
	import { secondsLeft, type Group, type TutoringView } from './types';

	let { client, view }: { client: TutorClient; view: TutoringView } = $props();
	const path = $derived('/api/sessions/' + view.session.id);
	const lead = $derived(view.me.role === 'teacher');
	const s = $derived(view.session);
	const live = $derived(s.status === 'live');
	const names = $derived(new Map((view.participants ?? []).map((p) => [p.user_id, p.name])));
	const students = $derived((view.participants ?? []).filter((p) => p.role === 'student' && p.state === 'admitted'));
	const placed = $derived(new Set(view.groups.flatMap((g) => g.members)));
	const unplaced = $derived(students.filter((p) => !placed.has(p.user_id)));
	const asking = $derived(view.groups.filter((g) => g.help_at));

	let mode = $state<'random' | 'size' | 'categories' | 'choose' | 'empty'>('random');
	let n = $state(4);
	let announce = $state('');
	let now = $state(Date.now());
	onMount(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});
	const left = $derived(secondsLeft(s.groups_close_at, now));

	async function act(f: () => Promise<unknown>, ok?: string) {
		try {
			await f();
			if (ok) toast(ok);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	async function form(e: SubmitEvent) {
		e.preventDefault();
		if (view.groups.length && !(await confirmDialog({ title: 'Make new groups?', body: 'The groups you have now are replaced, with their chat.', confirm: 'Make new groups' }))) return;
		const body =
			mode === 'size' ? { mode: 'random', size: n }
			: mode === 'empty' ? { mode: 'manual', groups: Array.from({ length: n }, (_, i) => ({ name: `Group ${i + 1}`, user_ids: [] })) }
			: mode === 'categories' ? { mode }
			: { mode, count: n };
		await act(() => client.put(path + '/groups', body), 'Groups made');
	}
	const open = () => act(() => client.post(path + '/groups/open'), 'Groups open: students are in their groups');
	const close = (countdown: number) => act(() => client.post(path + '/groups/close', { countdown }), countdown ? `Groups close in ${countdown} s` : 'Everyone is back');
	const visit = (g: Group | null) => act(() => client.put(path + '/visit', { group_id: g?.id ?? 0 }));
	const dismiss = (g: Group) => act(() => client.del(`${path}/groups/${g.id}/help`));
	const move = (userId: string, groupId: number) => act(() => client.put(`${path}/participants/${userId}/group`, { group_id: groupId }));
	const set = (k: string, v: boolean) => act(() => client.put(path + '/groups/settings', { [k]: v }));
	async function rename(g: Group) {
		const name = await promptDialog({ title: 'Rename the group', confirm: 'Rename', value: g.name, label: 'Name', maxlength: 60 });
		if (name?.trim()) await act(() => client.patch(`${path}/groups/${g.id}`, { name }));
	}
	async function speak(e: SubmitEvent) {
		e.preventDefault();
		if (!announce.trim()) return;
		await act(() => client.post(path + '/groups/announce', { text: announce }), 'Sent to every group');
		announce = '';
	}
</script>

<section class="vstack gap-3" aria-label="Breakout groups">
	{#if asking.length}
		<div class="alert alert-soft alert-warning py-2" role="status">
			<span>Asking for help: {asking.map((g) => g.name).join(', ')}</span>
		</div>
	{/if}

	{#if s.groups_open}
		<div class="row gap-2 flex-wrap">
			<span class="badge badge-soft ok">Groups open</span>
			{#if left !== null}<span class="badge badge-soft warn tabular" role="timer">Closing in {left} s</span>{/if}
			{#if view.me.visiting}<button class="btn btn-xs" onclick={() => visit(null)}>Leave the group</button>{/if}
		</div>
		{#if lead}
			<div class="row gap-2 flex-wrap">
				<button class="btn btn-sm btn-primary" onclick={() => close(0)}>Bring everyone back</button>
				<button class="btn btn-sm" onclick={() => close(60)}>In 1 minute</button>
				<button class="btn btn-sm" onclick={() => close(30)}>In 30 s</button>
			</div>
		{/if}
		<form class="flex gap-2" onsubmit={speak}>
			<label class="sr-only" for="announce">Message to every group</label>
			<input id="announce" class="input input-sm w-full" bind:value={announce} maxlength="1000" placeholder="Message to every group…" />
			<button class="btn btn-sm" disabled={!announce.trim()}>Send</button>
		</form>
		<p class="small muted m-0">Students still hear you in the main room: turn your microphone on there to speak to all groups.</p>
	{:else if lead}
		<form class="vstack gap-2" onsubmit={form}>
			<label class="small" for="g-mode">Make groups</label>
			<select id="g-mode" class="select select-sm" bind:value={mode}>
				<option value="random">At random: number of groups</option>
				<option value="size">At random: students per group</option>
				<option value="categories">From the classroom’s categories</option>
				<option value="choose">Students choose: number of groups</option>
				<option value="empty">Empty groups: I place students</option>
			</select>
			{#if mode !== 'categories'}
				<label class="small flex items-center gap-2">{mode === 'size' ? 'Students per group' : 'Groups'}
					<input class="input input-sm w-20" type="number" min="1" max="50" bind:value={n} /></label>
			{/if}
			<div class="row gap-2"><button class="btn btn-sm">{view.groups.length ? 'Make new groups' : 'Make groups'}</button>
				{#if view.groups.length}<button type="button" class="btn btn-sm btn-primary" disabled={!live} onclick={open} title={live ? '' : 'Start the session first'}>Open the groups</button>{/if}</div>
		</form>
	{/if}

	{#each view.groups as g (g.id)}
		<div class="group" class:help={!!g.help_at} class:here={view.me.visiting === g.id}>
			<div class="row gap-2">
				<strong class="flex-1 truncate">{g.name}</strong>
				<span class="small muted">{count(g.size, 'student')}</span>
				{#if g.help_at}<span class="badge badge-warning badge-sm">Help</span>{/if}
			</div>
			<ul class="members">
				{#each g.members as u (u)}
					<li><span class="truncate">{names.get(u) ?? 'A student'}</span>
						<select class="select select-xs w-auto" aria-label="Move {names.get(u)}" value={String(g.id)} onchange={(e) => move(u, Number(e.currentTarget.value))}>
							{#each view.groups as o (o.id)}<option value={String(o.id)}>{o.name}</option>{/each}
							<option value="0">No group</option>
						</select></li>
				{:else}<li class="small muted">Nobody yet.</li>{/each}
			</ul>
			<div class="row gap-1 flex-wrap">
				{#if s.groups_open && view.me.visiting !== g.id}<button class="btn btn-xs btn-primary" onclick={() => visit(g)}>Visit</button>{/if}
				{#if g.help_at}<button class="btn btn-xs" onclick={() => dismiss(g)}>Dismiss help</button>{/if}
				<button class="btn btn-xs btn-ghost" onclick={() => rename(g)}>Rename</button>
			</div>
		</div>
	{/each}

	{#if view.groups.length && unplaced.length}
		<div class="group">
			<strong>Not in a group</strong>
			<ul class="members">
				{#each unplaced as p (p.user_id)}
					<li><span class="truncate">{p.name}</span>
						<select class="select select-xs w-auto" aria-label="Place {p.name}" value="0" onchange={(e) => move(p.user_id, Number(e.currentTarget.value))}>
							<option value="0">Choose a group</option>
							{#each view.groups as o (o.id)}<option value={String(o.id)}>{o.name}</option>{/each}
						</select></li>
				{/each}
			</ul>
		</div>
	{/if}

	{#if lead && view.groups.length}
		<details>
			<summary class="small font-semibold">In groups, students may…</summary>
			<div class="vstack gap-1 mt-2">
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={s.group_chat} onchange={(e) => set('group_chat', e.currentTarget.checked)} /> Chat in their group</label>
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={s.group_mic} onchange={(e) => set('group_mic', e.currentTarget.checked)} /> Use their microphone</label>
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={s.group_camera} onchange={(e) => set('group_camera', e.currentTarget.checked)} /> Use their camera</label>
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={s.group_screen} onchange={(e) => set('group_screen', e.currentTarget.checked)} /> Share their screen</label>
				<label class="row font-normal small"><input type="checkbox" class="toggle toggle-sm" checked={s.groups_choose} onchange={(e) => set('groups_choose', e.currentTarget.checked)} /> Choose their own group</label>
				<p class="small muted m-0">On top of what each student may do in the main room. Only their group sees and hears them, and the teachers can read every group’s chat.</p>
			</div>
		</details>
	{/if}
</section>

<style>
	.group { display: grid; gap: 0.35rem; padding: 0.6rem; border: 1px solid var(--color-base-300); border-radius: 0.6rem; }
	.group.help { border-color: var(--color-warning); }
	.group.here { background: color-mix(in oklab, var(--color-primary) 8%, var(--color-base-100)); }
	.members { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.2rem; }
	.members li { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; font-size: 0.875rem; }
</style>
