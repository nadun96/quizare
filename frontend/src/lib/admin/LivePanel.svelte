<script lang="ts">
	// Live activity (D-90): everything live across the platform, ending any
	// of it with a reason the teacher is emailed, and pausing new
	// activities for everyone or one teacher to manage the server's load.
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '../api';
	import { confirmDialog, promptDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import { everyonePause, fromTutoring, kindLabel, liveFor, peopleText, summary, type Activity, type ActivityKind, type Pause, type TutoringLive } from './live';
	import { auth } from '../session.svelte';
	import { isAdmin } from '../staff';
	import { TutorClient, tutoringConfig } from '../tutoring/client';

	// Tutoring sessions come from the tutoring service, for admins only.
	const tutor = new TutorClient();
	let tutoringOn = $state(false);
	if (isAdmin(auth.user))
		tutoringConfig().then((c) => {
			tutoringOn = c.enabled;
			if (tutoringOn) load();
		});

	let activities = $state<Activity[] | null>(null);
	let pauses = $state<Pause[]>([]);
	let kind = $state<ActivityKind | ''>('');
	let email = $state('');
	let reason = $state('');
	let errors = $state<Record<string, string>>({});
	let now = $state(Date.now());
	let timer: ReturnType<typeof setInterval> | undefined;

	async function load() {
		const r = await api.get<{ activities: Activity[]; pauses: Pause[] }>('/api/admin/live-activity');
		let tutoring: Activity[] = [];
		if (tutoringOn) {
			try {
				tutoring = (await tutor.call<{ sessions: TutoringLive[] }>('GET', '/api/admin/live')).sessions.map(fromTutoring);
			} catch {
				// The tutoring service is down: show the rest.
			}
		}
		activities = [...r.activities, ...tutoring].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at));
		pauses = r.pauses;
		now = Date.now();
	}
	load();
	timer = setInterval(load, 15000);
	onDestroy(() => clearInterval(timer));

	const all = $derived(everyonePause(pauses));
	const teachers = $derived(pauses.filter((p) => p.teacher_id !== null));
	const counts = $derived(summary(activities ?? []));
	const shown = $derived((activities ?? []).filter((a) => !kind || a.kind === kind));

	function fail(err: unknown, what: string) {
		toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : what, 'error');
	}

	async function end(a: Activity) {
		const why = await promptDialog({
			title: `End "${a.title}" now?`,
			body: `It ends for everyone in it. ${a.teacher_name} is emailed why, and it's in the audit log. What was done until now is kept.`,
			label: 'Why (the teacher is told)',
			confirm: 'End now',
			danger: true,
			multiline: true,
			maxlength: 500
		});
		if (why === null) return;
		try {
			if (a.kind === 'tutoring') await tutor.call('POST', `/api/admin/sessions/${a.id}/end`, { reason: why });
			else await api.post(`/api/admin/live-activity/${a.kind}/${a.id}/end`, { reason: why });
			toast(`${kindLabel(a.kind)} ended`);
		} catch (err) {
			fail(err, 'Could not end it');
		}
		await load();
	}

	async function pause(body: { teacher_id?: string; teacher_email?: string; reason: string }) {
		errors = {};
		try {
			await api.post('/api/admin/live-activity/pauses', body);
			toast('New activities paused');
			await load();
			return true;
		} catch (err) {
			if (err instanceof ApiError) errors = err.fields;
			fail(err, 'Could not pause');
			return false;
		}
	}

	async function pauseEveryone() {
		const why = await promptDialog({
			title: 'Pause new activities for everyone?',
			body: "No teacher can start a quiz session, open a poll or a meeting, present live or start a tutoring session until you resume. What's live now carries on.",
			label: 'Why (teachers see it when they try)',
			allowEmpty: true,
			confirm: 'Pause',
			maxlength: 500
		});
		if (why !== null) await pause({ reason: why });
	}

	async function pauseTeacher(a: Activity) {
		const why = await promptDialog({
			title: `Pause new activities for ${a.teacher_name}?`,
			body: "They can't start anything new until you resume. What they have live now carries on.",
			label: 'Why (they see it when they try)',
			allowEmpty: true,
			confirm: 'Pause',
			maxlength: 500
		});
		if (why !== null) await pause({ teacher_id: a.teacher_id, reason: why });
	}

	async function pauseByEmail(e: SubmitEvent) {
		e.preventDefault();
		if (await pause({ teacher_email: email.trim(), reason })) email = reason = '';
	}

	async function resume(p: Pause) {
		const who = p.teacher_id ? p.teacher_name || p.teacher_email : 'everyone';
		if (!(await confirmDialog({ title: `Resume new activities for ${who}?`, confirm: 'Resume' }))) return;
		try {
			await api.del('/api/admin/live-activity/pauses/' + p.id);
			toast('Resumed');
		} catch (err) {
			fail(err, 'Could not resume');
		}
		await load();
	}
