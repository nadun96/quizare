<script lang="ts">
	// A tutoring session's room (D-59): the waiting room, the broadcast, the
	// teacher's controls, raised hands, chat, breakout groups (D-79) and
	// recording the presenter (D-80).
	// Joining needs a login and a computer (TS-FR-02, TS-FR-09).
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { looksLikeComputer, TutorClient, TutorSocket, tutoringConfig, type SocketEvent } from '$lib/tutoring/client';
	import { rejoinDelay, SessionMedia, type Tile, type Tokens } from '$lib/tutoring/media.svelte';
	import TrackView from '$lib/tutoring/TrackView.svelte';
	import ChatPanel from '$lib/tutoring/ChatPanel.svelte';
	import PeoplePanel from '$lib/tutoring/PeoplePanel.svelte';
	import GroupsPanel from '$lib/tutoring/GroupsPanel.svelte';
	import RecordingsList from '$lib/tutoring/RecordingsList.svelte';
	import ResourcesPanel from '$lib/tutoring/ResourcesPanel.svelte';
	import SessionTiming from '$lib/tutoring/SessionTiming.svelte';
	import { PresenterRecorder } from '$lib/tutoring/recorder.svelte';
	import { sentence, withBudget } from '$lib/tutoring/budget';
	import { broadcasterName, CHAT_MODES, groupNow, isBroadcaster, isStaffRole, LAYOUTS, publishTarget, secondsLeft, STEP_LABELS, type Attendee, type AutoOffStep, type ChatMessage, type Recording, type Resources, type TutoringSession, type TutoringView } from '$lib/tutoring/types';

	const code = page.params.code ?? '';
	const client = new TutorClient();
	let view = $state<TutoringView | null>(null);
	let messages = $state<ChatMessage[]>([]);
	let problem = $state(''); // why we can't show the room
	let closed = $state(''); // why the service closed our connection
	let socketStatus = $state<'open' | 'reconnecting' | 'closed'>('reconnecting');
	let audioHost = $state<HTMLDivElement>();
	const media = new SessionMedia(() => audioHost, (m) => toast(m, 'error'));
	media.forceRelay = page.url.searchParams.get('relay') === '1'; // checking the relay (D-83)
	let socket: TutorSocket | null = null;
	let side = $state<'chat' | 'people' | 'groups' | 'recordings' | 'attendance' | 'resources'>('chat');
	// What the session sends now, pushed to the teachers every 2 s (D-81).
	let resources = $state<Resources | null>(null);
	async function showResources() {
		side = 'resources';
		if (view && !resources) resources = await client.get<Resources>(`/api/sessions/${view.session.id}/resources`).catch(() => null);
	}
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
	const broadcast = $derived(media.tiles.filter((t) => !t.backstage && !t.group));
	// Breakout groups (D-79): the group this person is in (or visiting), and its people.
	const group = $derived(view ? groupNow(view) : undefined);
	const groupTiles = $derived(media.tiles.filter((t) => t.group));
	let now = $state(Date.now());
	const closingIn = $derived(secondsLeft(view?.session.groups_close_at, now));
	// Recording (D-80): the recorder runs in the browser of the teacher who started it.
	let recorder = $state<PresenterRecorder | null>(null);
	let recordings = $state<Recording[]>([]);
	const recordingHere = $derived(!!recorder && recorder.state !== 'idle' && recorder.id === view?.recording?.id);
	function startRecording() {
		if (!view) return;
		const v = view;
		recorder = new PresenterRecorder(client, v.session.id, () => {
			const who = view?.session.broadcaster_id ?? view?.session.teacher_id ?? v.session.teacher_id;
			return { ...media.presenterTracks(who), name: view ? broadcasterName(view) : 'The presenter' };
		}, (m) => toast(m, 'error'));
		recorder.start();
	}
	async function loadRecordings() {
		side = 'recordings';
		if (view) recordings = (await client.get<{ recordings: Recording[] }>(`/api/sessions/${view.session.id}/recordings`)).recordings;
	}
	$effect(() => {
		if (!recordingHere) return;
		const warn = (e: BeforeUnloadEvent) => e.preventDefault(); // leaving stops the recording
		window.addEventListener('beforeunload', warn);
		return () => window.removeEventListener('beforeunload', warn);
	});
	const helpAsked = $derived(!!view && !!view.groups.find((g) => g.id === view!.me.group)?.help_at);
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

	onMount(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});
	onMount(async () => {
		if (!looksLikeComputer(window, navigator.userAgent)) {
			problem = 'Tutoring sessions need a computer. Open this link on a laptop or desktop.';
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

	// Asked once signed in: the platform answers only signed-in people, so a
	// student opening the link in a fresh browser goes to the login first.
	async function join() {
		if (!(await tutoringConfig()).enabled) {
			problem = 'Tutoring is unavailable on this platform.';
			return;
		}
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
			case 'groups_opened':
				if (!teacher) toast('Groups are open: you’re with your group now');
				break;
			case 'groups_closed':
				toast('The groups have closed: everyone is back together');
				break;
			case 'announcement':
				if (!teacher) toast(`${e.data.name}: ${e.data.text}`, 'info');
				break;
			case 'group_help':
				toast(`${e.data.name} asks for help in their group`, 'error');
				break;
			// The bandwidth budget (D-81).
			case 'resources':
				resources = e.data;
				break;
			case 'auto_disabled':
				toast(e.data.message, 'error');
				if (view && isBroadcaster(view)) media.cutBack(e.data.step);
				break;
			case 'resources_available':
				toast(`There’s bandwidth again: turn back on ${(e.data.auto_off as AutoOffStep[]).map((s) => STEP_LABELS[s].toLowerCase()).join(', ')} under Resources`);
				break;
			case 'restored':
				if (e.data.step === 'fps') media.restoreFps();
				else toast(e.data.step === 'camera' ? 'You can turn your camera back on' : 'You can add more screens and cameras again');
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
		// At once, before the token arrives: a state update in the meantime
		// would otherwise join a second time, and LiveKit closes the first
		// connection for the same person (DUPLICATE_IDENTITY).
		media.state = 'connecting';
		try {
			const t = await client.post<Tokens>(`/api/sessions/${view.session.id}/media-token`);
			await media.connect(t, publishTarget(view));
			routed = routeKey(view);
		} catch (e) {
			media.state = 'error';
			media.error = e instanceof ApiError ? e.message : 'Could not join the video';
		}
	}

	// The video coming back after a drop counts as a reconnect, for the admin's monitoring (D-82).
	let videoWasDown = false;
	$effect(() => {
		const st = media.state;
		if (st === 'reconnecting' || st === 'disconnected' || st === 'error') videoWasDown = true;
		else if (st === 'connected' && videoWasDown && view) {
			videoWasDown = false;
			client.post(`/api/sessions/${view.session.id}/media-reconnected`).catch(() => {});
		}
	});

	// The video connection never came up, or LiveKit gave up reconnecting:
	// join again, waiting longer each time (2 s doubling to 30 s).
	let rejoins = 0;
	$effect(() => {
		if (media.state === 'connected') rejoins = 0;
		if (closed || (media.state !== 'error' && media.state !== 'disconnected')) return;
		const t = setTimeout(() => {
			if (!closed) media.state = 'idle';
		}, rejoinDelay(rejoins++));
		return () => clearTimeout(t);
	});

	// When the broadcaster changes, or a student is first allowed to share,
	// media moves: new tokens, publishing to the other room (D-60).
	let routed = '';
	const routeKey = (v: TutoringView) => `${publishTarget(v)}|${v.me.allow_mic || v.me.allow_camera || v.me.allow_screen}|${groupNow(v)?.id ?? ''}`;
	$effect(() => {
		if (!view || media.state !== 'connected') return;
		const key = routeKey(view);
		if (!routed || key === routed) return;
		routed = key;
		const v = view;
		client.post<Tokens>(`/api/sessions/${v.session.id}/media-token`).then((t) => media.update(t, publishTarget(v)), () => {});
	});

	onDestroy(() => {
		recorder?.stop();
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
	// Starting checks the session's plan against the server's budget (TS-FR-32).
	async function start() {
		try {
			if (await withBudget((force) => client.post(`/api/sessions/${sid()}/start`, { force }), 'Start anyway')) toast('The session is live');
		} catch (e) {
			toast(e instanceof ApiError ? sentence(e.message) : 'That didn’t work', 'error');
		}
	}
	async function end() {
		if (!(await confirmDialog({ title: 'End the session for everyone?', body: 'Everyone is disconnected and all video stops. Nobody can join again.', confirm: 'End session', danger: true }))) return;
		act(() => client.post(`/api/sessions/${sid()}/end`));
	}
	const settings = (s: Record<string, unknown>) => act(() => client.put(`/api/sessions/${sid()}/settings`, s));
	const hand = (raised: boolean) => act(() => client.put(`/api/sessions/${sid()}/hand`, { raised }));
	const help = (on: boolean) => act(() => client.put(`/api/sessions/${sid()}/help`, { on }), on ? 'Your teachers have been asked to come to your group' : undefined);
	const choose = (groupId: number) => act(() => client.put(`/api/sessions/${sid()}/my-group`, { group_id: groupId }));

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
	// The broadcaster asks before sending one more screen or camera to everyone (TS-FR-32).
	async function addTrack(kind: 'screen' | 'camera', f: () => Promise<unknown>) {
		if (view && live && isBroadcaster(view) && publishTarget(view) === 'main') {
			try {
				if (!(await withBudget((force) => client.post(`/api/sessions/${sid()}/budget/track`, { kind, force }), 'Send it anyway'))) return;
			} catch (e) {
				toast(e instanceof ApiError ? sentence(e.message) : 'That didn’t work', 'error');
				return;
			}
		}
		await deviceAct(f);
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
				<p class="small muted m-0">{view.session.classroom_name} · {view.teachers.map((t) => t.name + (t.role === 'coteacher' ? ' (co-teacher)' : '')).join(', ')} · {view.online} here{#if teacher && view.waiting}{' · '}{view.waiting} waiting{/if}{#if view.session.broadcaster_id}{' · '}{broadcasterName(view)} is broadcasting{/if}</p>
			</div>
			<span class="spacer"></span>
			{#if socketStatus === 'reconnecting'}<span class="badge badge-soft warn" role="status">Reconnecting…</span>{/if}
			{#if !closed && (media.state === 'reconnecting' || media.state === 'error' || media.state === 'disconnected')}<span class="badge badge-soft warn" role="status">Video is reconnecting</span>{/if}
			{#if media.forceRelay}<span class="badge badge-soft" title="Opened with ?relay=1: video goes only through the relay on port 443">Relay only</span>{/if}
			{#if live}<span class="badge badge-soft danger">● Live</span>{:else if view.session.status === 'open'}<span class="badge badge-soft">{view.session.scheduled_at ? 'Starts ' + new Date(view.session.scheduled_at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : 'Not started'}</span>{/if}
			{#if view.recording}<span class="badge badge-error gap-1" role="status" title="Only the presenter’s screen and voice are recorded">{view.recording.status === 'paused' ? '❚❚ Recording paused' : '● Recording'}</span>{/if}
			{#if teacher && live}
				{#if recordingHere && recorder}
					<button class="btn btn-sm" onclick={() => recorder?.pause(recorder.state !== 'paused')}>{recorder.state === 'paused' ? 'Resume' : 'Pause'}</button>
					<button class="btn btn-sm btn-error" disabled={recorder.state === 'stopping'} onclick={() => recorder?.stop()}>{recorder.state === 'stopping' ? 'Saving…' : 'Stop recording'}</button>
				{:else if view.recording}
					<span class="small muted">Recorded by {view.recording.started_by_name}</span>
				{:else}
					<button class="btn btn-sm" disabled={recorder?.state === 'starting'} onclick={startRecording} title="Records the presenter’s screen and voice, in this browser">Record</button>
				{/if}
			{/if}
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
					{#if !teacher && view.session.groups_open}
						<div class="alert alert-soft py-2" class:alert-info={!!group} role="status">
							{#if group}
								<span class="flex-1">You’re in <strong>{group.name}</strong>{#if view.group_mates?.length}{' with '}{view.group_mates.map((m) => m.name).join(', ')}{/if}. Only your group sees and hears you here.</span>
								<button class="btn btn-sm" class:btn-warning={helpAsked} onclick={() => help(!helpAsked)}>{helpAsked ? 'Cancel the call' : 'Ask for help'}</button>
							{:else}
								<span>The class is in groups; your teacher will put you in one.</span>
							{/if}
						</div>
					{/if}
					{#if closingIn !== null}<p class="alert alert-soft alert-warning py-2 m-0" role="timer">Groups close in {closingIn} s</p>{/if}
					{#if !teacher && view.session.groups_choose && view.groups.length && !group}
						<div class="card card-border bg-base-100 p-3 vstack gap-2">
							<p class="m-0 font-semibold">Choose your group</p>
							<div class="row flex-wrap gap-2">
								{#each view.groups as g (g.id)}<button class="btn btn-sm" class:btn-primary={view.me.group === g.id} onclick={() => choose(g.id)}>{g.name} <span class="muted">({g.size})</span></button>{/each}
							</div>
						</div>
					{/if}
					{#if group}
						<h2 class="m-0 text-sm">{teacher ? `Visiting ${group.name}` : group.name}</h2>
						{#if groupTiles.length}
							<div class="grid-view">{#each groupTiles as t (t.key)}<TrackView tile={t} onstop={t.local ? () => media.stop(t) : undefined} />{/each}</div>
						{:else}
							<div class="empty small">{media.canMic || media.canCamera ? 'Nobody has turned on a camera here yet. Turn on your microphone to talk with your group.' : 'Your group is here. Use the chat to work together.'}</div>
						{/if}
						<h2 class="m-0 text-sm">The main room</h2>
					{/if}
					{#if view.recording && isBroadcaster(view)}<p class="alert alert-soft alert-error py-2 m-0" role="status">You’re presenting: your screen and voice are being recorded.</p>{/if}
					{#if media.needsClick}<button class="btn btn-primary" onclick={() => media.startAudio()}>Click to hear the session</button>{/if}
					<div class="main-room" class:mini={!!group}>
					{#if !main}
						<div class="empty">{isBroadcaster(view) ? 'Share a screen or camera; everyone will see it here.' : `${broadcasterName(view)} isn’t sharing anything yet.`}{media.state === 'error' ? ' ' + media.error : ''}</div>
					{:else if layout === 'spotlight'}
						<TrackView tile={main} big onstop={main.local ? () => media.stop(main) : undefined} />
						{#if others.length}<div class="thumbs">{#each others as t (t.key)}<TrackView tile={t} onstop={t.local ? () => media.stop(t) : undefined} />{/each}</div>{/if}
					{:else}
						<div class={layout === 'side' ? 'side-by-side' : 'grid-view'}>{#each broadcast as t (t.key)}<TrackView tile={t} onstop={t.local ? () => media.stop(t) : undefined} />{/each}</div>
					{/if}
					</div>
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
							<button class="btn btn-sm" onclick={() => addTrack('screen', () => media.addScreen())} disabled={media.state !== 'connected'}>{teacher ? 'Share a screen' : 'Share my screen'}</button>
						{:else}
							<button class="btn btn-sm" disabled title="Your teacher hasn't allowed this">Screen</button>
						{/if}
						{#if sidelined}
							<span></span>
						{:else if media.canCamera}
							<details class="dropdown" ontoggle={(e) => e.currentTarget.open && loadCameras()}>
								<summary class="btn btn-sm" class:btn-disabled={media.state !== 'connected'}>Add a camera</summary>
								<ul class="menu dropdown-content z-40 w-64 rounded-box border border-base-300 bg-base-100 p-1 shadow">
									<li><button onclick={(e) => { closeMenu(e); addTrack('camera', () => media.addCamera()); }}>Default camera</button></li>
									{#each cameras.filter((c) => c.deviceId) as c (c.deviceId)}<li><button onclick={(e) => { closeMenu(e); addTrack('camera', () => media.addCamera(c.deviceId, c.label || undefined)); }}>{c.label || 'Camera'}</button></li>{/each}
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
						<button class="tab" role="tab" aria-selected={side === 'recordings'} class:tab-active={side === 'recordings'} onclick={loadRecordings}>Recordings</button>
						{#if teacher}
							<button class="tab" role="tab" aria-selected={side === 'people'} class:tab-active={side === 'people'} onclick={() => (side = 'people')}>People{#if view.waiting}{' '}({view.waiting}){/if}</button>
							<button class="tab" role="tab" aria-selected={side === 'groups'} class:tab-active={side === 'groups'} onclick={() => (side = 'groups')}>Groups{#if view.groups.some((g) => g.help_at)}{' '}<span class="badge badge-warning badge-xs">help</span>{/if}</button>
							<button class="tab" role="tab" aria-selected={side === 'attendance'} class:tab-active={side === 'attendance'} onclick={showAttendance}>Attendance</button>
							<button class="tab" role="tab" aria-selected={side === 'resources'} class:tab-active={side === 'resources'} onclick={showResources}>Resources{#if view.session.auto_off.length}{' '}<span class="badge badge-warning badge-xs">{view.session.auto_off.length} off</span>{/if}</button>
						{/if}
					</div>
					{#if side === 'chat'}
						<ChatPanel {client} {view} {messages} />
					{:else if side === 'people' && teacher}
						<PeoplePanel {client} {view} />
					{:else if side === 'groups' && teacher}
						<GroupsPanel {client} {view} />
					{:else if side === 'recordings'}
						<RecordingsList {client} {recordings} meId={view.me.user_id} leadOf={(r) => r.lead} onchange={loadRecordings} />
						{#if !recordings.length && !teacher}<p class="small muted m-0">Your teacher chooses whether students can watch recordings.</p>{/if}
					{:else if side === 'resources' && teacher}
						<ResourcesPanel {client} {view} {resources} />
					{:else if side === 'attendance' && teacher}
						<div class="vstack">
							<button class="btn btn-sm" onclick={() => act(() => client.download(`/api/sessions/${sid()}/attendance?format=csv`, `attendance-${code}.csv`))}>Download CSV</button>
							<table class="table table-sm"><thead><tr><th>Name</th><th>Visits</th><th>Minutes</th></tr></thead><tbody>
								{#each attendance as a (a.user_id)}<tr><td>{a.name}{#if a.online}{' '}<span class="badge badge-soft ok badge-xs">here</span>{/if}</td><td class="tabular">{a.visits}</td><td class="tabular">{minutes(a.seconds)}</td></tr>{:else}<tr><td colspan="3" class="muted">Attendance starts when the session does.</td></tr>{/each}
							</tbody></table>
						</div>
					{/if}
				</aside>
			</div>

			{#if lead && view.session.status === 'open'}
				<details class="card card-border bg-base-100 p-4" open={!!view.session.scheduled_at && Date.parse(view.session.scheduled_at) > now + 60 * 60_000}>
					<summary class="font-semibold">Schedule{#if view.session.scheduled_at}{' '}<span class="font-normal small muted">· {new Date(view.session.scheduled_at).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}</span>{/if}</summary>
					<div class="mt-3"><SessionTiming {client} session={view.session} /></div>
				</details>
			{/if}
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
	/* While in a breakout group, the group comes first and the main room shrinks (D-79). */
	.main-room { display: grid; gap: 0.5rem; }
	.main-room.mini { max-width: 22rem; }
	.main-room.mini .empty { aspect-ratio: auto; padding: 0.75rem; }
	.empty { aspect-ratio: 16 / 9; display: grid; place-items: center; text-align: center; padding: 1rem; border-radius: 0.75rem; background: var(--color-base-200); color: var(--color-muted); }
	.thumbs { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr)); }
	.side-by-side { display: grid; gap: 0.5rem; grid-template-columns: repeat(2, minmax(0, 1fr)); }
	.grid-view { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(20rem, 1fr)); }
	.controls { flex-wrap: wrap; gap: 0.5rem; }
	.side { position: sticky; top: 4.5rem; max-height: calc(100vh - 6rem); overflow-y: auto; }
	.settings { display: grid; gap: 1.5rem; grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr)); }
</style>
