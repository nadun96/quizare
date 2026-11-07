<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// The teacher's people panel: the waiting room (TS-FR-03), raised hands
	// (TS-FR-24), what each student may share (TS-FR-21) and muting
	// (TS-FR-23). The server applies every change; this only asks.
	import { ApiError } from '../api';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import Icon from '../ui/Icon.svelte';
	import type { TutorClient } from './client';
	import { broadcasterName, handQueue, type TutoringParticipant, type TutoringView } from './types';

	let { client, view }: { client: TutorClient; view: TutoringView } = $props();
	const path = $derived('/api/sessions/' + view.session.id);
	const people = $derived(view.participants ?? []);
	const waiting = $derived(people.filter((p) => p.state === 'waiting'));
	const hands = $derived(handQueue(people));
	const students = $derived(people.filter((p) => p.role === 'student' && p.state === 'admitted'));
	const online = $derived(students.filter((p) => p.online).length);
	// Choosing the broadcaster and co-teachers is the lead teacher's (TS-FR-71).
	const lead = $derived(view.me.role === 'teacher');
	const coteachers = $derived(people.filter((p) => p.role === 'coteacher' && p.state === 'admitted'));
	const someoneElse = $derived(!!view.session.broadcaster_id);
	let email = $state('');
	let emailError = $state('');
	async function addCoteacher(e: SubmitEvent) {
		e.preventDefault();
		emailError = '';
		try {
			await client.post(path + '/coteachers', { email });
			toast(tr("Co-teacher added: send them the session link"));
			email = '';
		} catch (err) {
			emailError = err instanceof ApiError ? (err.fields.email ?? err.message) : 'That didn’t work';
		}
	}
	const broadcast = (p: TutoringParticipant | null) =>
		act(() => client.put(path + '/broadcaster', { user_id: p?.user_id ?? '' }), p ? `Asked ${p.name} to broadcast` : 'You broadcast again');
	const offered = (p: TutoringParticipant) => view.session.broadcast_offer === p.user_id;
	const live = (p: TutoringParticipant) => view.session.broadcaster_id === p.user_id;

	async function act(f: () => Promise<unknown>, ok?: string) {
		try {
			await f();
			if (ok) toast(ok);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		}
	}
	const admit = (ids: string[], yes: boolean) => act(() => client.post(path + '/admit', { user_ids: ids, admit: yes }));
	const allow = (ids: string[] | 'all', what: { mic?: boolean; camera?: boolean; screen?: boolean }, ask = false, lower = false) =>
		act(() => client.put(path + '/permissions', { ...(ids === 'all' ? { all: true } : { user_ids: ids }), ...what, ask, lower_hand: lower }));
	const mute = (target: string | 'all', what: { mic?: boolean; camera?: boolean; screen?: boolean }) =>
		act(() => client.post(path + '/mute', { ...(target === 'all' ? { all: true } : { user_id: target }), ...what }), 'Muted');
	async function remove(p: TutoringParticipant) {
		if (!(await confirmDialog({ title: tr("Remove {0}?", [p.name]), body: tr("They leave the session and can’t rejoin it."), confirm: tr("Remove"), danger: true }))) return;
		act(() => client.del(path + '/participants/' + p.user_id));
	}
	const perms = [
		{ key: 'mic', field: 'allow_mic', label: 'Microphone', icon: 'mic', off: 'mic-off' },
		{ key: 'camera', field: 'allow_camera', label: 'Camera', icon: 'video', off: 'video-off' },
		{ key: 'screen', field: 'allow_screen', label: 'Screen', icon: 'monitor', off: 'monitor' }
	] as const;
	const wait = (s?: string) => (s ? Math.max(0, Math.round((Date.now() - Date.parse(s)) / 1000)) : 0);
	const fmtWait = (s: number) => (s < 60 ? `${s} s` : `${Math.floor(s / 60)} min`);
	let tick = $state(0);
	$effect(() => {
		const t = setInterval(() => tick++, 5000);
		return () => clearInterval(t);
	});
