<script lang="ts">
	// A tutoring session's room (D-59): the waiting room, the broadcast, the
	// teacher's controls, raised hands and chat. Joining needs a login and a
	// computer (TS-FR-02, TS-FR-09).
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { looksLikeComputer, TutorClient, TutorSocket, tutoringConfig, type SocketEvent } from '$lib/tutoring/client';
	import { SessionMedia, type Tile, type Tokens } from '$lib/tutoring/media.svelte';
	import TrackView from '$lib/tutoring/TrackView.svelte';
	import ChatPanel from '$lib/tutoring/ChatPanel.svelte';
	import PeoplePanel from '$lib/tutoring/PeoplePanel.svelte';
	import { broadcasterName, CHAT_MODES, isBroadcaster, isStaffRole, LAYOUTS, publishTarget, type Attendee, type ChatMessage, type TutoringSession, type TutoringView } from '$lib/tutoring/types';

	const code = page.params.code ?? '';
	const client = new TutorClient();
	let view = $state<TutoringView | null>(null);
	let messages = $state<ChatMessage[]>([]);
	let problem = $state(''); // why we can't show the room
	let closed = $state(''); // why the service closed our connection
	let socketStatus = $state<'open' | 'reconnecting' | 'closed'>('reconnecting');
	let audioHost = $state<HTMLDivElement>();
	const media = new SessionMedia(() => audioHost);
	let socket: TutorSocket | null = null;
	let side = $state<'chat' | 'people' | 'attendance'>('chat');
	let ask = $state<{ mic: boolean; camera: boolean; screen: boolean } | null>(null);
	let offer = $state(false); // asked to broadcast (TS-FR-17)
	let myLayout = $state<TutoringSession['layout'] | null>(null); // a student's own choice (TS-FR-13)
	let lastLayout = '';
	let cameras = $state<MediaDeviceInfo[]>([]);
	let attendance = $state<Attendee[]>([]);
	let historyLoaded = false;

	// Staff: the lead teacher and co-teachers; some controls are the lead's only.
	const teacher = $derived(isStaffRole(view?.me.role));
	const lead = $derived(view?.me.role === 'teacher');
	const admitted = $derived(view?.me.state === 'admitted');
	const live = $derived(view?.session.status === 'live');
	const link = $derived(typeof location !== 'undefined' ? location.origin + '/tutor/' + code : '');
	// The broadcast, and backstage (students' and co-teachers' tracks, seen by the teachers only).
	const broadcast = $derived(media.tiles.filter((t) => !t.backstage));
	const stageTiles = $derived(media.tiles.filter((t) => t.backstage && (teacher || t.local)));
	const main = $derived<Tile | undefined>(broadcast.find((t) => !t.local) ?? broadcast[0]);
	const others = $derived(broadcast.filter((t) => t !== main));
	const layout = $derived(myLayout ?? view?.session.layout ?? 'spotlight');
	// The lead teacher keeps only the microphone while someone else broadcasts.
	const sidelined = $derived(!!view && lead && !!view.session.broadcaster_id);
	$effect(() => {
		// A new layout from the teacher replaces a student's own choice.
		const l = view?.session.layout ?? '';
		if (l !== lastLayout) {
			lastLayout = l;
			myLayout = null;
		}
	});

	onMount(async () => {
		if (!looksLikeComputer(window, navigator.userAgent)) {
			problem = 'Tutoring sessions need a computer. Open this link on a laptop or desktop.';
			return;
		}
		const cfg = await tutoringConfig();
		if (!cfg.enabled) {
			problem = 'Tutoring is unavailable on this platform.';
			return;
		}
	});

	$effect(() => {
		if (!auth.loaded || problem || view) return;
		if (!auth.user) {
			goto(loginUrl('/tutor/' + code), { replaceState: true });
			return;
		}
		join();
	});

	async function join() {
		try {
			view = await client.post<TutoringView>('/api/join/' + encodeURIComponent(code));
			socket = new TutorSocket(client, view.session.id, onEvent, (s, reason) => {
				socketStatus = s;
				if (s === 'closed' && reason) closed ||= reason;
			});
			await socket.open();
		} catch (e) {
			problem = e instanceof ApiError ? e.message : 'Could not open the session';
		}
	}

	function onEvent(e: SocketEvent) {
		switch (e.type) {
			case 'state':
				view = e.data as TutoringView;
				break;
			case 'chat':
				if (!messages.some((m) => m.id === e.data.id)) messages = [...messages, e.data];
				break;
			case 'chat_deleted':
				messages = messages.filter((m) => m.id !== e.data.id);
				break;
			case 'ask':
				ask = e.data;
				break;
			case 'broadcast_offer':
				offer = true;
				break;
			case 'broadcast_ended':
				toast('The teacher took the broadcast back');
				break;
			case 'broadcast_answer':
				toast(e.data.accepted ? `${e.data.name} is broadcasting` : `${e.data.name} declined to broadcast`, e.data.accepted ? 'info' : 'error');
				break;
			case 'ask_answer':
				toast(e.data.accepted ? `${e.data.name} is turning it on` : `${e.data.name} declined`, e.data.accepted ? 'info' : 'error');
				break;
			case 'closed':
				closed = e.data?.reason ?? 'closed';
				media.disconnect();
				break;
		}
	}

	// Chat history once admitted (TS-FR-44); media once allowed in (teachers can get ready before the start).
	$effect(() => {
		if (!view || !admitted || historyLoaded) return;
		historyLoaded = true;
		client.get<{ messages: ChatMessage[] }>(`/api/sessions/${view.session.id}/chat`).then((r) => {
			const ids = new Set(r.messages.map((m) => m.id));
			messages = [...r.messages, ...messages.filter((m) => !ids.has(m.id))];
		});
	});
	$effect(() => {
		if (!view || !admitted || closed || media.state !== 'idle') return;
		if (!teacher && !live) return;
		connectMedia();
	});

	async function connectMedia() {
		if (!view) return;
		try {
			const t = await client.post<Tokens>(`/api/sessions/${view.session.id}/media-token`);
			await media.connect(t, publishTarget(view));
			routed = routeKey(view);
		} catch (e) {
			media.state = 'error';
			media.error = e instanceof ApiError ? e.message : 'Could not join the video';
		}
	}

	// When the broadcaster changes, or a student is first allowed to share,
	// media moves: new tokens, publishing to the other room (D-60).
	let routed = '';
	const routeKey = (v: TutoringView) => `${publishTarget(v)}|${v.me.allow_mic || v.me.allow_camera || v.me.allow_screen}`;
	$effect(() => {
		if (!view || media.state !== 'connected') return;
		const key = routeKey(view);
		if (!routed || key === routed) return;
		routed = key;
		const v = view;
		client.post<Tokens>(`/api/sessions/${v.session.id}/media-token`).then((t) => media.update(t, publishTarget(v)), () => {});
	});

	onDestroy(() => {
		socket?.close();
		media.disconnect();
	});

	const sid = () => view!.session.id;
	async function act(f: () => Promise<unknown>, ok?: string) {
		try {
			await f();
			if (ok) toast(ok);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	const start = () => act(() => client.post(`/api/sessions/${sid()}/start`), 'The session is live');
	async function end() {
		if (!(await confirmDialog({ title: 'End the session for everyone?', body: 'Everyone is disconnected and all video stops. Nobody can join again.', confirm: 'End session', danger: true }))) return;
		act(() => client.post(`/api/sessions/${sid()}/end`));
	}
	const settings = (s: Record<string, unknown>) => act(() => client.put(`/api/sessions/${sid()}/settings`, s));
	const hand = (raised: boolean) => act(() => client.put(`/api/sessions/${sid()}/hand`, { raised }));

	// Devices: only ever after the person's own click (PO-3). A student just
	// allowed may not have joined backstage yet; join it first.
	async function deviceAct(f: () => Promise<unknown>) {
		try {
			if (view && !media.joined(publishTarget(view))) {
				const t = await client.post<Tokens>(`/api/sessions/${view.session.id}/media-token`);
				await media.update(t, publishTarget(view));
				routed = routeKey(view);
			}
			await f();
		} catch (e) {
			const msg = e instanceof Error ? e.message : String(e);
			toast(/Permission|NotAllowed/i.test(msg) ? 'Your browser blocked it. Allow the camera, microphone or screen for this site in the browser’s settings.' : msg, 'error');
		}
	}
	async function loadCameras() {
		cameras = await media.cameras();
	}
	// Picking from a menu closes it.
	const closeMenu = (e: Event) => ((e.currentTarget as HTMLElement).closest('details')?.removeAttribute('open'));
	async function acceptAsk() {
		const a = ask;
		ask = null;
		answer(true);
		if (a?.mic) await deviceAct(() => media.setMic(true));
		if (a?.camera) await deviceAct(() => media.addCamera());
		if (a?.screen) await deviceAct(() => media.addScreen());
	}
	// The teachers see whether a request was accepted or declined (TS-FR-22).
	const answer = (accepted: boolean) => client.post(`/api/sessions/${sid()}/ask-answer`, { accepted }).catch(() => {});
	function declineAsk() {
		ask = null;
		answer(false);
	}
	async function answerOffer(accept: boolean) {
		offer = false;
		await act(() => client.post(`/api/sessions/${sid()}/broadcast-answer`, { accept }), accept ? 'You are broadcasting: share your screen or camera' : undefined);
	}
	async function showAttendance() {
		side = 'attendance';
		attendance = (await client.get<{ attendance: Attendee[] }>(`/api/sessions/${sid()}/attendance`)).attendance;
	}
	const minutes = (s: number) => Math.round(s / 60);
	const closedText: Record<string, string> = {
		ended: 'The session has ended.',
		removed: 'The teacher removed you from this session.',
		refused: 'The teacher didn’t admit you to this session.',
		replaced: 'You opened this session in another tab or window, so this one has closed.',
		expired: 'Your sign-in expired. Reload the page to continue.'
	};
</script>

<svelte:head><title>{view?.session.title ?? 'Tutoring'} · Classroom Quiz</title></svelte:head>

<div class="page-container vstack tutor">
	{#if problem}
		<div class="card card-border bg-base-100 p-6 vstack"><h1 class="m-0 text-xl">Tutoring</h1><p class="m-0">{problem}</p></div>
	{:else if closed}
		<div class="card card-border bg-base-100 p-6 vstack">
			<h1 class="m-0 text-xl">{view?.session.title ?? 'Tutoring'}</h1>
			<p class="m-0">{closedText[closed] ?? 'You were disconnected.'}</p>
			{#if closed === 'replaced' || closed === 'expired'}<div><button class="btn" onclick={() => location.reload()}>Reload</button></div>{/if}
		</div>
	{:else if !view}
		<p class="muted">Joining…</p>
	{:else}
		<header class="row">
			<div class="min-w-0">
				<h1 class="m-0 text-xl truncate">{view.session.title}</h1>
				<p class="small muted m-0">{view.session.classroom_name} · {view.teachers.map((t) => t.name + (t.role === 'coteacher' ? ' (co-teacher)' : '')).join(', ')} · {view.online} here{#if teacher && view.waiting} · {view.waiting} waiting{/if}{#if view.session.broadcaster_id} · {broadcasterName(view)} is broadcasting{/if}</p>
			</div>
			<span class="spacer"></span>
			{#if socketStatus === 'reconnecting'}<span class="badge badge-soft warn" role="status">Reconnecting…</span>{/if}
			{#if media.state === 'reconnecting' || media.state === 'error'}<span class="badge badge-soft warn" role="status">Video is reconnecting</span>{/if}
			{#if live}<span class="badge badge-soft danger">● Live</span>{:else if view.session.status === 'open'}<span class="badge badge-soft">Not started</span>{/if}
			<label class="sr-only" for="layout">Layout</label>
			<select id="layout" class="select select-sm w-auto" value={layout} onchange={(e) => (lead ? settings({ layout: e.currentTarget.value }) : (myLayout = e.currentTarget.value as TutoringSession['layout']))} title={lead ? 'What students see' : 'Your own view, until the teacher changes it'}>
				{#each LAYOUTS as l (l.id)}<option value={l.id}>{l.label}</option>{/each}
			</select>
			{#if lead}
				{#if view.session.status === 'open'}<button class="btn btn-primary btn-sm" onclick={start}>Start session</button>{/if}
				<button class="btn btn-error btn-outline btn-sm" onclick={end}>End</button>
			{/if}
		</header>

		{#if !admitted}
			<div class="card card-border bg-base-100 p-6"><p class="m-0">Waiting for the teacher to let you in…</p></div>
		{:else if !teacher && !live}
			<div class="card card-border bg-base-100 p-6"><p class="m-0">You're in. The session starts when the teacher starts it.</p></div>
		{:else}
			<div class="layout">
				<div class="stage vstack">
					{#if media.needsClick}<button class="btn btn-primary" onclick={() => media.startAudio()}>Click to hear the session</button>{/if}
					{#if !main}
						<div class="empty">{isBroadcaster(view) ? 'Share a screen or camera; everyone will see it here.' : `${broadcasterName(view)} isn’t sharing anything yet.`}{media.state === 'error' ? ' ' + media.error : ''}</div>
					{:else if layout === 'spotlight'}
						<TrackView tile={main} big onstop={main.local ? () => media.stop(main) : undefined} />
						{#if others.length}<div class="thumbs">{#each others as t (t.key)}<TrackView tile={t} onstop={t.local ? () => media.stop(t) : undefined} />{/each}</div>{/if}
					{:else}
						<div class={layout === 'side' ? 'side-by-side' : 'grid-view'}>{#each broadcast as t (t.key)}<TrackView tile={t} onstop={t.local ? () => media.stop(t) : undefined} />{/each}</div>
					{/if}
					{#if stageTiles.length}
						<h2 class="m-0 text-sm">{teacher ? 'Shared with the teachers only' : 'What you share: only your teachers see it'}</h2>
						<div class="thumbs">{#each stageTiles as t (t.key)}<TrackView tile={t} onstop={t.local ? () => media.stop(t) : undefined} />{/each}</div>
					{/if}

					<div class="controls row">
						{#if media.canMic}
							<button class="btn btn-sm" class:btn-primary={media.micOn} onclick={() => deviceAct(() => media.setMic(!media.micOn))} disabled={media.state !== 'connected'}>{media.micOn ? 'Mute mic' : 'Turn mic on'}</button>
						{:else}
							<button class="btn btn-sm" disabled title="Your teacher hasn't allowed this">Mic</button>
						{/if}
						{#if sidelined}
							<span class="small muted">{broadcasterName(view)} is broadcasting; your microphone still reaches everyone.</span>
						{:else if media.canScreen}
							<button class="btn btn-sm" onclick={() => deviceAct(() => media.addScreen())} disabled={media.state !== 'connected'}>{teacher ? 'Share a screen' : 'Share my screen'}</button>
						{:else}
							<button class="btn btn-sm" disabled title="Your teacher hasn't allowed this">Screen</button>
						{/if}
						{#if sidelined}
							<span></span>
						{:else if media.canCamera}
							<details class="dropdown" ontoggle={(e) => e.currentTarget.open && loadCameras()}>
								<summary class="btn btn-sm" class:btn-disabled={media.state !== 'connected'}>Add a camera</summary>
								<ul class="menu dropdown-content z-40 w-64 rounded-box border border-base-300 bg-base-100 p-1 shadow">
									<li><button onclick={(e) => { closeMenu(e); deviceAct(() => media.addCamera()); }}>Default camera</button></li>
									{#each cameras.filter((c) => c.deviceId) as c (c.deviceId)}<li><button onclick={(e) => { closeMenu(e); deviceAct(() => media.addCamera(c.deviceId, c.label || undefined)); }}>{c.label || 'Camera'}</button></li>{/each}
								</ul>
							</details>
						{:else}
							<button class="btn btn-sm" disabled title="Your teacher hasn't allowed this">Camera</button>
						{/if}
						{#if !teacher}
							<button class="btn btn-sm" class:btn-warning={!!view.me.hand_at} onclick={() => hand(!view?.me.hand_at)}>{view.me.hand_at ? 'Lower hand' : 'Raise hand'}</button>
							{#if !media.canMic && !media.canCamera && !media.canScreen}<span class="small muted">Your teacher hasn't allowed you to share anything.</span>{/if}
							{#if media.tiles.some((t) => t.local && t.backstage)}<span class="badge badge-soft">Your teachers can see what you share</span>{/if}
						{/if}
					</div>
					<div bind:this={audioHost} hidden></div>
				</div>

				<aside class="side card card-border bg-base-100 p-3 vstack">
					<div class="tabs tabs-border" role="tablist">
						<button class="tab" role="tab" aria-selected={side === 'chat'} class:tab-active={side === 'chat'} onclick={() => (side = 'chat')}>Chat</button>
						{#if teacher}
							<button class="tab" role="tab" aria-selected={side === 'people'} class:tab-active={side === 'people'} onclick={() => (side = 'people')}>People{#if view.waiting} ({view.waiting}){/if}</button>
							<button class="tab" role="tab" aria-selected={side === 'attendance'} class:tab-active={side === 'attendance'} onclick={showAttendance}>Attendance</button>
						{/if}
					</div>
					{#if side === 'chat'}
						<ChatPanel {client} {view} {messages} />
					{:else if side === 'people' && teacher}
						<PeoplePanel {client} {view} />
					{:else if side === 'attendance' && teacher}
						<div class="vstack">
							<button class="btn btn-sm" onclick={() => act(() => client.download(`/api/sessions/${sid()}/attendance?format=csv`, `attendance-${code}.csv`))}>Download CSV</button>
							<table class="table table-sm"><thead><tr><th>Name</th><th>Visits</th><th>Minutes</th></tr></thead><tbody>
								{#each attendance as a (a.user_id)}<tr><td>{a.name}{#if a.online} <span class="badge badge-soft ok badge-xs">here</span>{/if}</td><td class="tabular">{a.visits}</td><td class="tabular">{minutes(a.seconds)}</td></tr>{:else}<tr><td colspan="3" class="muted">Attendance starts when the session does.</td></tr>{/each}
							</tbody></table>
						</div>
					{/if}
				</aside>
			</div>

			{#if teacher}
				<details class="card card-border bg-base-100 p-4">
					<summary class="font-semibold">Joining and chat settings</summary>
					<div class="settings mt-3">
						<div class="vstack gap-1">
							<p class="small m-0">Code <strong class="tabular">{view.session.join_code}</strong> · <a href={link}>{link}</a></p>
							<QrCode text={link} size={160} />
						</div>
						<div class="vstack gap-2">
							<label class="row font-normal"><input type="checkbox" class="toggle toggle-sm" checked={view.session.locked} onchange={(e) => settings({ locked: e.currentTarget.checked })} /> Lock: nobody new can join</label>
							<label class="row font-normal"><input type="checkbox" class="toggle toggle-sm" checked={view.session.admit_mode === 'manual'} onchange={(e) => settings({ admit_mode: e.currentTarget.checked ? 'manual' : 'auto' })} /> Students wait until I admit them</label>
							<label class="small" for="chat-mode">Chat</label>
							<select id="chat-mode" class="select select-sm" value={view.session.chat_mode} onchange={(e) => settings({ chat_mode: e.currentTarget.value })}>{#each CHAT_MODES as m (m.id)}<option value={m.id}>{m.label}</option>{/each}</select>
							<label class="small" for="slow">Slow mode</label>
							<select id="slow" class="select select-sm" value={String(view.session.slow_seconds)} onchange={(e) => settings({ slow_seconds: Number(e.currentTarget.value) })}>{#each [0, 10, 30, 60, 120] as n (n)}<option value={String(n)}>{n ? `One message every ${n} s` : 'Off'}</option>{/each}</select>
						</div>
					</div>
				</details>
			{/if}
		{/if}
	{/if}
</div>

<dialog class="modal" open={!!ask} aria-labelledby="ask-h">
	{#if ask}
		<div class="modal-box vstack">
			<h2 id="ask-h" class="m-0 text-lg">Your teacher asks you to turn on your {[ask.mic && 'microphone', ask.camera && 'camera', ask.screen && 'screen'].filter(Boolean).join(' and ')}</h2>
			<p class="m-0 small muted">Nothing turns on unless you choose it. Your browser will ask for permission.</p>
			<div class="modal-action"><button class="btn" onclick={declineAsk}>Not now</button><button class="btn btn-primary" onclick={acceptAsk}>Turn on</button></div>
		</div>
	{/if}
</dialog>

<dialog class="modal" open={offer} aria-labelledby="offer-h">
	{#if offer}
		<div class="modal-box vstack">
			<h2 id="offer-h" class="m-0 text-lg">The teacher asks you to present to the class</h2>
			<p class="m-0 small muted">Everyone will see and hear what you share until the teacher takes the broadcast back. Nothing turns on until you choose it.</p>
			<div class="modal-action"><button class="btn" onclick={() => answerOffer(false)}>Not now</button><button class="btn btn-primary" onclick={() => answerOffer(true)}>Present</button></div>
		</div>
	{/if}
</dialog>

<style>
	.tutor { max-width: 1400px; }
	.layout { display: grid; gap: 1rem; grid-template-columns: minmax(0, 1fr) 22rem; align-items: start; }
	@media (max-width: 1000px) { .layout { grid-template-columns: 1fr; } }
	.stage { min-width: 0; }
	.empty { aspect-ratio: 16 / 9; display: grid; place-items: center; text-align: center; padding: 1rem; border-radius: 0.75rem; background: var(--color-base-200); color: var(--color-muted); }
	.thumbs { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr)); }
	.side-by-side { display: grid; gap: 0.5rem; grid-template-columns: repeat(2, minmax(0, 1fr)); }
	.grid-view { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(20rem, 1fr)); }
	.controls { flex-wrap: wrap; gap: 0.5rem; }
	.side { position: sticky; top: 4.5rem; max-height: calc(100vh - 6rem); overflow-y: auto; }
	.settings { display: grid; gap: 1.5rem; grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr)); }
</style>
