<script lang="ts">
	// Teams in a live session (V2-06, V2-07, D-44): form them, move students,
	// pick captains, and follow the standings as members finish.
	import { api, ApiError } from './api';
	import Icon from './ui/Icon.svelte';
	import { confirmDialog } from './ui/dialog.svelte';
	import { flyIn } from './ui/motion';
	import { toast } from './ui/toast.svelte';
	import type { TeamMember, TeamStanding, TeamsView } from './types';

	let { sessionId, version = 0, ended = false }: { sessionId: string; version?: number; ended?: boolean } = $props();

	let view = $state<TeamsView | null>(null);
	let standings = $state<TeamStanding[]>([]);
	let count = $state(4);
	let reassign = $state(false);
	let busy = $state(false);

	async function load() {
		try {
			const [v, s] = await Promise.all([
				api.get<TeamsView>('/api/teacher/sessions/' + sessionId + '/teams'),
				api.get<{ teams: TeamStanding[] }>('/api/teacher/sessions/' + sessionId + '/teams/standings')
			]);
			view = v;
			standings = s.teams;
		} catch {
			/* the next dashboard update tries again */
		}
	}
	// The dashboard changes whenever someone joins, moves or finishes.
	let pending: ReturnType<typeof setTimeout> | null = null;
	$effect(() => {
		void version;
		if (pending) return;
		pending = setTimeout(() => {
			pending = null;
			load();
		}, view ? 1500 : 0);
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
	const base = $derived('/api/teacher/sessions/' + sessionId + '/teams');
	const generate = (from: 'random' | 'categories') => run(() => api.post(base + '/generate', { from, count: Number(count), reassign }), from === 'random' ? 'Teams shuffled' : 'Teams made from categories');
	const add = () => run(() => api.post(base, {}), 'Team added');
	const move = (m: TeamMember, team: string) => run(() => api.post(base + '/members', { attempt_ids: [m.attempt_id], team_id: team }));
	const captain = (m: TeamMember) => run(() => api.post(base + '/members', { attempt_ids: [m.attempt_id], captain: !m.captain }));
	async function rename(id: string, name: string) {
		const n = prompt('Team name', name);
		if (n?.trim() && n !== name) await run(() => api.patch('/api/teacher/session-teams/' + id, { name: n }));
	}
	async function remove(id: string, name: string) {
		if (await confirmDialog({ title: `Delete ${name}?`, body: 'Its members carry on with the quiz without a team. Their marks are kept.', confirm: 'Delete team', danger: true }))
			await run(() => api.del('/api/teacher/session-teams/' + id), 'Team deleted');
	}
	const RULE: Record<string, string> = {
		all: "every member's mark counts", first: "each question's first answer in the team counts", captain: "the captain's marks count", best: "the team's best mark on each question counts"
	};
	const CALC: Record<string, string> = { sum: 'added up', average: 'averaged over all members', max: 'highest', min: 'lowest (0 if someone skips)' };
	const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));
</script>

