<script lang="ts">
	// The teacher's people panel: the waiting room (TS-FR-03), raised hands
	// (TS-FR-24), what each student may share (TS-FR-21) and muting
	// (TS-FR-23). The server applies every change; this only asks.
	import { ApiError } from '../api';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
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
			toast('Co-teacher added: send them the session link');
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
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	const admit = (ids: string[], yes: boolean) => act(() => client.post(path + '/admit', { user_ids: ids, admit: yes }));
	const allow = (ids: string[] | 'all', what: { mic?: boolean; camera?: boolean; screen?: boolean }, ask = false, lower = false) =>
		act(() => client.put(path + '/permissions', { ...(ids === 'all' ? { all: true } : { user_ids: ids }), ...what, ask, lower_hand: lower }));
	const mute = (target: string | 'all', what: { mic?: boolean; camera?: boolean; screen?: boolean }) =>
		act(() => client.post(path + '/mute', { ...(target === 'all' ? { all: true } : { user_id: target }), ...what }), 'Muted');
	async function remove(p: TutoringParticipant) {
		if (!(await confirmDialog({ title: `Remove ${p.name}?`, body: 'They leave the session and can’t rejoin it.', confirm: 'Remove', danger: true }))) return;
		act(() => client.del(path + '/participants/' + p.user_id));
	}
	const wait = (s?: string) => (s ? Math.max(0, Math.round((Date.now() - Date.parse(s)) / 1000)) : 0);
	const fmtWait = (s: number) => (s < 60 ? `${s} s` : `${Math.floor(s / 60)} min`);
	let tick = $state(0);
	$effect(() => {
		const t = setInterval(() => tick++, 5000);
		return () => clearInterval(t);
	});
</script>

