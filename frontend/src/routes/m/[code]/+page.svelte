<script lang="ts">
	// A peer-to-peer meeting (D-69): a lobby to check the microphone and
	// camera, then a grid of everyone's video. Media goes browser to browser.
	import { onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { MeetingCall } from '$lib/meet/call.svelte';
	import { ENDINGS, gridColumns, type MeetingAccess } from '$lib/meet/mesh';
	import Tile from '$lib/meet/Tile.svelte';
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
		if (await confirmDialog({ title: 'End the meeting for everyone?', body: 'Everyone leaves, and the link stops working.', confirm: 'End meeting', danger: true })) call.endForAll();
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
	// Shared screens first and larger, then cameras: yours first.
	const screens = $derived([
		...(call.screen ? [{ key: 'me-screen', name: call.me?.name ?? 'You', stream: call.screenStream, self: true, rev: call.localRev }] : []),
		...call.people.filter((p) => p.screen && call.remotes[p.peer_id]?.screen).map((p) => ({ key: p.peer_id + '-screen', name: p.name, stream: call.remotes[p.peer_id].screen, self: false, rev: call.remotes[p.peer_id].rev }))
	]);
	const count = $derived(call.people.length + 1);
	const cols = $derived(gridColumns(count));
	const linkLabel: Record<string, string> = { new: 'Connecting…', connecting: 'Connecting…', disconnected: 'Reconnecting…', failed: 'Can’t connect' };
	// A link only comes up once one side has something to send.
	const sending = (p: { mic: boolean; cam: boolean; screen: boolean }) => p.mic || p.cam || p.screen || call.mic || call.cam || call.screen;
	const hands = $derived(call.people.filter((p) => p.hand).length + (call.hand ? 1 : 0));
	let lastHands = 0;
	$effect(() => {
		if (host && hands > lastHands) toast(hands === 1 ? 'A hand is raised' : `${hands} hands are raised`);
		lastHands = hands;
	});
</script>

<svelte:head><title>{access?.meeting.title ?? 'Meeting'}</title></svelte:head>

{#if call.ending}
	<div class="narrow">
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
	<div class="narrow">
		<div class="card card-border vstack bg-base-100 p-6 text-center shadow-sm" role="alert">
			<h1 class="m-0">Can’t join this meeting</h1>
			<p class="muted m-0">{problem}</p>
			<a class="btn mx-auto" href="/join">Try another code</a>
		</div>
	</div>
{:else if !access}
	<div class="narrow"><Skeleton lines={5} /></div>
{:else if !joined}
	<div class="narrow">
		<div class="card card-border vstack bg-base-100 p-5 shadow-sm sm:p-7">
			<div>
				<p class="small muted m-0">Meeting{access.meeting.host_name ? ` · hosted by ${access.meeting.host_name}` : ''}</p>
				<h1 class="m-0">{access.meeting.title}</h1>
			</div>
			<Tile name={auth.user?.name ?? 'You'} stream={call.local} rev={call.localRev} video={call.cam} self mic={call.mic} />
			<div class="flex flex-wrap gap-2">
				<button class="btn flex-1" class:btn-primary={call.mic} aria-pressed={call.mic} disabled={starting !== ''} onclick={() => toggle('mic')}>
					<Icon name={call.mic ? 'mic' : 'mic-off'} size={18} />{call.mic ? 'Microphone on' : 'Microphone off'}
				</button>
				<button class="btn flex-1" class:btn-primary={call.cam} aria-pressed={call.cam} disabled={starting !== ''} onclick={() => toggle('cam')}>
					<Icon name={call.cam ? 'video' : 'video-off'} size={18} />{call.cam ? 'Camera on' : 'Camera off'}
				</button>
			</div>
			<button class="btn btn-primary btn-lg w-full" onclick={join}>Join meeting<Icon name="arrow-right" size={18} /></button>
			<p class="small muted m-0 center">Video goes straight between the people in the call, up to {access.meeting.max_people}. You can change these once you’re in.</p>
		</div>
	</div>
{:else}
	<div class="meet">
		<header class="flex flex-wrap items-center gap-2">
			<h1 class="m-0 truncate text-lg">{access.meeting.title}</h1>
			<span class="badge badge-soft"><Icon name="users" size={14} />{count}</span>
			{#if !call.connected}<span class="badge badge-warning"><Icon name="wifi-off" size={14} />Reconnecting…</span>{/if}
			<span class="spacer"></span>
			<IconBtn icon="link" label="Copy link" hint="Copy the meeting link" class="btn-sm btn-ghost" onclick={copyLink} />
		</header>

		{#if screens.length}
			<div class="screens" style="--cols:{Math.min(2, screens.length)}">
				{#each screens as s (s.key)}<Tile name={s.name} stream={s.stream} rev={s.rev} self={s.self} screen />{/each}
			</div>
		{/if}

		<div class="grid-tiles" style="--cols:{cols}" class:strip={screens.length > 0}>
			<Tile name={call.me?.name ?? 'You'} stream={call.local} rev={call.localRev} video={call.cam} self mic={call.mic} hand={call.hand} host={host} />
			{#each call.people as p (p.peer_id)}
				{@const r = call.remotes[p.peer_id]}
				<Tile name={p.name} stream={r?.cam ?? null} rev={r?.rev ?? 0} video={p.cam && !!r?.cam} mic={p.mic} hand={p.hand} host={p.host} status={sending(p) && r && r.link !== 'connected' ? (linkLabel[r.link] ?? '') : ''}>
					{#snippet actions()}
						{#if host}<IconBtn icon="x" label="Remove" hint="Remove {p.name}" class="btn-xs btn-ghost" onclick={() => remove(p.peer_id, p.name)} />{/if}
					{/snippet}
				</Tile>
			{/each}
		</div>
		{#if call.people.length === 0}
			<p class="muted center m-0">You’re the only one here. Share the link or the code <strong class="tabular">{code}</strong>.</p>
		{/if}

		<div class="controls" role="toolbar" aria-label="Call controls">
			<button class="btn" class:btn-error={!call.mic} aria-pressed={call.mic} disabled={starting !== ''} onclick={() => toggle('mic')}>
				<Icon name={call.mic ? 'mic' : 'mic-off'} size={18} /><span class="lbl">{call.mic ? 'Mute' : 'Unmute'}</span>
			</button>
			<button class="btn" class:btn-error={!call.cam} aria-pressed={call.cam} disabled={starting !== ''} onclick={() => toggle('cam')}>
				<Icon name={call.cam ? 'video' : 'video-off'} size={18} /><span class="lbl">{call.cam ? 'Stop camera' : 'Start camera'}</span>
			</button>
			{#if canShare}
				<button class="btn" class:btn-primary={call.screen} aria-pressed={call.screen} onclick={() => call.setScreen(!call.screen)}>
					<Icon name="monitor" size={18} /><span class="lbl">{call.screen ? 'Stop sharing' : 'Share screen'}</span>
				</button>
			{/if}
			<button class="btn" class:btn-warning={call.hand} aria-pressed={call.hand} onclick={() => call.setHand(!call.hand)}>
				<Icon name="hand" size={18} /><span class="lbl">{call.hand ? 'Lower hand' : 'Raise hand'}</span>
			</button>
			<span class="spacer"></span>
			{#if host}<button class="btn btn-error btn-outline" onclick={endForAll}>End for all</button>{/if}
			<button class="btn btn-error" onclick={() => call.leave()}><Icon name="logout" size={18} /><span class="lbl">Leave</span></button>
		</div>
	</div>
{/if}

<style>
	.meet { display: flex; flex-direction: column; gap: 0.75rem; max-width: 1400px; margin: 0 auto; padding: 0.75rem 1rem 1rem; }
	.screens { display: grid; grid-template-columns: repeat(var(--cols), minmax(0, 1fr)); gap: 0.5rem; }
	.grid-tiles { display: grid; grid-template-columns: repeat(var(--cols), minmax(0, 1fr)); gap: 0.5rem; }
	.grid-tiles.strip { grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); }
	.controls { position: sticky; bottom: 0.5rem; display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; padding: 0.5rem; border-radius: 1rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); box-shadow: 0 4px 16px rgb(0 0 0 / 0.08); }
	@media (max-width: 640px) {
		.grid-tiles { grid-template-columns: repeat(min(var(--cols), 2), minmax(0, 1fr)); }
		.screens { grid-template-columns: 1fr; }
		.controls .lbl { display: none; }
	}
</style>