{#snippet member(m: TeamMember, teamId: string)}
	<li class="member">
		<span class="min-w-0 flex-1"><span class="block truncate">{m.name}</span>{#if m.student_number}<span class="small muted">{m.student_number}</span>{/if}</span>
		{#if teamId}
			<button type="button" class="btn btn-ghost btn-xs btn-square" class:text-warning={m.captain} aria-pressed={m.captain} aria-label={m.captain ? `${m.name} is captain` : `Make ${m.name} captain`} title={m.captain ? 'Captain' : 'Make captain'} disabled={busy || ended} onclick={() => captain(m)}><Icon name="star" size={14} /></button>
		{/if}
		<select class="select select-xs w-28" aria-label="Move {m.name} to" value={teamId} disabled={busy || ended} onchange={(e) => move(m, e.currentTarget.value)}>
			<option value="">No team</option>
			{#each view?.teams ?? [] as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
		</select>
	</li>
{/snippet}

<section class="card card-border bg-base-100 p-4 shadow-sm sm:p-6 vstack" aria-labelledby="teams-h">
	<div class="flex flex-wrap items-center gap-2">
		<h2 id="teams-h" class="m-0 flex items-center gap-2 text-lg"><Icon name="users" size={18} />Teams</h2>
		{#if view}<span class="small muted">{RULE[view.acceptance]}{view.acceptance === 'all' ? `, ${CALC[view.calc]}` : ''}</span>{/if}
	</div>

	{#if standings.length}
		<ol class="standings" aria-label="Team standings">
			{#each standings as s (s.id)}
				<li class="stand" in:flyIn>
					<span class="pos tabular" class:m1={s.rank === 1}>{s.rank}</span>
					<span class="min-w-0">
						<span class="flex items-center gap-2 font-semibold"><span class="dot" style:background="var(--cat-{s.color})" aria-hidden="true"></span><span class="truncate">{s.name}</span></span>
						<span class="track" aria-hidden="true"><span class="fill" style:width="{s.pct}%"></span></span>
					</span>
					<span class="text-right tabular">
						<strong>{fmt(s.pct)}%</strong>
						<span class="small muted block">{fmt(s.score)}/{fmt(s.max_score)} · {s.finished}/{s.members} done{#if !s.complete} · <span class="text-warning">marking</span>{/if}</span>
					</span>
				</li>
			{/each}
		</ol>
		<p class="small muted m-0">Marks arrive as each member finishes. Teams are ranked by percentage, so team size doesn't decide the winner.</p>
	{/if}

	{#if !ended}
		<div class="flex flex-wrap items-end gap-2">
			<div class="w-20"><label for="st-n">Teams</label><input id="st-n" class="input input-sm w-full" type="number" min="2" max="50" bind:value={count} /></div>
			<button class="btn btn-sm btn-primary" disabled={busy} onclick={() => generate('random')}><Icon name="shuffle" size={14} />Random teams</button>
			<button class="btn btn-sm" disabled={busy} onclick={() => generate('categories')}>From categories</button>
			<button class="btn btn-sm" disabled={busy} onclick={add}><Icon name="plus" size={14} />Add a team</button>
			<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={reassign} />Also move students who have a team</label>
		</div>
	{/if}

	{#if view}
		<div class="teams">
			{#each view.teams as t (t.id)}
				<div class="team" aria-label={t.name}>
					<div class="flex items-center gap-2">
						<span class="dot" style:background="var(--cat-{t.color})" aria-hidden="true"></span>
						<strong class="min-w-0 flex-1 truncate">{t.name}</strong>
						<span class="badge badge-soft badge-sm tabular">{t.member_list.length}</span>
						{#if !ended}
							<button class="btn btn-ghost btn-xs btn-square" aria-label="Rename {t.name}" onclick={() => rename(t.id, t.name)}><Icon name="pencil" size={13} /></button>
							<button class="btn btn-ghost btn-xs btn-square text-error" aria-label="Delete {t.name}" onclick={() => remove(t.id, t.name)}><Icon name="x" size={13} /></button>
						{/if}
					</div>
					<ul class="members">{#each t.member_list as m (m.attempt_id)}{@render member(m, t.id)}{:else}<li class="small muted py-1">Nobody yet</li>{/each}</ul>
				</div>
			{/each}
		</div>
		{#if !view.teams.length}<p class="small muted m-0">No teams yet.{view.mode === 'self' ? ' Students choose from your teams when they join, so add some first.' : ''}</p>{/if}
		{#if view.unassigned.length}
			<div class="team">
				<strong>No team <span class="badge badge-soft badge-sm tabular">{view.unassigned.length}</span></strong>
				<ul class="members">{#each view.unassigned as m (m.attempt_id)}{@render member(m, '')}{/each}</ul>
			</div>
		{/if}
	{/if}
</section>

<style>
	.teams { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(min(15rem, 100%), 1fr)); }
	.team { padding: 0.65rem 0.75rem; border-radius: var(--radius-box); border: 1px solid var(--color-base-300); }
	.dot { width: 0.8rem; height: 0.8rem; border-radius: 999px; flex: none; display: inline-block; }
	.members { list-style: none; margin: 0.4rem 0 0; padding: 0; }
	.member { display: flex; align-items: center; gap: 0.4rem; padding: 0.2rem 0; border-top: 1px solid var(--color-base-200); }
	.standings { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
	.stand { display: grid; grid-template-columns: 2rem minmax(0, 1fr) auto; gap: 0.75rem; align-items: center; padding: 0.45rem 0.7rem; border-radius: var(--radius-box); background: var(--color-base-200); }
	.pos { width: 2rem; height: 2rem; border-radius: 999px; display: grid; place-items: center; font-weight: 750; background: var(--color-base-100); }
	.pos.m1 { background: #f5c542; color: #3d2f00; }
	.track { display: block; height: 0.3rem; margin-top: 0.3rem; border-radius: 999px; background: var(--color-base-100); overflow: hidden; }
	.fill { display: block; height: 100%; background: var(--color-primary); transition: width 600ms var(--ease-out); }
	@media (prefers-reduced-motion: reduce) { .fill { transition: none; } }
</style>