</script>

<section class="vstack" aria-label={tr("People")}>
	{#if someoneElse}
		<div class="alert alert-soft alert-info small py-2">
			<span><strong>{broadcasterName(view)}</strong> {tr("is broadcasting. Your microphone still reaches everyone.")}</span>
			{#if lead}<button class="btn btn-xs btn-primary" onclick={() => broadcast(null)}>{tr("Take the broadcast back")}</button>{/if}
		</div>
	{/if}
	{#if waiting.length}
		<div class="vstack gap-1">
			<div class="row"><h3 class="m-0 text-sm">{tr("Waiting ({0})", [waiting.length])}</h3><span class="spacer"></span><button class="btn btn-xs btn-primary" onclick={() => admit(waiting.map((p) => p.user_id), true)}>{tr("Admit all")}</button></div>
			<ul class="list">
				{#each waiting as p (p.user_id)}
					<li><span class="truncate">{p.name}</span><span class="spacer"></span><button class="btn btn-xs" onclick={() => admit([p.user_id], true)}>{tr("Admit")}</button><button class="btn btn-xs btn-ghost" onclick={() => admit([p.user_id], false)}>{tr("Refuse")}</button></li>
				{/each}
			</ul>
		</div>
	{/if}

	{#if hands.length}
		<div class="vstack gap-1">
			<h3 class="m-0 text-sm">{tr("Hands raised ({0})", [hands.length])}</h3>
			<ol class="list">
				{#each hands as p (p.user_id)}
					<li>
						<span class="truncate">✋ {p.name}</span>
						<span class="small muted tabular">{tick >= 0 ? fmtWait(wait(p.hand_at)) : ''}</span>
						<span class="spacer"></span>
						<button class="btn btn-xs btn-primary" onclick={() => allow([p.user_id], { mic: true }, true, true)}>{tr("Allow mic")}</button>
						<button class="btn btn-xs btn-ghost" onclick={() => act(() => client.del(path + '/participants/' + p.user_id + '/hand'))}>{tr("Lower")}</button>
					</li>
				{/each}
			</ol>
		</div>
	{/if}

	<div class="vstack gap-1">
		<h3 class="m-0 text-sm">{tr("Co-teachers")}</h3>
		<ul class="list">
			{#each coteachers as c (c.user_id)}
				<li>
					<span class="dot" class:on={c.online}></span><span class="truncate name">{c.name}</span>{#if live(c)}{' '}<span class="badge badge-soft badge-xs">{tr("broadcasting")}</span>{/if}
					<span class="spacer"></span>
					{#if lead}
						{#if live(c)}<button class="btn btn-xs" onclick={() => broadcast(null)}>{tr("Take back")}</button>
						{:else}<button class="btn btn-xs" onclick={() => broadcast(c)} disabled={!c.online}>{offered(c) ? tr("Asked…") : tr("Make broadcaster")}</button>{/if}
						<button class="btn btn-xs btn-ghost" onclick={() => act(() => client.del(path + '/coteachers/' + c.user_id))}>{tr("Remove")}</button>
					{/if}
				</li>
			{:else}
				<li class="small muted">{tr("None yet.")}</li>
			{/each}
		</ul>
		{#if lead}
			<form class="flex gap-2" onsubmit={addCoteacher}>
				<label class="sr-only" for="co-email">{tr("Co-teacher's email")}</label>
				<input id="co-email" class="input input-sm w-full" type="email" placeholder={tr("Another teacher's email")} bind:value={email} required aria-invalid={!!emailError} />
				<button class="btn btn-sm">{tr("Add")}</button>
			</form>
			{#if emailError}<p class="field-error m-0">{emailError}</p>{/if}
			<p class="small muted m-0">{tr("Co-teachers admit, allow, mute and moderate. Only you choose the broadcaster, remove students and end the session.")}</p>
		{/if}
	</div>

	<div class="vstack gap-1">
		<div class="row"><h3 class="m-0 text-sm">{tr("Students ({0} here of {1})", [online, students.length])}</h3></div>
		<div class="row small gap-1">
			<span class="muted">{tr("Everyone:")}</span>
			<button class="btn btn-xs" onclick={() => allow('all', { mic: true })}>{tr("Allow mics")}</button>
			<button class="btn btn-xs" onclick={() => allow('all', { mic: false, camera: false, screen: false })}>{tr("Forbid all")}</button>
			<button class="btn btn-xs btn-ghost" onclick={() => mute('all', { mic: true })}>{tr("Mute everyone")}</button>
		</div>
		<ul class="list people">
			{#each students as p (p.user_id)}
				<li>
					<span class="dot" class:on={p.online} title={p.online ? tr("Here") : tr("Not connected")} aria-label={p.online ? tr("Here") : tr("Not connected")}></span>
					<span class="truncate name">{p.name}{#if live(p)}{' '}<span class="badge badge-soft badge-xs">{tr("broadcasting")}</span>{/if}{#if p.chat_muted}{' '}<span class="badge badge-soft badge-xs">{tr("chat muted")}</span>{/if}</span>
					<!-- What they may share, as three switches that fit beside the name (D-116). -->
					{#each perms as k (k.key)}
						{@const on = p[k.field]}
						<button type="button" class="perm" class:on aria-pressed={on} title="{k.label}: {on ? 'allowed' : 'not allowed'}" aria-label={tr("Allow {0}’s {1}", [p.name, k.label.toLowerCase()])} onclick={() => allow([p.user_id], { [k.key]: !on })}><Icon name={on ? k.icon : k.off} size={15} /></button>
					{/each}
					<details class="dropdown dropdown-end">
						<summary class="btn btn-xs btn-ghost" aria-label={tr("More for {0}", [p.name])}>⋯</summary>
						<ul class="menu dropdown-content z-40 w-48 rounded-box border border-base-300 bg-base-100 p-1 shadow">
							{#if lead}
								{#if live(p)}<li><button onclick={() => broadcast(null)}>{tr("Take the broadcast back")}</button></li>
								{:else}<li><button onclick={() => broadcast(p)} disabled={!p.online}>{offered(p) ? tr("Asked to broadcast…") : tr("Make broadcaster")}</button></li>{/if}
							{/if}
							<li><button onclick={() => mute(p.user_id, { mic: true, camera: true, screen: true })}>{tr("Mute and stop sharing")}</button></li>
							<li><button onclick={() => allow([p.user_id], { mic: p.allow_mic, camera: p.allow_camera, screen: p.allow_screen }, true)}>{tr("Ask to turn on")}</button></li>
							<li><button onclick={() => act(() => client.put(path + '/participants/' + p.user_id + '/chat-muted', { muted: !p.chat_muted }))}>{p.chat_muted ? tr("Let write in chat") : tr("Mute in chat")}</button></li>
							{#if lead}<li><button class="text-error" onclick={() => remove(p)}>{tr("Remove from session")}</button></li>{/if}
						</ul>
					</details>
				</li>
			{:else}
				<li class="muted small">{tr("No students yet. Share the code or link.")}</li>
			{/each}
		</ul>
	</div>
</section>

<style>
	.list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
	.list li { display: flex; align-items: center; gap: 0.4rem; min-height: 2rem; }
	.name { min-width: 0; flex: 1 1 auto; }
	.perm { display: grid; place-items: center; width: 1.75rem; height: 1.75rem; flex: none; border: 1px solid var(--color-base-300); color: var(--color-muted); opacity: 0.7; }
	.perm.on { border-color: var(--color-success); background: color-mix(in oklab, var(--color-success) 18%, transparent); color: var(--color-base-content); opacity: 1; }
	.perm:focus-visible { outline: 2px solid var(--color-primary); }
	.dot { width: 0.55rem; height: 0.55rem; border-radius: 50%; background: var(--color-base-300); flex: none; }
	.dot.on { background: var(--color-success); }
</style>