</script>

<div class="vstack">
	<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="pause-h">
		<h2 id="pause-h" class="m-0 text-lg">New activities</h2>
		{#if all}
			<div role="status" class="alert alert-warning">
				<span>Paused for everyone{all.reason ? `: ${all.reason}` : ''}. Teachers can't start anything new.</span>
				<button class="btn btn-sm" onclick={() => resume(all)}>Resume</button>
			</div>
		{:else}
			<p class="m-0 small muted">Teachers may start quiz sessions, polls, meetings, live presentations and tutoring sessions. Pausing stops new ones; what's live carries on.</p>
			<div><button class="btn btn-sm" onclick={pauseEveryone}>Pause for everyone</button></div>
		{/if}

		{#if teachers.length}
			<h3 class="m-0 mt-2 text-base">Paused teachers</h3>
			<ul class="vstack m-0 p-0 list-none">
				{#each teachers as p (p.id)}
					<li class="flex flex-wrap items-center gap-2">
						<span><strong>{p.teacher_name}</strong> <span class="muted small">{p.teacher_email}</span>{#if p.reason}{' · '}{p.reason}{/if}</span>
						<button class="btn btn-xs" onclick={() => resume(p)} aria-label={'Resume ' + p.teacher_name}>Resume</button>
					</li>
				{/each}
			</ul>
		{/if}

		<form class="flex flex-wrap items-end gap-2" onsubmit={pauseByEmail}>
			<div>
				<label for="pause-email" class="font-semibold small">Pause a teacher</label>
				<input id="pause-email" class="input input-sm w-64" class:input-error={errors.teacher} type="email" placeholder="teacher@school.edu" bind:value={email} required aria-describedby={errors.teacher ? 'pause-email-e' : undefined} />
				{#if errors.teacher}<p id="pause-email-e" class="small text-error m-0">{errors.teacher}</p>{/if}
			</div>
			<div>
				<label for="pause-reason" class="font-semibold small">Why (optional)</label>
				<input id="pause-reason" class="input input-sm w-64" maxlength="500" bind:value={reason} />
			</div>
			<button class="btn btn-sm">Pause</button>
		</form>
	</section>

	<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="live-h">
		<div class="flex flex-wrap items-center gap-2">
			<h2 id="live-h" class="m-0 text-lg grow">Live now</h2>
			<button class="btn btn-sm btn-ghost" onclick={load}>Refresh</button>
		</div>
		<div class="flex flex-wrap gap-2" role="group" aria-label="Show">
			<button class="btn btn-sm" class:btn-active={kind === ''} aria-pressed={kind === ''} onclick={() => (kind = '')}>All {activities?.length ?? 0}</button>
			{#each counts as c (c.kind)}
				<button class="btn btn-sm" class:btn-active={kind === c.kind} aria-pressed={kind === c.kind} onclick={() => (kind = c.kind)}>
					{c.label}{' '}{c.count}{#if c.people}<span class="muted">{' · '}{c.people} people</span>{/if}
				</button>
			{/each}
		</div>
		{#if activities === null}
			<p class="muted m-0">Loading…</p>
		{:else if shown.length === 0}
			<p class="muted m-0">Nothing is live{kind ? ' of this kind' : ''}.</p>
		{:else}
			<div class="table-wrap">
				<table class="table">
					<thead><tr><th>What</th><th>Teacher</th><th>Classroom</th><th>People</th><th>Live for</th><th><span class="sr-only">Actions</span></th></tr></thead>
					<tbody>
						{#each shown as a (a.kind + a.id)}
							<tr>
								<td><span class="badge badge-ghost badge-sm">{kindLabel(a.kind)}</span>{' '}{a.title}</td>
								<td>{a.teacher_name}<br /><span class="small muted">{a.teacher_email}</span></td>
								<td>{a.classroom || '—'}</td>
								<td>{peopleText(a.kind, a.people)}</td>
								<td>{liveFor(a.started_at, now)}</td>
								<td class="whitespace-nowrap">
									<button class="btn btn-xs btn-error" onclick={() => end(a)}>End now</button>
									{#if !pauses.some((p) => p.teacher_id === a.teacher_id)}
										<button class="btn btn-xs" onclick={() => pauseTeacher(a)}>Pause teacher</button>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
		<p class="small muted m-0">Polls stay live while open, which can be days. Tutoring sessions are listed for admins, when tutoring is on.</p>
	</section>
</div>
