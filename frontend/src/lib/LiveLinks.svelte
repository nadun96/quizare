<script lang="ts">
	// Public live leaderboard links (V2-08, D-45): anyone with the link sees
	// the rankings update, without logging in. Names never appear.
	import { api, ApiError } from './api';
	import { Paged } from './paged.svelte';
	import Pager from './ui/Pager.svelte';
	import QrCode from './QrCode.svelte';
	import Icon from './ui/Icon.svelte';
	import { confirmDialog } from './ui/dialog.svelte';
	import { flyIn } from './ui/motion';
	import { toast } from './ui/toast.svelte';

	type Link = { id: string; scope: string; views: string[]; identify: string; label: string; expires_at: string | null; revoked_at: string | null; created_at: string; token?: string };
	let { scope, targetId, teams = false }: { scope: 'live_poll' | 'live_session'; targetId: string; teams?: boolean } = $props();

	const poll = $derived(scope === 'live_poll');
	// The links of this target and scope, one page at a time (PL-FR-02).
	const links = new Paged<Link>(() => '/api/teacher/share-links', 'links', { sort: 'created', desc: true, filters: () => ({ target_id: targetId, scope }) });
	let fresh = $state<{ id: string; url: string } | null>(null);
	let label = $state('');
	let people = $state(true);
	let withTeams = $state(true);
	// svelte-ignore state_referenced_locally
	let identify = $state(scope === 'live_poll' ? 'nickname' : 'anonymous');
	let expires = $state('');
	let busy = $state(false);

	const urlFor = (token: string) => location.origin + '/live/' + token;
	const load = () => links.load();
	$effect(() => {
		if (targetId) load().catch(() => {});
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		try {
			const views = [...(people ? ['leaderboard'] : []), ...(withTeams && teams ? ['teams'] : [])];
			const l = await api.post<Link>('/api/teacher/share-links', { scope, target_id: targetId, views, identify, label, expires_at: expires ? new Date(expires).toISOString() : null });
			fresh = { id: l.id, url: urlFor(l.token!) };
			label = '';
			await load();
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not create the link', 'error');
		} finally {
			busy = false;
		}
	}
	async function revoke(l: Link) {
		if (!(await confirmDialog({ title: 'Turn off this link?', body: 'Anyone who has it will see "This link is no longer available".', confirm: 'Turn off', danger: true }))) return;
		await api.del('/api/teacher/share-links/' + l.id);
		if (fresh?.id === l.id) fresh = null;
		toast('Link turned off');
		await load();
	}
	async function regenerate(l: Link) {
		const n = await api.post<Link>('/api/teacher/share-links/' + l.id + '/regenerate');
		fresh = { id: n.id, url: urlFor(n.token!) };
		toast('New link made; the old one stopped working');
		await load();
	}
	async function copy(url: string) {
		try {
			await navigator.clipboard.writeText(url);
			toast('Link copied');
		} catch {
			toast('Copy failed; select the link instead', 'warning');
		}
	}
	const shows = (l: Link) => [l.views.includes('leaderboard') ? 'leaderboard' : '', l.views.includes('teams') ? 'teams' : ''].filter(Boolean).join(' + ');
	const who = (l: Link) => (l.identify === 'nickname' ? 'nicknames' : l.identify === 'student_id' ? 'student IDs' : poll ? '"Participant N"' : '"Student N"');
</script>

<section class="vstack" aria-labelledby="ll-h">
	<div>
		<h2 id="ll-h" class="m-0 flex items-center gap-2 text-lg"><Icon name="share" size={18} />Live leaderboard link</h2>
		<p class="small muted m-0">Anyone with the link sees the rankings update live, without logging in. Names and emails never appear. Search engines are asked not to index it.</p>
	</div>
	<form class="vstack" onsubmit={create}>
		<div class="flex flex-wrap gap-x-5 gap-y-2">
			<label class="m-0 flex items-center gap-2 font-normal"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={people} />{poll ? 'Participants' : 'Students'}</label>
			{#if teams}<label class="m-0 flex items-center gap-2 font-normal"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={withTeams} />{poll ? 'Groups' : 'Teams'}</label>{/if}
		</div>
		{#if people}
			<fieldset class="m-0 border-0 p-0">
				<legend class="small font-semibold mb-1">Show {poll ? 'participants' : 'students'} as</legend>
				<div class="flex flex-wrap gap-x-5 gap-y-1">
					{#if poll}
						<label class="m-0 flex items-center gap-2 font-normal"><input type="radio" class="radio radio-sm" name="ll-id" value="nickname" bind:group={identify} />Their nicknames</label>
					{:else}
						<label class="m-0 flex items-center gap-2 font-normal"><input type="radio" class="radio radio-sm" name="ll-id" value="student_id" bind:group={identify} />Classroom student IDs</label>
					{/if}
					<label class="m-0 flex items-center gap-2 font-normal"><input type="radio" class="radio radio-sm" name="ll-id" value="anonymous" bind:group={identify} />{poll ? '"Participant 1, 2…"' : '"Student 1, 2…"'}</label>
				</div>
			</fieldset>
		{/if}
		<div class="flex flex-wrap items-end gap-2">
			<div class="min-w-48 flex-1"><label for="ll-label">Caption (optional)</label><input id="ll-label" class="input input-sm w-full" maxlength="80" bind:value={label} placeholder="e.g. Year 9 science fair" /></div>
			<div><label for="ll-exp">Expires (optional)</label><input id="ll-exp" class="input input-sm" type="datetime-local" bind:value={expires} /></div>
			<button class="btn btn-sm btn-primary" disabled={busy || (!people && !(withTeams && teams))}>Create link</button>
		</div>
	</form>

	{#if fresh}
		<div class="fresh" in:flyIn>
			<div class="qr"><QrCode text={fresh.url} size={132} /></div>
			<div class="vstack min-w-0 flex-1">
				<p class="small m-0"><strong>Copy it now:</strong> for safety the full link is only shown once.</p>
				<div class="join w-full">
					<input class="input input-sm join-item w-full" readonly value={fresh.url} aria-label="Live link" onfocus={(e) => e.currentTarget.select()} />
					<button class="btn btn-sm join-item" onclick={() => copy(fresh!.url)}>Copy</button>
				</div>
				<a class="btn btn-sm w-fit" href={fresh.url} target="_blank" rel="noopener"><Icon name="monitor" size={14} />Open</a>
			</div>
		</div>
	{/if}

	{#if links.total}
		<ul class="links">
			{#each links.rows as l (l.id)}
				<li class="link" class:off={!!l.revoked_at}>
					<span class="min-w-0 flex-1">
						<span class="block truncate font-semibold">{l.label || shows(l)}</span>
						<span class="small muted">{shows(l)} · {who(l)} · {l.expires_at ? 'until ' + new Date(l.expires_at).toLocaleString() : 'no expiry'}</span>
					</span>
					{#if l.revoked_at}<span class="badge badge-soft badge-sm">off</span>{:else}<span class="badge badge-soft badge-success badge-sm">on</span>{/if}
					{#if !l.revoked_at}<button class="btn btn-ghost btn-xs" onclick={() => revoke(l)}>Turn off</button>{/if}
					<button class="btn btn-ghost btn-xs" onclick={() => regenerate(l)}>New link</button>
				</li>
			{/each}
		</ul>
		<Pager list={links} label="Live links" />
	{/if}
</section>

<style>
	.fresh { display: flex; gap: 1rem; align-items: center; padding: 0.85rem; border-radius: var(--radius-box); background: color-mix(in oklab, var(--color-success) 9%, var(--color-base-100)); border: 1px solid color-mix(in oklab, var(--color-success) 35%, var(--color-base-300)); }
	@media (max-width: 520px) { .fresh { flex-direction: column; align-items: stretch; } .qr { align-self: center; } }
	.qr { background: #fff; padding: 0.4rem; border-radius: 0.6rem; flex: none; }
	.links { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.3rem; }
	.link { display: flex; align-items: center; gap: 0.5rem; padding: 0.45rem 0.6rem; border-radius: var(--radius-box); border: 1px solid var(--color-base-300); }
	.link.off { opacity: 0.6; }
</style>
