<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { formatDuration } from '$lib/clock';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import SessionTeams from '$lib/SessionTeams.svelte';
	import Avatar from '$lib/ui/Avatar.svelte';
	import LiveLinks from '$lib/LiveLinks.svelte';
	import { LiveSocket } from '$lib/socket';
	import { STATE_BADGE, STATE_ICON, STATE_LABEL, type Dashboard } from '$lib/types';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import StatCounter from '$lib/ui/StatCounter.svelte';
	import { confirmDialog, promptDialog } from '$lib/ui/dialog.svelte';
	import { fadeIn, flyIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';

	type Alert = { attempt_id: string; student_name: string; student_number: string | null; kind: string; action: string; at: number };
	const ready = requireRole('teacher');
	const id = $derived(page.params.id ?? '');
	let d = $state<Dashboard | null>(null);
	let alerts = $state<Alert[]>([]);
	let selected = $state<Set<string>>(new Set());
	let connected = $state(false);
	let notice = $state('');
	let now = $state(Date.now());
	let extendMin = $state(5);
	let countdown = $state<number | ''>('');
	let admission = $state('');
	let startMode = $state('');
	let teamMode = $state('');
	let teamAcceptance = $state('');
	let teamCalc = $state('');
	let version = $state(0);
	let socket: LiveSocket;
	let timer: ReturnType<typeof setInterval>;

	onMount(() => {
		socket = new LiveSocket('/ws/sessions/' + id);
		socket.onStatus = (c) => (connected = c);
		socket.onMessage = (m) => {
			if (m.type === 'dashboard') {
				d = m as unknown as Dashboard;
				version++;
			}
			else if (m.type === 'alert') {
				const a = { ...(m as unknown as Alert), at: Date.now() };
				alerts = [a, ...alerts].slice(0, 50);
				toast(`${a.student_name}: ${a.kind.replace('_', ' ')} → ${a.action}`, a.action === 'invalidated' ? 'error' : 'warning');
				if ('vibrate' in navigator) navigator.vibrate(200);
			}
		};
		timer = setInterval(() => (now = Date.now()), 500);
	});
	$effect(() => {
		if (ready() && id && socket && !socket.connected) socket.open();
	});
	$effect(() => {
		if (d && countdown === '') {
			countdown = (d.session.settings.countdown_seconds as number) ?? '';
			admission = (d.session.settings.admission_mode as string) ?? '';
			startMode = (d.session.settings.start_mode as string) ?? '';
			teamMode = (d.session.settings.team_mode as string) ?? '';
			teamAcceptance = (d.session.settings.team_acceptance as string) ?? '';
			teamCalc = (d.session.settings.team_calc as string) ?? '';
		}
	});
	onDestroy(() => {
		socket?.close();
		clearInterval(timer);
	});

	const rows = $derived(d?.rows ?? []);
	const ended = $derived(d?.session.status === 'ended');
	function toggle(id: string) {
		const s = new Set(selected);
		if (s.has(id)) s.delete(id);
		else s.add(id);
		selected = s;
	}
	function selectState(state: string) {
		selected = new Set(rows.filter((r) => r.state === state).map((r) => r.attempt_id));
	}
	async function cmd(action: string, all: boolean, extra: Record<string, unknown> = {}) {
		notice = '';
		try {
			const r = await api.post<{ affected: number }>('/api/teacher/sessions/' + id + '/' + action, { all, attempt_ids: all ? [] : [...selected], ...extra });
			const verb: Record<string, string> = { admit: 'Admitted', pause: 'Paused', resume: 'Resumed', extend: 'Extended time for', start: 'Started' };
			toast(`${verb[action] ?? action} ${r.affected} student${r.affected === 1 ? '' : 's'}`, r.affected ? 'success' : 'info');
			if (!all) selected = new Set();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Command failed', 'error');
		}
	}
	async function end() {
		const ok = await confirmDialog({ title: 'End the session now?', body: 'Running attempts are submitted as they are, and nobody else can join.', confirm: 'End session', danger: true });
		if (!ok) return;
		await api.post('/api/teacher/sessions/' + id + '/end');
		toast('Session ended');
	}
	async function reinstate(attemptId: string) {
		const reason = await promptDialog({ title: 'Reinstate this attempt?', body: 'The student can carry on. Your reason is logged with the attempt.', label: 'Reason', confirm: 'Reinstate', multiline: true, maxlength: 500 });
		if (!reason) return;
		try {
			await api.post('/api/teacher/attempts/' + attemptId + '/reinstate', { reason });
			toast('Attempt reinstated');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Could not reinstate', 'error');
		}
	}
	async function saveSessionSettings() {
		const o: Record<string, unknown> = { ...(d?.session.settings ?? {}) };
		if (countdown === '') delete o.countdown_seconds;
		else o.countdown_seconds = Number(countdown);
		if (admission) o.admission_mode = admission;
		else delete o.admission_mode;
		if (startMode) o.start_mode = startMode;
		else delete o.start_mode;
		for (const [k, v] of [['team_mode', teamMode], ['team_acceptance', teamAcceptance], ['team_calc', teamCalc]]) {
			if (v) o[k] = v;
			else delete o[k];
		}
		await api.put('/api/teacher/sessions/' + id + '/settings', o);
		toast('Session settings saved');
	}
	const teamsOn = $derived(!!d?.team_mode && d.team_mode !== 'off');
	// Admitted students either wait for the teacher (no countdown, D-53) or count down.
	const readyCount = $derived(rows.filter((r) => r.state === 'admitted' && r.countdown_deadline == null).length);
	const counting = $derived(rows.filter((r) => r.state === 'admitted' && r.countdown_deadline != null).length);
	const selectedReady = $derived(rows.some((r) => selected.has(r.attempt_id) && r.state === 'admitted'));
	function countdownLeft(r: Dashboard['rows'][number]) {
		void now;
		return r.countdown_deadline != null ? Math.max(0, r.countdown_deadline - socket.clock.serverNow()) : null;
	}
	const total = $derived(rows.length);
	const flagged = $derived(rows.filter((r) => r.violations > 0 || r.state === 'invalidated').length);
	const finished = $derived((d?.counts.submitted ?? 0) + (d?.counts.invalidated ?? 0));
	function left(r: Dashboard['rows'][number]) {
		void now;
		if (r.remaining_ms != null) return r.remaining_ms;
		return r.quiz_deadline ? Math.max(0, r.quiz_deadline - socket.clock.serverNow()) : null;
	}
</script>

<div class="page-container vstack" style="max-width:1300px">
	{#if d}
		<p class="small"><a href={'/t/quizzes/' + d.session.quiz_id}>← Quiz</a></p>
			<div class="row">
				<h1 class="m-0">{d.session.title}</h1>
				<span class="badge badge-soft gap-1 {d.session.status === 'live' ? 'ok' : ''}">{#if d.session.status === 'live'}<span class="status status-success animate-pulse" aria-hidden="true"></span>{/if}{d.session.status}</span>
				{#if !connected}<span class="badge badge-soft badge-warning gap-1"><Icon name="wifi-off" size={13} />reconnecting…</span>{/if}
				<span class="spacer"></span>
				<a class="btn" href={'/t/sessions/' + id + '/projector'} target="_blank"><Icon name="qr" size={16} />Show QR full screen</a>
				<a class="btn" href={'/t/sessions/' + id + '/results'}><Icon name="chart" size={16} />Results</a>
				{#if !ended}<button class="btn btn-error btn-outline" onclick={end}>End session</button>{/if}
			</div>

			<div class="stats-grid" role="list" aria-label="Session counts">
				<StatCounter label="Joined" value={total} icon="users" />
				<StatCounter label="Waiting" value={d.counts.waiting ?? 0} icon="clock" tone={d.counts.waiting ? 'warning' : ''} />
				<StatCounter label="In progress" value={(d.counts.in_progress ?? 0) + (d.counts.admitted ?? 0)} icon="play" tone="primary" />
				<StatCounter label="Finished" value={finished} icon="check-circle" tone="success" sub={total ? `${Math.round((finished / total) * 100)}%` : ''} />
				<StatCounter label="Flagged" value={flagged} icon="flag" tone={flagged ? 'error' : ''} />
			</div>

		<div class="layout">
			<aside class="vstack">
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" style="text-align:center">
					<QrCode text={d.session.join_url} size={220} />
					<p style="font-size:1.6rem;font-weight:700;letter-spacing:0.1em;margin:0">{d.session.join_code}</p>
					<p class="small" style="word-break:break-all">{d.session.join_url}</p>
				</div>
				{#if !ended}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<strong>Session settings</strong>
						<div><label for="cd">Countdown (seconds)</label><input class="input w-full" id="cd" type="number" min="0" bind:value={countdown} placeholder="inherit" /></div>
						<div><label for="adm">Admission</label><select class="select w-full" id="adm" bind:value={admission}><option value="">Inherit</option><option value="manual">I admit students</option><option value="auto">Admit automatically</option></select></div>
						<div><label for="sm">Quiz start</label><select class="select w-full" id="sm" bind:value={startMode}><option value="">Inherit</option><option value="countdown">Countdown starts when admitted</option><option value="teacher">I press Start</option></select>
							<p class="small muted m-0 mt-1">{(startMode || d.start_mode) === 'teacher' ? 'Admitted students wait until you press Start, then the countdown runs.' : 'Each student’s countdown starts as soon as they’re admitted.'}</p></div>
						<div><label for="tm">Teams</label><select class="select w-full" id="tm" bind:value={teamMode}><option value="">Inherit</option><option value="off">No teams</option><option value="manual">I put students in teams</option><option value="random">At random as they join</option><option value="categories">From classroom categories</option><option value="self">Students choose</option></select></div>
						{#if (teamMode || teamsOn) && teamMode !== 'off'}
							<div><label for="ta">Team marks count</label><select class="select w-full" id="ta" bind:value={teamAcceptance}><option value="">Inherit</option><option value="all">Every member's marks</option><option value="first">First answer per question</option><option value="captain">Captain's marks</option><option value="best">Best mark per question</option></select></div>
							{#if (teamAcceptance || 'all') === 'all'}
								<div><label for="tc">Combine members by</label><select class="select w-full" id="tc" bind:value={teamCalc}><option value="">Inherit</option><option value="sum">Total</option><option value="average">Average (skips count as 0)</option><option value="max">Highest</option><option value="min">Lowest</option></select></div>
							{/if}
						{/if}
						<button class="btn btn-sm" onclick={saveSessionSettings}>Save</button>
					</div>
				{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<strong class="flex items-center gap-2"><Icon name="flag" size={16} />Integrity alerts</strong>
						{#each alerts as a (a.at + a.attempt_id)}
							<div class="small alert alert-soft {a.action === 'invalidated' ? 'danger' : ''}" in:flyIn>
							<strong>{a.student_name}</strong>{a.student_number ? ` (${a.student_number})` : ''}: {a.kind.replace('_', ' ')} → {a.action}
							<br /><span class="muted">{new Date(a.at).toLocaleTimeString()}</span>
						</div>
					{:else}<p class="small muted">No violations so far.</p>{/each}
				</div>
			</aside>

			<section class="vstack">
				{#if !ended}
						<div class="card card-border command-bar bg-base-100 shadow-sm p-3 sm:p-4 row">
						<button class="btn btn-primary" onclick={() => cmd('admit', true)} disabled={!d.counts.waiting}>Admit all waiting ({d.counts.waiting ?? 0})</button>
						<button class="btn" onclick={() => cmd('admit', false)} disabled={!selected.size}>Admit selected</button>
						{#if d.start_mode === 'teacher' || readyCount}
							<button class="btn btn-success" onclick={() => cmd('start', true)} disabled={!readyCount} title="Starts the countdown for every admitted student; students admitted later count down by themselves">
								<Icon name="play" size={16} />Start quiz{readyCount ? ` (${readyCount} ready)` : ''}</button>
						{/if}
						{#if selectedReady}<button class="btn" onclick={() => cmd('start', false)}><Icon name="play" size={16} />Start selected</button>{/if}
						{#if counting || readyCount}
							<button class="btn" onclick={() => cmd('start', selected.size === 0, { now: true })} title="Skips the rest of the countdown"><Icon name="skip-forward" size={16} />Start now{selected.size ? ' (selected)' : ''}</button>
						{/if}
						<button class="btn" onclick={() => cmd('pause', selected.size === 0)}>Pause {selected.size ? 'selected' : 'all'}</button>
						<button class="btn" onclick={() => cmd('resume', selected.size === 0)}>Resume {selected.size ? 'selected' : 'all'}</button>
						<span class="row"><input class="input w-full" type="number" min="1" style="width:5rem" bind:value={extendMin} aria-label="Minutes" />
							<button class="btn" onclick={() => cmd('extend', selected.size === 0, { seconds: extendMin * 60 })}>+ min {selected.size ? 'selected' : 'everyone'}</button></span>
					</div>
						<div class="row small">
							<span class="muted">Select:</span> <button class="btn btn-sm" onclick={() => selectState('waiting')}>waiting</button>
						<button class="btn btn-sm" onclick={() => selectState('admitted')}>admitted</button>
						<button class="btn btn-sm" onclick={() => selectState('in_progress')}>in progress</button>
						<button class="btn btn-sm" onclick={() => selectState('paused')}>paused</button>
						<button class="btn btn-sm" onclick={() => (selected = new Set())}>none</button>
					</div>
				{/if}
				{#if notice}<p class="alert alert-soft alert-warning small">{notice}</p>{/if}
					{#if selected.size}<p class="small m-0" in:fadeIn><strong>{selected.size}</strong> selected · commands apply to them</p>{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table">
						<thead><tr><th></th><th>Student</th>{#if teamsOn}<th>Team</th>{/if}<th>Status</th><th>Progress</th><th>Time left</th><th>Flags</th><th></th></tr></thead>
						<tbody>
							{#each rows as r (r.attempt_id)}
									<tr class:sel={selected.has(r.attempt_id)} in:fadeIn>
									<td><input class="checkbox" type="checkbox" checked={selected.has(r.attempt_id)} onchange={() => toggle(r.attempt_id)} aria-label={'Select ' + r.name} /></td>
									<td><span class="flex items-center gap-2"><Avatar id={r.user_id} name={r.name} avatar={r.avatar} size={30} /><span class="min-w-0">{r.name}<br /><span class="small muted">{r.student_number ?? ''}</span></span></span></td>
									{#if teamsOn}
										{@const team = d.teams?.find((t) => t.id === r.team_id)}
										<td class="small">{#if team}<span class="inline-flex items-center gap-1"><span class="tdot" style:background="var(--cat-{team.color})" aria-hidden="true"></span>{team.name}{#if r.captain}<Icon name="star" size={12} /><span class="sr-only">(captain)</span>{/if}</span>{:else}<span class="muted">—</span>{/if}</td>
									{/if}
									<td><span class="badge badge-soft {STATE_BADGE[r.state] ?? ''}"><span aria-hidden="true">{STATE_ICON[r.state] ?? ''}</span> {r.state === 'admitted' ? (r.countdown_deadline == null ? 'Ready' : `Starting in ${formatDuration(countdownLeft(r))}`) : STATE_LABEL[r.state]}</span>{#if !r.connected && (r.state === 'in_progress' || r.state === 'waiting')}<br /><span class="small muted">offline</span>{/if}</td>
									<td class="small min-w-32">
										<div class="flex items-center gap-2"><progress class="progress progress-primary w-20" value={r.answered} max={r.total || 1} aria-hidden="true"></progress><span class="tabular">{r.answered}/{r.total}</span></div>
										{#if r.state === 'in_progress'}<span class="muted">on Q{r.index + 1}</span>{/if}
									</td>
									<td class="small tabular" class:text-error={(left(r) ?? Infinity) < 60_000 && r.state === 'in_progress'}>{formatDuration(left(r))}{#if r.extension_sec}<br /><span class="muted">+{Math.round(r.extension_sec / 60)} min</span>{/if}</td>
									<td class="small">{#if r.violations}<span class="badge badge-soft badge-error gap-1"><Icon name="flag" size={12} />{r.violations} violation{r.violations > 1 ? 's' : ''}</span>{/if}{#if r.invalid_reason}<br />{r.invalid_reason}{/if}</td>
									<td>{#if r.state === 'invalidated' && !ended}<button class="btn btn-sm" onclick={() => reinstate(r.attempt_id)}>Reinstate</button>{/if}</td>
								</tr>
							{:else}<tr><td colspan="8"><div class="muted py-6 text-center"><span class="loading loading-dots loading-md text-primary"></span><br />Waiting for students to scan the QR code…</div></td></tr>{/each}
						</tbody>
					</table>
				</div>
				{#if teamsOn}<SessionTeams sessionId={id} {version} {ended} />{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><LiveLinks scope="live_session" targetId={id} teams={teamsOn} /></div>
			</section>
		</div>
	{:else}
		<Skeleton lines={5} />
	{/if}
</div>

<style>
	.layout { display: grid; gap: 1rem; grid-template-columns: 280px minmax(0, 1fr); align-items: start; }
	@media (max-width: 900px) { .layout { grid-template-columns: minmax(0, 1fr); } .layout > section { order: -1; } }
	.tdot { width: 0.65rem; height: 0.65rem; border-radius: 999px; display: inline-block; flex: none; }
	tr.sel { background: color-mix(in oklab, var(--color-primary) 8%, transparent); }
	.stats-grid { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fit, minmax(9.5rem, 1fr)); }
	.command-bar { position: sticky; top: 4.25rem; z-index: 4; }
</style>
