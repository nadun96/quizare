<script lang="ts">
	// A peer-to-peer meeting (D-69): a lobby to check the microphone and
	// camera, then the call. The call fills the screen with nothing to scroll
	// (D-72): the stage shows the whiteboard, a shared screen or everyone's
	// video, sized to fit; the bars float over it and fade when the pointer
	// rests. Media goes browser to browser.
	import { onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { MeetingCall } from '$lib/meet/call.svelte';
	import { ENDINGS, fitGrid, type MeetingAccess } from '$lib/meet/mesh';
	import Tile from '$lib/meet/Tile.svelte';
	import Whiteboard from '$lib/board/Whiteboard.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import IconBtn from '$lib/ui/IconBtn.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';

	const code = (page.params.code ?? '').toUpperCase();
	let access = $state<MeetingAccess | null>(null);
	let problem = $state('');
	let joined = $state(false);
	let starting = $state<'' | 'mic' | 'cam'>('');
	const call = new MeetingCall(code, (m) => toast(m, 'error'));
	const canShare = typeof navigator !== 'undefined' && !!navigator.mediaDevices?.getDisplayMedia;
	const link = typeof location !== 'undefined' ? location.origin + '/m/' + code : '';

	$effect(() => {
		if (!auth.loaded || access || problem) return;
		if (!auth.user) {
			goto(loginUrl('/m/' + code), { replaceState: true });
			return;
		}
		if (typeof RTCPeerConnection === 'undefined' || !navigator.mediaDevices) {
			problem = 'This browser can’t make video calls. Try a recent Chrome, Edge, Firefox or Safari.';
			return;
		}
		api.get<MeetingAccess>('/api/meetings/' + encodeURIComponent(code)).then(
			(a) => (access = a),
			(e) => (problem = e instanceof ApiError ? e.message : 'Could not open the meeting')
		);
	});

	onDestroy(() => call.leave());

	async function toggle(what: 'mic' | 'cam') {
		starting = what;
		try {
			if (what === 'mic') await call.setMic(!call.mic);
			else await call.setCam(!call.cam);
		} finally {
			starting = '';
		}
	}

	function join() {
		if (!access) return;
		joined = true;
		call.join(access.ice_servers);
	}

	async function endForAll() {
		if (await confirmDialog({ title: 'End the meeting for everyone?', body: 'Everyone leaves, the link stops working and the whiteboard is cleared.', confirm: 'End meeting', danger: true })) call.endForAll();
	}

	async function remove(peerId: string, name: string) {
		if (await confirmDialog({ title: `Remove ${name}?`, body: 'They can join again with the link while the meeting is open.', confirm: 'Remove', danger: true })) call.remove(peerId);
	}

	async function copyLink() {
		try {
			await navigator.clipboard.writeText(link);
			toast('Link copied');
		} catch {
			toast(link);
		}
	}

	const host = $derived(!!call.me?.host || !!access?.host);
	const board = call.board;
	// Participants may look at the people instead while the host shows the board.
	let peopleView = $state(false);
	$effect(() => {
		if (!board.open) peopleView = false;
	});
	const showBoard = $derived(board.open && !peopleView);

	type Shown = { key: string; name: string; stream: MediaStream | null; rev: number; self: boolean };
	const screens = $derived<Shown[]>([
		...(call.screen ? [{ key: 'me-screen', name: call.me?.name ?? 'You', stream: call.screenStream, self: true, rev: call.localRev }] : []),
		...call.people.filter((p) => p.screen && call.remotes[p.peer_id]?.screen).map((p) => ({ key: p.peer_id + '-screen', name: p.name, stream: call.remotes[p.peer_id].screen, self: false, rev: call.remotes[p.peer_id].rev }))
	]);
	// The stage: the board, else shared screens, else everyone's video. People
	// go to the strip whenever something else has the stage.
	const stage = $derived(showBoard ? 'board' : screens.length ? 'screens' : 'people');
	const count = $derived(call.people.length + 1);
	const linkLabel: Record<string, string> = { new: 'Connecting…', connecting: 'Connecting…', disconnected: 'Reconnecting…', failed: 'Can’t connect: a network blocks it' };
	// A link only comes up once one side has something to send.
	const sending = (p: { mic: boolean; cam: boolean; screen: boolean }) => p.mic || p.cam || p.screen || call.mic || call.cam || call.screen;
	const hands = $derived(call.people.filter((p) => p.hand).length + (call.hand ? 1 : 0));
	let lastHands = 0;
	$effect(() => {
		if (host && hands > lastHands) toast(hands === 1 ? 'A hand is raised' : `${hands} hands are raised`);
		lastHands = hands;
	});

	// Tiles sized to fill the stage with nothing to scroll.
	let stageW = $state(0);
	let stageH = $state(0);
	const grid = $derived(fitGrid(stage === 'screens' ? screens.length : count, stageW, stageH));
	let stripOpen = $state(true);

	// The bars fade after a few seconds without the pointer moving, and while
	// drawing; they come back on any move, tap or key. "Keep visible" pins them.
	let idle = $state(false);
	let pinned = $state(false);
	let barsHaveFocus = $state(false);
	let idleTimer: ReturnType<typeof setTimeout> | undefined;
	function wake(e?: PointerEvent) {
		if (e && e.buttons) {
			idle = !pinned; // drawing: get out of the way
			return;
		}
		idle = false;
		clearTimeout(idleTimer);
		idleTimer = setTimeout(() => (idle = !pinned), 3000);
	}
	$effect(() => {
		if (joined) wake();
		return () => clearTimeout(idleTimer);
	});
	const away = $derived(idle && !pinned && !barsHaveFocus);

	function setBoard(open: boolean) {
		call.setBoard(open);
		toast(open ? 'The whiteboard is showing for everyone' : 'The whiteboard is hidden');
	}
</script>

<svelte:head><title>{access?.meeting.title ?? 'Meeting'}</title></svelte:head>

{#snippet person(p: (typeof call.people)[number], small = false)}
	{@const r = call.remotes[p.peer_id]}
	<Tile name={p.name} stream={r?.cam ?? null} rev={r?.rev ?? 0} video={p.cam && !!r?.cam} mic={p.mic} hand={p.hand} host={p.host}
		status={sending(p) && r && r.link !== 'connected' ? (linkLabel[r.link] ?? '') : ''}>
		{#snippet actions()}
			{#if host && !small}<IconBtn icon="x" label="Remove" hint="Remove {p.name}" class="btn-xs btn-ghost" onclick={() => remove(p.peer_id, p.name)} />{/if}
		{/snippet}
	</Tile>
{/snippet}
{#snippet me()}
	<Tile name={call.me?.name ?? 'You'} stream={call.local} rev={call.localRev} video={call.cam} self mic={call.mic} hand={call.hand} host={host} />
{/snippet}

{#if call.ending}
	<div class="center-screen">
		<div class="card card-border vstack bg-base-100 p-6 text-center shadow-sm">
			<div class="mx-auto grid size-14 place-items-center rounded-2xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="video-off" size={28} /></div>
			<h1 class="m-0">{ENDINGS[call.ending].title}</h1>
			<p class="muted m-0">{ENDINGS[call.ending].body}</p>
			<div class="flex flex-wrap justify-center gap-2">
				{#if call.ending === 'left' || call.ending === 'full'}<button class="btn btn-primary" onclick={() => location.reload()}>Rejoin</button>{/if}
				<a class="btn" href={auth.home()}>Home</a>
			</div>
		</div>
	</div>
{:else if problem}
	<div class="center-screen">
		<div class="card card-border vstack bg-base-100 p-6 text-center shadow-sm" role="alert">
			<h1 class="m-0">Can’t join this meeting</h1>
			<p class="muted m-0">{problem}</p>
			<div class="flex flex-wrap justify-center gap-2"><a class="btn" href="/join">Try another code</a><a class="btn btn-ghost" href={auth.home()}>Home</a></div>
		</div>
	</div>
{:else if !access}
	<div class="center-screen"><div class="w-full max-w-md"><Skeleton lines={5} /></div></div>
{:else if !joined}
	<div class="center-screen">
		<div class="card card-border vstack lobby bg-base-100 p-5 shadow-sm sm:p-7">
			<div>
				<p class="small muted m-0"><a href={auth.home()}>← Home</a> · Meeting{access.meeting.host_name ? ` hosted by ${access.meeting.host_name}` : ''}</p>
				<h1 class="m-0">{access.meeting.title}</h1>
			</div>
			<div class="preview"><Tile name={auth.user?.name ?? 'You'} stream={call.local} rev={call.localRev} video={call.cam} self mic={call.mic} /></div>
			<div class="flex flex-wrap gap-2">
				<button class="btn flex-1" class:btn-primary={call.mic} aria-pressed={call.mic} disabled={starting !== ''} onclick={() => toggle('mic')}>
					<Icon name={call.mic ? 'mic' : 'mic-off'} size={18} />{call.mic ? 'Microphone on' : 'Microphone off'}
				</button>
				<button class="btn flex-1" class:btn-primary={call.cam} aria-pressed={call.cam} disabled={starting !== ''} onclick={() => toggle('cam')}>
					<Icon name={call.cam ? 'video' : 'video-off'} size={18} />{call.cam ? 'Camera on' : 'Camera off'}
				</button>
			</div>
			<button class="btn btn-primary btn-lg w-full" onclick={join}>Join meeting<Icon name="arrow-right" size={18} /></button>
			<p class="small muted m-0 center">Video goes straight between the people in the call{access.meeting.max_people ? `, up to ${access.meeting.max_people}` : ''}. You can change these once you’re in.</p>
		</div>
	</div>
{:else}
	<div class="call" class:away class:with-strip={stage !== 'people' && stripOpen} onpointermove={wake} onpointerdown={wake} role="application" aria-label="Meeting: {access.meeting.title}">
		<main class="stage">
			<!-- The box inside the stage's padding: what the tiles must fit. -->
			<div class="fitbox" bind:clientWidth={stageW} bind:clientHeight={stageH}>
			{#if stage === 'board'}
				<Whiteboard {board} client={call.boardClient} teacher={host} title={access.meeting.title + ' whiteboard'} fit />
			{:else if stage === 'screens'}
				<div class="tiles" style:--cols={grid.cols} style:--tw="{grid.w}px">
					{#each screens as s (s.key)}<div class="cell"><Tile name={s.name} stream={s.stream} rev={s.rev} self={s.self} screen /></div>{/each}
				</div>
			{:else}
				<div class="tiles" style:--cols={grid.cols} style:--tw="{grid.w}px">
					<div class="cell">{@render me()}</div>
					{#each call.people as p (p.peer_id)}<div class="cell">{@render person(p)}</div>{/each}
				</div>
				{#if call.people.length === 0}
					<p class="alone">You’re the only one here. Share the link or the code <strong class="tabular">{code}</strong>.</p>
				{/if}
			{/if}
			</div>
		</main>

		{#if stage !== 'people'}
			<aside class="strip" class:closed={!stripOpen} aria-label="People in the meeting">
				<button class="btn btn-xs strip-toggle" aria-expanded={stripOpen} onclick={() => (stripOpen = !stripOpen)}>
					<Icon name="users" size={13} />{stripOpen ? 'Hide' : count}
				</button>
				{#if stripOpen}
					<div class="strip-tiles">
						<div class="cell">{@render me()}</div>
						{#each call.people as p (p.peer_id)}<div class="cell">{@render person(p, true)}</div>{/each}
					</div>
				{/if}
			</aside>
		{/if}

		<header class="bar top" onfocusin={() => (barsHaveFocus = true)} onfocusout={() => (barsHaveFocus = false)}>
			<h1 class="m-0 truncate text-base">{access.meeting.title}</h1>
			<span class="badge badge-soft"><Icon name="users" size={14} />{count}</span>
			{#if !call.connected}<span class="badge badge-warning"><Icon name="wifi-off" size={14} />Reconnecting…</span>{/if}
			<IconBtn icon="link" label="Copy link" hint="Copy the meeting link" class="btn-sm btn-ghost" onclick={copyLink} />
		</header>

		<div class="bar bottom" role="toolbar" aria-label="Call controls" onfocusin={() => (barsHaveFocus = true)} onfocusout={() => (barsHaveFocus = false)}>
			<button class="btn" class:btn-error={!call.mic} aria-pressed={call.mic} disabled={starting !== ''} onclick={() => toggle('mic')} title={call.mic ? 'Mute' : 'Unmute'}>
				<Icon name={call.mic ? 'mic' : 'mic-off'} size={18} /><span class="lbl">{call.mic ? 'Mute' : 'Unmute'}</span>
			</button>
			<button class="btn" class:btn-error={!call.cam} aria-pressed={call.cam} disabled={starting !== ''} onclick={() => toggle('cam')} title={call.cam ? 'Stop camera' : 'Start camera'}>
				<Icon name={call.cam ? 'video' : 'video-off'} size={18} /><span class="lbl">{call.cam ? 'Stop camera' : 'Start camera'}</span>
			</button>
			{#if canShare}
				<button class="btn" class:btn-primary={call.screen} aria-pressed={call.screen} onclick={() => call.setScreen(!call.screen)} title={call.screen ? 'Stop sharing' : 'Share screen'}>
					<Icon name="monitor" size={18} /><span class="lbl">{call.screen ? 'Stop sharing' : 'Share screen'}</span>
				</button>
			{/if}
			{#if host}
				<button class="btn" class:btn-primary={board.open} aria-pressed={board.open} onclick={() => setBoard(!board.open)} title={board.open ? 'Hide the whiteboard for everyone' : 'Show the whiteboard to everyone'}>
					<Icon name="brush" size={18} /><span class="lbl">Whiteboard</span>
				</button>
				{#if board.open}
					<button class="btn" aria-pressed={board.mode === 'everyone'} onclick={() => call.setBoard(true, board.mode === 'everyone' ? 'host' : 'everyone')} title="Who can draw">
						<Icon name={board.mode === 'everyone' ? 'users' : 'user'} size={18} /><span class="lbl">{board.mode === 'everyone' ? 'Everyone draws' : 'Only I draw'}</span>
					</button>
				{/if}
			{:else if board.open}
				<button class="btn" aria-pressed={!peopleView} onclick={() => (peopleView = !peopleView)} title={peopleView ? 'Show the whiteboard' : 'Show the people'}>
					<Icon name={peopleView ? 'brush' : 'users'} size={18} /><span class="lbl">{peopleView ? 'Whiteboard' : 'People'}</span>
				</button>
			{/if}
			<button class="btn" class:btn-warning={call.hand} aria-pressed={call.hand} onclick={() => call.setHand(!call.hand)} title={call.hand ? 'Lower hand' : 'Raise hand'}>
				<Icon name="hand" size={18} /><span class="lbl">{call.hand ? 'Lower hand' : 'Raise hand'}</span>
			</button>
			<IconBtn icon={pinned ? 'eye' : 'eye-off'} label={pinned ? 'Let controls hide' : 'Keep controls visible'} hint={pinned ? 'Let the controls hide when the pointer rests' : 'Keep the controls visible'} class="btn-ghost" aria-pressed={pinned} onclick={() => { pinned = !pinned; wake(); }} />
			{#if host}<button class="btn btn-error btn-outline" onclick={endForAll}><span class="lbl">End for all</span><span class="sm-only">End</span></button>{/if}
			<button class="btn btn-error" onclick={() => call.leave()} title="Leave"><Icon name="logout" size={18} /><span class="lbl">Leave</span></button>
		</div>
	</div>
{/if}

<style>
	.center-screen { min-height: 100dvh; display: grid; place-items: center; padding: 1rem; overflow: hidden; }
	.center-screen > :global(*) { width: min(100%, 32rem); }
	.lobby { max-height: calc(100dvh - 2rem); }
	/* The camera preview shrinks before anything else does, so the lobby never scrolls. */
	.preview { width: min(100%, calc((100dvh - 22rem) * 16 / 9)); min-width: 10rem; margin: 0 auto; }

	/* The call: exactly the screen, nothing to scroll. */
	.call { position: fixed; inset: 0; display: grid; grid-template-columns: minmax(0, 1fr); background: color-mix(in oklab, var(--color-base-content) 92%, black); color: #fff; overflow: hidden; }
	.call.with-strip { grid-template-columns: minmax(0, 1fr) 13rem; }
	/* Room is kept for the bars, so nothing jumps when they fade. */
	.stage { position: relative; min-width: 0; min-height: 0; padding: 3.5rem 0.75rem 4.5rem; }
	.fitbox { position: relative; width: 100%; height: 100%; display: grid; place-items: center; }
	.stage :global(.wb.fit) { color: var(--color-base-content); }
	.tiles { display: grid; grid-template-columns: repeat(var(--cols), var(--tw)); gap: 8px; place-content: center; width: 100%; height: 100%; }
	.cell { min-width: 0; }
	.alone { position: absolute; left: 0; right: 0; bottom: 0.5rem; width: fit-content; max-width: calc(100% - 1rem); margin: 0 auto; padding: 0.4rem 0.8rem; border-radius: 999px; background: rgb(0 0 0 / 0.55); font-size: 0.9rem; text-align: center; }

	.strip { position: relative; min-height: 0; padding: 3.5rem 0.75rem 4.5rem 0; display: flex; flex-direction: column; gap: 0.5rem; }
	.strip.closed { position: absolute; right: 0.75rem; top: 3.5rem; padding: 0; }
	.strip-toggle { align-self: flex-end; }
	.strip-tiles { display: flex; flex-direction: column; gap: 0.5rem; overflow-y: auto; min-height: 0; scrollbar-width: thin; }

	.bar { position: absolute; z-index: 10; display: flex; align-items: center; gap: 0.5rem; padding: 0.4rem 0.6rem; border-radius: 1rem;
		background: color-mix(in oklab, var(--color-base-100) 94%, transparent); color: var(--color-base-content);
		border: 1px solid var(--color-base-300); box-shadow: 0 6px 24px rgb(0 0 0 / 0.25); transition: opacity 0.25s, translate 0.25s; }
	.bar.top { top: 0.5rem; left: 0.75rem; max-width: min(36rem, calc(100% - 1.5rem)); }
	/* Centred with auto margins: left: 50% would cap its width at half the screen. */
	.bar.bottom { bottom: 0.5rem; left: 0; right: 0; width: max-content; max-width: calc(100% - 1rem); margin: 0 auto; flex-wrap: wrap; justify-content: center; }
	.call.away .bar { opacity: 0; pointer-events: none; }
	.call.away .bar.top { translate: 0 -0.5rem; }
	.call.away .bar.bottom { translate: 0 0.5rem; }
	.sm-only { display: none; }

	@media (max-width: 760px), (max-aspect-ratio: 4/5) {
		/* Phones: the people row sits along the bottom, above the controls. */
		.call.with-strip { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 1fr) auto; }
		.stage { padding-bottom: 0.5rem; }
		.strip { padding: 0 0.5rem 4.25rem; }
		.strip-tiles { flex-direction: row; overflow-x: auto; overflow-y: hidden; }
		.strip-tiles .cell { flex: 0 0 8rem; }
		.strip-toggle { position: absolute; right: 0.5rem; top: -2rem; }
		.strip.closed { top: auto; bottom: 4.5rem; right: 0.5rem; }
		.bar .lbl { display: none; }
		.sm-only { display: inline; }
		.bar.bottom { gap: 0.3rem; padding: 0.35rem; }
		.bar.bottom :global(.btn) { padding-inline: 0.6rem; }
	}
	@media (prefers-reduced-motion: reduce) { .bar { transition: none; } }
</style>
