<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	import { SETTINGS } from '$lib/settingsMeta';
	import OptionGroup from '$lib/ui/OptionGroup.svelte';
	import { count } from '$lib/words';
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { formatDuration } from '$lib/clock';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { findRows, hiddenSelected, ROW_SHOW, type RowShow } from '$lib/live/findRows';
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
	// Finding students (D-155). Results keep arriving; the search stays.
	let rowSearch = $state('');
	let rowShow = $state<RowShow>('');
	const teamName = (r: { team_id?: string }) => d?.teams?.find((t) => t.id === r.team_id)?.name ?? '';
	const shownRows = $derived(findRows(rows, rowSearch, rowShow, teamName));
	const searching = $derived(!!rowSearch.trim() || !!rowShow);
	const selectedHidden = $derived(hiddenSelected(selected, shownRows));
	function toggle(id: string) {
		const s = new Set(selected);
		if (s.has(id)) s.delete(id);
		else s.add(id);
		selected = s;
	}
	function selectState(state: string) {
		// Only those on the list: nobody the search is hiding gets picked unseen.
		selected = new Set(shownRows.filter((r) => r.state === state).map((r) => r.attempt_id));
	}
	async function cmd(action: string, all: boolean, extra: Record<string, unknown> = {}) {
		notice = '';
		try {
			const r = await api.post<{ affected: number }>('/api/teacher/sessions/' + id + '/' + action, { all, attempt_ids: all ? [] : [...selected], ...extra });
			const verb: Record<string, string> = { admit: 'Admitted', pause: 'Paused', resume: 'Resumed', extend: 'Extended time for', start: 'Started', retry: 'Started a new try for' };
			toast(tr("{0} {1} student{2}", [verb[action] ?? action, r.affected, r.affected === 1 ? '' : 's']), r.affected ? 'success' : 'info');
			if (!all) selected = new Set();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("Command failed"), 'error');
		}
	}
	// Let students who finished take the quiz again from the start (D-131).
	const finishedCount = $derived(rows.filter((r) => r.state === 'submitted' || r.state === 'invalidated').length);
	const selectedFinished = $derived(rows.filter((r) => selected.has(r.attempt_id) && (r.state === 'submitted' || r.state === 'invalidated')).length);
	async function letRetry(all: boolean) {
		const n = all ? finishedCount : selectedFinished;
		const ok = await confirmDialog({
			title: tr("Let {0} {1} retry?", [n, n === 1 ? 'student' : 'students']),
			body: tr("They start again from the first question, with the questions drawn and ordered afresh. Their earlier try is kept, and the best try counts."),
			confirm: tr("Let them retry")
		});
		if (ok) cmd('retry', all);
	}
	async function end() {
		const ok = await confirmDialog({ title: tr("End the session now?"), body: tr("Running attempts are submitted as they are, and nobody else can join."), confirm: tr("End session"), danger: true });
		if (!ok) return;
		await api.post('/api/teacher/sessions/' + id + '/end');
		toast(tr("Session ended"));
	}
	async function reinstate(attemptId: string) {
		const reason = await promptDialog({ title: tr("Reinstate this attempt?"), body: tr("The student can carry on. Your reason is logged with the attempt."), label: tr("Reason"), confirm: tr("Reinstate"), multiline: true, maxlength: 500 });
		if (!reason) return;
		try {
			await api.post('/api/teacher/attempts/' + attemptId + '/reinstate', { reason });
			toast(tr("Attempt reinstated"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("Could not reinstate"), 'error');
		}
	}
	// A student who handed in by mistake carries on where they were (D-139).
	async function reopen(attemptId: string) {
		const reason = await promptDialog({ title: tr("Reopen this attempt?"), body: tr("The student carries on with their answers and the time they had left, which starts running now. Any marks are cleared and given again when they hand in. If results were already showing, they may have seen them. Your reason is logged with the attempt."), label: tr("Reason"), confirm: tr("Reopen"), multiline: true, maxlength: 500 });
		if (!reason) return;
		try {
			await api.post('/api/teacher/attempts/' + attemptId + '/reopen', { reason });
			toast(tr("Attempt reopened"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("Could not reopen"), 'error');
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
		toast(tr("Session settings saved"));
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
		<p class="small"><a href={'/t/quizzes/' + d.session.quiz_id}>{tr("← Quiz")}</a></p>
			<div class="row">
				<h1 class="m-0">{d.session.title}</h1>
				<span class="badge badge-soft gap-1 {d.session.status === 'live' ? 'ok' : ''}">{#if d.session.status === 'live'}<span class="status status-success animate-pulse" aria-hidden="true"></span>{/if}{d.session.status === 'live' ? tr("Live") : d.session.status === 'ended' ? tr("Ended") : d.session.status}</span>
				{#if !connected}<span class="badge badge-soft badge-warning gap-1"><Icon name="wifi-off" size={13} />{tr("reconnecting…")}</span>{/if}
				<span class="spacer"></span>
				{#if !ended}<a class="btn" href={'/t/sessions/' + id + '/projector'} target="_blank"><Icon name="qr" size={16} />{tr("Show QR full screen")}</a>{/if}
				<a class="btn" class:btn-primary={ended} href={'/t/sessions/' + id + '/results'}><Icon name="chart" size={16} />{tr("Results")}</a>
				{#if d.session.classroom_id}<a class="btn" href={`/t/classrooms/${d.session.classroom_id}?tab=report&scope=session:${id}`}><Icon name="table" size={16} />{tr("Class report")}</a>{/if}
				{#if !ended}<button class="btn btn-error btn-outline" onclick={end}>{tr("End session")}</button>{/if}
			</div>

			<div class="stats-grid" role="list" aria-label={tr("Session counts")}>
				<StatCounter label={tr("Joined")} value={total} icon="users" />
				<StatCounter label={tr("Waiting")} value={d.counts.waiting ?? 0} icon="clock" tone={d.counts.waiting ? 'warning' : ''} />
				<StatCounter label={tr("In progress")} value={(d.counts.in_progress ?? 0) + (d.counts.admitted ?? 0)} icon="play" tone="primary" />
				<StatCounter label={tr("Finished")} value={finished} icon="check-circle" tone="success" sub={total ? `${Math.round((finished / total) * 100)}%` : ''} />
				<StatCounter label={tr("Flagged")} value={flagged} icon="flag" tone={flagged ? 'error' : ''} />
			</div>

		<div class="layout">
			<aside class="vstack">
				{#if ended}
					<!-- After the end the QR code would only invite students to a closed session. -->
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<strong class="flex items-center gap-2"><Icon name="check-circle" size={16} />{tr("Session ended")}</strong>
						<p class="small muted m-0">{tr("Joining is closed. {0} of {1} finished.", [finished, count(total, 'student')])}</p>
						<a class="btn btn-primary btn-sm" href={'/t/sessions/' + id + '/results'}>{tr("Mark and release results")}</a>
					</div>
				{:else}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" style="text-align:center">
						<QrCode text={d.session.join_url} size={220} />
						<p style="font-size:1.6rem;font-weight:700;letter-spacing:0.1em;margin:0">{d.session.join_code}</p>
						<p class="small" style="word-break:break-all">{d.session.join_url}</p>
					</div>
				{/if}
				{#if !ended}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<strong>{tr("Session settings")}</strong>
						<div><label for="cd">{tr("Countdown before the first question")}</label><label class="input input-sm w-36"><input id="cd" type="number" min="0" bind:value={countdown} placeholder={tr("Default")} /><span class="muted">s</span></label></div>
						{#snippet pick(key: string, label: string, val: string, setv: (v: string) => void, fallback?: string)}
							<div class="grid gap-1">
								<span class="small font-semibold">{label}</span>
								<OptionGroup {label} options={SETTINGS.find((m) => m.key === key)?.options ?? []} value={val || undefined} {fallback} onchange={setv} />
								{#if val}<button type="button" class="link link-hover small justify-self-start" onclick={() => setv('')}>{tr("Use the default")}</button>{/if}
							</div>
						{/snippet}
						{@render pick('admission_mode', 'Letting students in', admission, (v) => (admission = v))}
						{@render pick('start_mode', 'Starting the quiz', startMode, (v) => (startMode = v), d.start_mode)}
						{@render pick('team_mode', 'Teams', teamMode, (v) => (teamMode = v))}
						{#if (teamMode || teamsOn) && teamMode !== 'off'}
							{@render pick('team_acceptance', 'Which marks count', teamAcceptance, (v) => (teamAcceptance = v))}
							{#if (teamAcceptance || 'all') === 'all'}
								{@render pick('team_calc', 'Combine marks by', teamCalc, (v) => (teamCalc = v))}
							{/if}
						{/if}
						<button class="btn btn-sm" onclick={saveSessionSettings}>{tr("Save")}</button>
					</div>
				{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<strong class="flex items-center gap-2"><Icon name="flag" size={16} />{tr("Integrity alerts")}</strong>
						{#each alerts as a (a.at + a.attempt_id)}
							<div class="small alert alert-soft {a.action === 'invalidated' ? 'danger' : ''}" in:flyIn>
							<strong>{a.student_name}</strong>{a.student_number ? ` (${a.student_number})` : ''}: {a.kind.replace('_', ' ')} → {a.action}
							<br /><span class="muted">{new Date(a.at).toLocaleTimeString(localeTag())}</span>
						</div>
					{:else}<p class="small muted m-0">{flagged ? tr("{0} flagged. The Flags column shows what happened.", [count(flagged, 'student')]) : ended ? tr("No violations in this session.") : tr("No violations so far.")}</p>{/each}
				</div>
			</aside>

			<section class="vstack">
				{#if !ended}
						<div class="card card-border command-bar bg-base-100 shadow-sm p-3 sm:p-4 row">
						<button class="btn btn-primary" onclick={() => cmd('admit', true)} disabled={!d.counts.waiting}>{tr("Admit all waiting ({0})", [d.counts.waiting ?? 0])}</button>
						<button class="btn" onclick={() => cmd('admit', false)} disabled={!selected.size}>{tr("Admit selected")}</button>
						{#if d.start_mode === 'teacher' || readyCount}
							<button class="btn btn-success" onclick={() => cmd('start', true)} disabled={!readyCount} title={tr("Starts the countdown for every admitted student; students admitted later count down by themselves")}>
								<Icon name="play" size={16} />{tr("Start quiz{0}", [readyCount ? tr(" ({0} ready)", [readyCount]) : ''])}</button>
						{/if}
						{#if selectedReady}<button class="btn" onclick={() => cmd('start', false)}><Icon name="play" size={16} />{tr("Start selected")}</button>{/if}
						{#if counting || readyCount}
							<button class="btn" onclick={() => cmd('start', selected.size === 0, { now: true })} title={tr("Skips the rest of the countdown")}><Icon name="skip-forward" size={16} />{tr("Start now{0}", [selected.size ? tr(" (selected)") : ''])}</button>
						{/if}
						<button class="btn" onclick={() => cmd('pause', selected.size === 0)}>{tr("Pause {0}", [selected.size ? 'selected' : 'all'])}</button>
						<button class="btn" onclick={() => cmd('resume', selected.size === 0)}>{tr("Resume {0}", [selected.size ? 'selected' : 'all'])}</button>
						<span class="row"><input class="input w-full" type="number" min="1" style="width:5rem" bind:value={extendMin} aria-label={tr("Minutes")} />
							<button class="btn" onclick={() => cmd('extend', selected.size === 0, { seconds: extendMin * 60 })}>{tr("+ min {0}", [selected.size ? 'selected' : 'everyone'])}</button></span>
						{#if selected.size ? selectedFinished : finishedCount}
							<button class="btn" onclick={() => letRetry(selected.size === 0)} title={tr("They start again from the first question; the best try counts")}><Icon name="undo" size={16} />{tr("Let {0} retry", [selected.size ? tr("{0} selected", [selectedFinished]) : tr("all {0} finished", [finishedCount])])}</button>
						{/if}
					</div>
						<div class="row small">
							<span class="muted">{tr("Select:")}</span> <button class="btn btn-sm" onclick={() => selectState('waiting')}>{tr("waiting")}</button>
						<button class="btn btn-sm" onclick={() => selectState('admitted')}>{tr("admitted")}</button>
						<button class="btn btn-sm" onclick={() => selectState('in_progress')}>{tr("in progress")}</button>
						<button class="btn btn-sm" onclick={() => selectState('paused')}>{tr("paused")}</button>
						<button class="btn btn-sm" onclick={() => selectState('submitted')}>{tr("finished")}</button>
						<button class="btn btn-sm" onclick={() => (selected = new Set())}>{tr("none")}</button>
					</div>
				{/if}
				{#if notice}<p class="alert alert-soft alert-warning small">{notice}</p>{/if}
					{#if selected.size}<p class="small m-0" in:fadeIn><strong>{selected.size}</strong> {tr("selected · commands apply to them")}{#if selectedHidden}{tr(", including {0} the search is hiding", [selectedHidden])}{/if}</p>{/if}
					{#if rows.length}
						<div class="flex flex-wrap items-end gap-2" role="search" aria-label={tr("Find students")}>
							<label class="vstack gap-1 small">{tr("Search")}<input class="input input-sm w-56" type="search" bind:value={rowSearch} placeholder={tr("Name, ID or team")} /></label>
							<label class="vstack gap-1 small">{tr("Show")}
								<select class="select select-sm w-auto" bind:value={rowShow}>
									{#each ROW_SHOW as o (o.v)}<option value={o.v}>{tr(o.label)}</option>{/each}
								</select>
							</label>
							{#if searching}<button type="button" class="btn btn-ghost btn-sm" onclick={() => ((rowSearch = ''), (rowShow = ''))}>{tr("Clear")}</button>{/if}
							<span class="small muted pb-1 ms-auto" role="status">{searching ? tr("Showing {0} of {1} students", [shownRows.length, rows.length]) : `${rows.length} ${rows.length === 1 ? 'student' : 'students'}`}</span>
						</div>
					{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table">
						<thead><tr>{#if !ended}<th><span class="sr-only">{tr("Select")}</span></th>{/if}<th>{tr("Student")}</th>{#if teamsOn}<th>{tr("Team")}</th>{/if}<th>{tr("Status")}</th><th>{tr("Progress")}</th><th>{tr("Time left")}</th><th>{tr("Flags")}</th><th></th></tr></thead>
						<tbody>
							{#each shownRows as r (r.attempt_id)}
									<tr class:sel={selected.has(r.attempt_id)} in:fadeIn>
									{#if !ended}<td><input class="checkbox" type="checkbox" checked={selected.has(r.attempt_id)} onchange={() => toggle(r.attempt_id)} aria-label={tr("Select {0}", [r.name])} /></td>{/if}
									<td><span class="flex items-center gap-2"><Avatar id={r.user_id} name={r.name} avatar={r.avatar} size={30} /><span class="min-w-0">{r.name}{#if (r.try ?? 1) > 1}{' '}<span class="badge badge-soft badge-info badge-xs" title={tr("Which try this is")}>{tr("try {0}", [r.try])}</span>{/if}<br /><span class="small muted">{r.student_number ?? ''}</span></span></span></td>
									{#if teamsOn}
										{@const team = d.teams?.find((t) => t.id === r.team_id)}
										<td class="small">{#if team}<span class="inline-flex items-center gap-1"><span class="tdot" style:background="var(--cat-{team.color})" aria-hidden="true"></span>{team.name}{#if r.captain}<Icon name="star" size={12} /><span class="sr-only">{tr("(captain)")}</span>{/if}</span>{:else}<span class="muted">—</span>{/if}</td>
									{/if}
									<td><span class="badge badge-soft {STATE_BADGE[r.state] ?? ''}"><span aria-hidden="true">{STATE_ICON[r.state] ?? ''}</span> {r.state === 'admitted' ? (r.countdown_deadline == null ? tr("Ready") : tr("Starting in {0}", [formatDuration(countdownLeft(r))])) : STATE_LABEL[r.state]}</span>{#if !r.connected && (r.state === 'in_progress' || r.state === 'waiting')}<br /><span class="small muted">{tr("offline")}</span>{/if}</td>
									<td class="small min-w-32">
										<div class="flex items-center gap-2"><progress class="progress progress-primary w-20" value={r.answered} max={r.total || 1} aria-hidden="true"></progress><span class="tabular">{r.answered}/{r.total}</span></div>
										{#if r.state === 'in_progress'}<span class="muted">{tr("on Q{0}", [r.index + 1])}</span>{/if}
									</td>
									<td class="small tabular" class:text-error={(left(r) ?? Infinity) < 60_000 && r.state === 'in_progress'}>{formatDuration(left(r))}{#if r.extension_sec}<br /><span class="muted">{tr("+{0} min extra", [Math.round(r.extension_sec / 60)])}</span>{/if}</td>
									<td class="small">{#if r.violations}<span class="badge badge-soft badge-error gap-1 whitespace-nowrap"><Icon name="flag" size={12} />{tr("{0} violation{1}", [r.violations, r.violations > 1 ? 's' : ''])}</span>{/if}{#if r.invalid_reason}<br />{r.invalid_reason}{/if}</td>
									<td>{#if r.state === 'invalidated' && !ended}<button class="btn btn-sm" onclick={() => reinstate(r.attempt_id)}>{tr("Reinstate")}</button>{:else if r.state === 'submitted' && !ended && !teamsOn}<button class="btn btn-sm" onclick={() => reopen(r.attempt_id)} title={tr("For a student who handed in by mistake: they carry on with their answers")}>{tr("Reopen")}</button>{/if}</td>
								</tr>
							{:else}<tr><td colspan="8"><div class="muted py-6 text-center">{#if rows.length}{tr("No student matches. Clear the search to see everyone.")}{:else if ended}{tr("Nobody joined this session.")}{:else}<span class="loading loading-dots loading-md text-primary"></span><br />{tr("Waiting for students to scan the QR code…")}{/if}</div></td></tr>{/each}
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
