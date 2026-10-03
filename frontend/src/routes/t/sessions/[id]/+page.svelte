<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { formatDuration } from '$lib/clock';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { LiveSocket } from '$lib/socket';
	import { STATE_BADGE, STATE_LABEL, type Dashboard } from '$lib/types';

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
	let socket: LiveSocket;
	let timer: ReturnType<typeof setInterval>;

	onMount(() => {
		socket = new LiveSocket('/ws/sessions/' + id);
		socket.onStatus = (c) => (connected = c);
		socket.onMessage = (m) => {
			if (m.type === 'dashboard') d = m as unknown as Dashboard;
			else if (m.type === 'alert') {
				alerts = [{ ...(m as unknown as Alert), at: Date.now() }, ...alerts].slice(0, 50);
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
			notice = `${action}: ${r.affected} student(s)`;
			if (!all) selected = new Set();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Command failed';
		}
	}
	async function end() {
		if (!confirm('End the session now? Running attempts are submitted as they are.')) return;
		await api.post('/api/teacher/sessions/' + id + '/end');
	}
	async function reinstate(attemptId: string) {
		const reason = prompt('Reason for reinstating this attempt (logged):');
		if (!reason) return;
		try {
			await api.post('/api/teacher/attempts/' + attemptId + '/reinstate', { reason });
		} catch (e) {
			notice = e instanceof ApiError ? e.message : 'Could not reinstate';
		}
	}
	async function saveSessionSettings() {
		const o: Record<string, unknown> = { ...(d?.session.settings ?? {}) };
		if (countdown === '') delete o.countdown_seconds;
		else o.countdown_seconds = Number(countdown);
		if (admission) o.admission_mode = admission;
		else delete o.admission_mode;
		await api.put('/api/teacher/sessions/' + id + '/settings', o);
		notice = 'Session settings saved';
	}
	function left(r: Dashboard['rows'][number]) {
		void now;
		if (r.remaining_ms != null) return r.remaining_ms;
		return r.quiz_deadline ? Math.max(0, r.quiz_deadline - socket.clock.serverNow()) : null;
	}
</script>

<div class="container stack" style="max-width:1300px">
	{#if d}
		<p class="small"><a href={'/t/quizzes/' + d.session.quiz_id}>← Quiz</a></p>
		<div class="row">
			<h1 style="margin:0">{d.session.title}</h1>
			<span class="badge {d.session.status === 'live' ? 'ok' : ''}">{d.session.status}</span>
			{#if !connected}<span class="badge warn">reconnecting…</span>{/if}
			<span class="spacer"></span>
			<a class="button" href={'/t/sessions/' + id + '/projector'} target="_blank">Show QR full screen</a>
			<a class="button" href={'/t/sessions/' + id + '/results'}>Results</a>
			{#if !ended}<button class="danger" onclick={end}>End session</button>{/if}
		</div>

		<div class="layout">
			<aside class="stack">
				<div class="card stack" style="text-align:center">
					<QrCode text={d.session.join_url} size={220} />
					<p style="font-size:1.6rem;font-weight:700;letter-spacing:0.1em;margin:0">{d.session.join_code}</p>
					<p class="small" style="word-break:break-all">{d.session.join_url}</p>
				</div>
				<div class="card stack">
					<strong>Counts</strong>
					{#each Object.entries(STATE_LABEL) as [k, l] (k)}
						{#if d.counts[k]}<div class="row small"><span>{l}</span><span class="spacer"></span><strong>{d.counts[k]}</strong></div>{/if}
					{/each}
				</div>
				{#if !ended}
					<div class="card stack">
						<strong>Session settings</strong>
						<div><label for="cd">Countdown (seconds)</label><input id="cd" type="number" min="0" bind:value={countdown} placeholder="inherit" /></div>
						<div><label for="adm">Admission</label><select id="adm" bind:value={admission}><option value="">Inherit</option><option value="manual">I admit students</option><option value="auto">Admit automatically</option></select></div>
						<button class="small" onclick={saveSessionSettings}>Save</button>
					</div>
				{/if}
				<div class="card stack">
					<strong>Alerts</strong>
					{#each alerts as a (a.at + a.attempt_id)}
						<div class="small alert {a.action === 'invalidated' ? 'danger' : ''}">
							<strong>{a.student_name}</strong>{a.student_number ? ` (${a.student_number})` : ''}: {a.kind.replace('_', ' ')} → {a.action}
							<br /><span class="muted">{new Date(a.at).toLocaleTimeString()}</span>
						</div>
					{:else}<p class="small muted">No violations so far.</p>{/each}
				</div>
			</aside>

			<section class="stack">
				{#if !ended}
					<div class="card row">
						<button class="primary" onclick={() => cmd('admit', true)} disabled={!d.counts.waiting}>Admit all waiting ({d.counts.waiting ?? 0})</button>
						<button onclick={() => cmd('admit', false)} disabled={!selected.size}>Admit selected</button>
						<button onclick={() => cmd('pause', selected.size === 0)}>Pause {selected.size ? 'selected' : 'all'}</button>
						<button onclick={() => cmd('resume', selected.size === 0)}>Resume {selected.size ? 'selected' : 'all'}</button>
						<span class="row"><input type="number" min="1" style="width:5rem" bind:value={extendMin} aria-label="Minutes" />
							<button onclick={() => cmd('extend', selected.size === 0, { seconds: extendMin * 60 })}>+ min {selected.size ? 'selected' : 'everyone'}</button></span>
					</div>
					<div class="row small">
						Select: <button class="small" onclick={() => selectState('waiting')}>waiting</button>
						<button class="small" onclick={() => selectState('in_progress')}>in progress</button>
						<button class="small" onclick={() => selectState('paused')}>paused</button>
						<button class="small" onclick={() => (selected = new Set())}>none</button>
					</div>
				{/if}
				{#if notice}<p class="alert small">{notice}</p>{/if}
				<div class="card table-wrap">
					<table>
						<thead><tr><th></th><th>Student</th><th>Status</th><th>Progress</th><th>Time left</th><th>Flags</th><th></th></tr></thead>
						<tbody>
							{#each rows as r (r.attempt_id)}
								<tr class:sel={selected.has(r.attempt_id)}>
									<td><input type="checkbox" checked={selected.has(r.attempt_id)} onchange={() => toggle(r.attempt_id)} aria-label={'Select ' + r.name} /></td>
									<td>{r.name}<br /><span class="small muted">{r.student_number ?? ''}</span></td>
									<td><span class="badge {STATE_BADGE[r.state] ?? ''}">{STATE_LABEL[r.state]}</span>{#if !r.connected && (r.state === 'in_progress' || r.state === 'waiting')}<br /><span class="small muted">offline</span>{/if}</td>
									<td class="small">{r.answered}/{r.total} answered{#if r.state === 'in_progress'}<br />on Q{r.index + 1}{/if}</td>
									<td class="small">{formatDuration(left(r))}{#if r.extension_sec}<br /><span class="muted">+{Math.round(r.extension_sec / 60)} min</span>{/if}</td>
									<td class="small">{#if r.violations}<span class="badge danger">{r.violations} violation{r.violations > 1 ? 's' : ''}</span>{/if}{#if r.invalid_reason}<br />{r.invalid_reason}{/if}</td>
									<td>{#if r.state === 'invalidated' && !ended}<button class="small" onclick={() => reinstate(r.attempt_id)}>Reinstate</button>{/if}</td>
								</tr>
							{:else}<tr><td colspan="7" class="muted">Waiting for students to scan the QR code…</td></tr>{/each}
						</tbody>
					</table>
				</div>
			</section>
		</div>
	{:else}
		<p>Connecting…</p>
	{/if}
</div>

<style>
	.layout { display: grid; gap: 1rem; grid-template-columns: 280px 1fr; align-items: start; }
	@media (max-width: 900px) { .layout { grid-template-columns: 1fr; } }
	tr.sel { background: var(--bg); }
</style>