<section class="vstack" aria-label="People">
	{#if someoneElse}
		<div class="alert alert-soft alert-info small py-2">
			<span><strong>{broadcasterName(view)}</strong> is broadcasting. Your microphone still reaches everyone.</span>
			{#if lead}<button class="btn btn-xs btn-primary" onclick={() => broadcast(null)}>Take the broadcast back</button>{/if}
		</div>
	{/if}
	{#if waiting.length}
		<div class="vstack gap-1">
			<div class="row"><h3 class="m-0 text-sm">Waiting ({waiting.length})</h3><span class="spacer"></span><button class="btn btn-xs btn-primary" onclick={() => admit(waiting.map((p) => p.user_id), true)}>Admit all</button></div>
			<ul class="list">
				{#each waiting as p (p.user_id)}
					<li><span class="truncate">{p.name}</span><span class="spacer"></span><button class="btn btn-xs" onclick={() => admit([p.user_id], true)}>Admit</button><button class="btn btn-xs btn-ghost" onclick={() => admit([p.user_id], false)}>Refuse</button></li>
				{/each}
			</ul>
		</div>
	{/if}

	{#if hands.length}
		<div class="vstack gap-1">
			<h3 class="m-0 text-sm">Hands raised ({hands.length})</h3>
			<ol class="list">
				{#each hands as p (p.user_id)}
					<li>
						<span class="truncate">✋ {p.name}</span>
						<span class="small muted tabular">{tick >= 0 ? fmtWait(wait(p.hand_at)) : ''}</span>
						<span class="spacer"></span>
						<button class="btn btn-xs btn-primary" onclick={() => allow([p.user_id], { mic: true }, true, true)}>Allow mic</button>
						<button class="btn btn-xs btn-ghost" onclick={() => act(() => client.del(path + '/participants/' + p.user_id + '/hand'))}>Lower</button>
					</li>
				{/each}
			</ol>
		</div>
	{/if}

	<div class="vstack gap-1">
		<h3 class="m-0 text-sm">Co-teachers</h3>
		<ul class="list">
			{#each coteachers as c (c.user_id)}
				<li>
					<span class="dot" class:on={c.online}></span><span class="truncate name">{c.name}</span>{#if live(c)} <span class="badge badge-soft badge-xs">broadcasting</span>{/if}
					<span class="spacer"></span>
					{#if lead}
						{#if live(c)}<button class="btn btn-xs" onclick={() => broadcast(null)}>Take back</button>
						{:else}<button class="btn btn-xs" onclick={() => broadcast(c)} disabled={!c.online}>{offered(c) ? 'Asked…' : 'Make broadcaster'}</button>{/if}
						<button class="btn btn-xs btn-ghost" onclick={() => act(() => client.del(path + '/coteachers/' + c.user_id))}>Remove</button>
					{/if}
				</li>
			{:else}
				<li class="small muted">None yet.</li>
			{/each}
		</ul>
		{#if lead}
			<form class="flex gap-2" onsubmit={addCoteacher}>
				<label class="sr-only" for="co-email">Co-teacher's email</label>
				<input id="co-email" class="input input-sm w-full" type="email" placeholder="Another teacher's email" bind:value={email} required aria-invalid={!!emailError} />
				<button class="btn btn-sm">Add</button>
			</form>
			{#if emailError}<p class="field-error m-0">{emailError}</p>{/if}
			<p class="small muted m-0">Co-teachers admit, allow, mute and moderate. Only you choose the broadcaster, remove students and end the session.</p>
		{/if}
	</div>

	<div class="vstack gap-1">
		<div class="row"><h3 class="m-0 text-sm">Students ({online} here of {students.length})</h3></div>
		<div class="row small gap-1">
			<span class="muted">Everyone:</span>
			<button class="btn btn-xs" onclick={() => allow('all', { mic: true })}>Allow mics</button>
			<button class="btn btn-xs" onclick={() => allow('all', { mic: false, camera: false, screen: false })}>Forbid all</button>
			<button class="btn btn-xs btn-ghost" onclick={() => mute('all', { mic: true })}>Mute everyone</button>
		</div>
		<ul class="list people">
			{#each students as p (p.user_id)}
				<li>
					<span class="dot" class:on={p.online} title={p.online ? 'Here' : 'Not connected'} aria-label={p.online ? 'Here' : 'Not connected'}></span>
					<span class="truncate name">{p.name}{#if live(p)} <span class="badge badge-soft badge-xs">broadcasting</span>{/if}{#if p.chat_muted} <span class="badge badge-soft badge-xs">chat muted</span>{/if}</span>
					<span class="spacer"></span>
					<label class="tog" title="Microphone"><input type="checkbox" class="toggle toggle-xs" checked={p.allow_mic} onchange={(e) => allow([p.user_id], { mic: e.currentTarget.checked })} /> Mic</label>
					<label class="tog" title="Camera"><input type="checkbox" class="toggle toggle-xs" checked={p.allow_camera} onchange={(e) => allow([p.user_id], { camera: e.currentTarget.checked })} /> Cam</label>
					<label class="tog" title="Screen"><input type="checkbox" class="toggle toggle-xs" checked={p.allow_screen} onchange={(e) => allow([p.user_id], { screen: e.currentTarget.checked })} /> Screen</label>
					<details class="dropdown dropdown-end">
						<summary class="btn btn-xs btn-ghost" aria-label="More for {p.name}">⋯</summary>
						<ul class="menu dropdown-content z-40 w-48 rounded-box border border-base-300 bg-base-100 p-1 shadow">
							{#if lead}
								{#if live(p)}<li><button onclick={() => broadcast(null)}>Take the broadcast back</button></li>
								{:else}<li><button onclick={() => broadcast(p)} disabled={!p.online}>{offered(p) ? 'Asked to broadcast…' : 'Make broadcaster'}</button></li>{/if}
							{/if}
							<li><button onclick={() => mute(p.user_id, { mic: true, camera: true, screen: true })}>Mute and stop sharing</button></li>
							<li><button onclick={() => allow([p.user_id], { mic: p.allow_mic, camera: p.allow_camera, screen: p.allow_screen }, true)}>Ask to turn on</button></li>
							<li><button onclick={() => act(() => client.put(path + '/participants/' + p.user_id + '/chat-muted', { muted: !p.chat_muted }))}>{p.chat_muted ? 'Let write in chat' : 'Mute in chat'}</button></li>
							{#if lead}<li><button class="text-error" onclick={() => remove(p)}>Remove from session</button></li>{/if}
						</ul>
					</details>
				</li>
			{:else}
				<li class="muted small">No students yet. Share the code or link.</li>
			{/each}
		</ul>
	</div>
</section>

<style>
	.list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
	.list li { display: flex; align-items: center; gap: 0.4rem; min-height: 2rem; }
	.name { max-width: 12rem; }
	.tog { display: inline-flex; align-items: center; gap: 0.2rem; font-size: 0.75rem; font-weight: 400; }
	.dot { width: 0.55rem; height: 0.55rem; border-radius: 50%; background: var(--color-base-300); flex: none; }
	.dot.on { background: var(--color-success); }
</style>
