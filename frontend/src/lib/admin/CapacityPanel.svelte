<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// The capacity test (D-125), admins only: virtual students take a real
	// quiz on this server in growing steps while it measures how the server
	// copes; the report says how big a class it handled and why the next
	// step failed. The virtual accounts are deleted afterwards.
	import { api, ApiError } from '../api';
	import { openPrint, printHTML } from '../grading/grading';
	import { saveFile } from '../tutoring/types';
	import { confirmDialog } from '../ui/dialog.svelte';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';
	import { advice, estimateMinutes, failures, firstLoadSeconds, fmtMB, fmtMs, fmtPct, fmtSecs, parseSteps, PRESETS, reportCSV, STATUS, verdict, type Plan, type Progress, type Run } from './capacity';

	type State = { running: Progress | null; runs: Run[]; defaults: Plan; max_students: number; max_steps: number };
	let st = $state<State | null>(null);
	let preset = $state('limit');
	let stepsText = $state('100, 250, 500, 1000, 2000');
	let f = $state<Plan>({ steps: [], questions: 10, think_seconds: 3, ramp_seconds: 30, one_network: false, link_mbps: 300 });
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);
	let open = $state<Run | null>(null);

	async function load() {
		const next = await api.get<State>('/api/admin/loadtest');
		const wasRunning = st?.running?.run_id;
		if (!st) f = { ...next.defaults, steps: [] };
		st = next;
		// The test just finished: show its report.
		if (wasRunning && !next.running) show(wasRunning);
	}
	$effect(() => {
		load();
		// Every 2 s while a test runs, every 10 s otherwise (another admin may start one).
		let n = 0;
		const t = setInterval(() => (st?.running || ++n % 5 === 0) && load(), 2000);
		return () => clearInterval(t);
	});

	const steps = $derived(parseSteps(stepsText, st?.max_students, st?.max_steps));
	const plan = $derived<Plan>({ ...f, steps: Array.isArray(steps) ? steps : [] });
	function pick(k: string) {
		preset = k;
		const p = PRESETS.find((x) => x.key === k);
		if (p) stepsText = p.steps.join(', ');
	}

	async function start(e: SubmitEvent) {
		e.preventDefault();
		if (!Array.isArray(steps)) return;
		const ok = await confirmDialog({
			title: tr("Start the capacity test?"),
			body: tr("Up to {0} virtual students take a quiz on this server for about {1} min. Real people using the site meanwhile will find it slower. Their accounts and everything they make are deleted at the end.", [Math.max(...steps).toLocaleString(localeTag()), estimateMinutes(plan)]),
			confirm: tr("Start the test")
		});
		if (!ok) return;
		busy = true;
		errors = {};
		try {
			await api.post('/api/admin/loadtest', plan);
			open = null;
			await load();
		} catch (err) {
			if (err instanceof ApiError) {
				errors = err.fields;
				toast(Object.values(err.fields)[0] ?? err.message, 'error');
			} else toast(tr("The test didn’t start"), 'error');
		} finally {
			busy = false;
		}
	}
	async function stop() {
		await api.post('/api/admin/loadtest/stop');
		toast(tr("Stopping: the test cleans up and keeps its report so far"), 'info');
		load();
	}
	async function show(id: string) {
		open = await api.get<Run>('/api/admin/loadtest/runs/' + id);
	}
	async function remove(r: Run) {
		if (!(await confirmDialog({ title: tr("Delete this report?"), confirm: tr("Delete"), danger: true }))) return;
		await api.del('/api/admin/loadtest/runs/' + r.id);
		if (open?.id === r.id) open = null;
		load();
	}

	const when = (s: string) => new Date(s).toLocaleString(localeTag());
	const stepsLine = (p: Plan) => p.steps.map((n) => n.toLocaleString(localeTag())).join(' → ');
	function print(r: Run) {
		const rep = r.report!;
		const sub = `${verdict(r)} ${when(r.started_at)} · ${rep.machine.cpus} cores${rep.machine.mem_total_mb > 0 ? ` · ${fmtMB(rep.machine.mem_total_mb)} memory` : ''} · ${r.plan.questions} questions, ${r.plan.think_seconds} s each${r.plan.one_network ? ' · all from one network' : ''}`;
		const head = ['Students', 'Result', 'Sign-in p95', 'Join p95', 'Save p50', 'Save p95', 'Save p99', 'Failed', 'Dropped', 'CPU peak', 'Free memory', 'DB busy', 'KB each', 'Why'];
		const rows = rep.steps.map((s) => [s.students, s.passed ? 'passed' : 'too many', fmtMs(s.ops.login?.p95), fmtMs(s.ops.join?.p95), fmtMs(s.ops.save?.p50), fmtMs(s.ops.save?.p95), fmtMs(s.ops.save?.p99), failures(s), s.drops, fmtPct(s.cpu_peak), fmtMB(s.mem_free_min_mb), `${s.db_busy_peak} of ${rep.db_max}`, s.kb_per_student, (s.reasons ?? []).join('; ')]);
		if (!openPrint(printHTML('Capacity test', sub, head, rows))) toast(tr("Allow pop-ups to print"), 'error');
	}
	const fileName = (r: Run, ext: string) => `capacity-test-${r.started_at.slice(0, 10)}.${ext}`;
</script>

{#if !st}
	<Skeleton lines={6} />
{:else}
	<div class="vstack gap-4">
		<div>
			<p class="m-0 small muted">{tr("How many students this server handles taking a quiz at once. Virtual students sign in, join a real session, answer and submit, in growing steps, while the server’s speed, processor, memory and database are measured. Their accounts are hidden, can’t be used from outside, and are deleted with everything they made at the end.")}</p>
		</div>

		{#if st.running}
			{@const p = st.running}
			<section class="card card-border bg-base-100 p-4 sm:p-5 vstack gap-3" aria-live="polite">
				<div class="flex flex-wrap items-center gap-2">
					<span class="loading loading-spinner loading-sm text-primary"></span>
					<strong>{tr("Step {0} of {1}: {2} students", [p.step + 1, p.plan.steps.length, p.students.toLocaleString(localeTag())])}</strong>
					<span class="small muted">· {p.phase} · {fmtSecs(p.elapsed)}</span>
					<span class="spacer"></span>
					<button class="btn btn-sm btn-error btn-outline" disabled={p.stopping} onclick={stop}><Icon name="square" size={14} />{p.stopping ? tr("Stopping…") : tr("Stop")}</button>
				</div>
				<div>
					<div class="flex justify-between small"><span>{tr("Joined {0}", [p.joined.toLocaleString(localeTag())])}</span><span>{tr("Finished {0} of {1}", [p.finished.toLocaleString(localeTag()), p.students.toLocaleString(localeTag())])}</span></div>
					<progress class="progress progress-primary w-full" value={p.finished} max={p.students || 1}></progress>
				</div>
				{#if p.sample}
					<div class="tiles">
						<div class="tile"><span class="small muted">{tr("Processor")}</span><strong>{fmtPct(p.sample.cpu)}</strong></div>
						<div class="tile"><span class="small muted">{tr("Free memory")}</span><strong>{fmtMB(p.sample.mem_free_mb)}</strong></div>
						<div class="tile"><span class="small muted">{tr("Connected")}</span><strong>{p.sample.active.toLocaleString(localeTag())}</strong></div>
						<div class="tile"><span class="small muted">{tr("Answers a second")}</span><strong>{p.sample.saves}</strong></div>
						<div class="tile"><span class="small muted">{tr("Database in use")}</span><strong>{p.sample.db_busy}</strong></div>
						<div class="tile"><span class="small muted">{tr("Traffic")}</span><strong>{tr("{0} Mbit/s", [p.sample.down_mbps])}</strong></div>
					</div>
				{/if}
				{#if p.errors}<p class="m-0 small text-warning">{tr("{0} requests failed so far.", [p.errors.toLocaleString(localeTag())])}</p>{/if}
				{#each p.done as d (d.students)}
					<p class="m-0 small"><Icon name={d.passed ? 'check-circle' : 'alert'} size={14} /> {tr("{0} students: {1} (saving an answer {2} for 1 in 20)", [d.students.toLocaleString(localeTag()), d.passed ? 'passed' : tr("too many: {0}", [d.reasons?.[0] ?? '']), fmtMs(d.ops.save?.p95)])}</p>
				{/each}
			</section>
		{:else}
			<form class="card card-border bg-base-100 p-4 sm:p-5 vstack gap-3" onsubmit={start}>
				<div class="flex flex-wrap gap-2" role="group" aria-label={tr("How many students")}>
					{#each PRESETS as p (p.key)}
						<button type="button" class="btn btn-sm" class:btn-primary={preset === p.key} aria-pressed={preset === p.key} title={tr(p.hint)} onclick={() => pick(p.key)}>{tr(p.label)}</button>
					{/each}
					<button type="button" class="btn btn-sm" class:btn-primary={preset === 'custom'} aria-pressed={preset === 'custom'} onclick={() => (preset = 'custom')}>{tr("My own steps")}</button>
				</div>
				<label class="flex flex-col gap-1">
					<span class="small">{tr("Students in each step, separated by commas or spaces")}</span>
					<input class="input w-full sm:w-96" class:input-error={!Array.isArray(steps) || errors.steps} bind:value={stepsText} oninput={() => (preset = 'custom')} />
					<span class="small" class:text-error={!Array.isArray(steps)} class:muted={Array.isArray(steps)}>{Array.isArray(steps) ? tr("The test stops at the first step that’s too many. About {0} min if every step runs.", [estimateMinutes(plan)]) : steps}</span>
				</label>
				<div class="grid gap-3 sm:grid-cols-4">
					<label class="flex flex-col gap-1"><span class="small">{tr("Questions")}</span><input class="input" type="number" min="3" max="30" bind:value={f.questions} /></label>
					<label class="flex flex-col gap-1"><span class="small">{tr("Seconds on each")}</span><input class="input" type="number" min="1" max="30" bind:value={f.think_seconds} /></label>
					<label class="flex flex-col gap-1"><span class="small">{tr("Students arrive over (s)")}</span><input class="input" type="number" min="0" max="300" bind:value={f.ramp_seconds} /></label>
					<label class="flex flex-col gap-1"><span class="small">{tr("Server upload (Mbit/s)")}</span><input class="input" type="number" min="1" bind:value={f.link_mbps} /></label>
				</div>
				<label class="flex items-start gap-2">
					<input type="checkbox" class="checkbox checkbox-sm mt-0.5" bind:checked={f.one_network} />
					<span class="small">{tr("All from one school network: every student shares one internet address, as on a school’s Wi-Fi. Tests the per-network sign-in limit.")}</span>
				</label>
				{#each Object.entries(errors).filter(([k]) => k !== 'steps') as [k, v] (k)}<p class="field-error m-0">{k}: {v}</p>{/each}
				<div class="flex flex-wrap items-center gap-2">
					<button class="btn btn-primary" disabled={busy || !Array.isArray(steps)}><Icon name="play" size={16} />{tr("Start the test")}</button>
					<span class="small muted">{tr("Run it when the school is quiet. It won’t start while students are in a live quiz. Video tutoring and real internet speed aren’t part of it.")}</span>
				</div>
			</form>
		{/if}

		{#if open?.report}
			{@const r = open}
			{@const rep = open.report}
			{@const top = rep.steps.find((s) => s.students === rep.max) ?? rep.steps[0]}
			<section class="card card-border bg-base-100 p-4 sm:p-5 vstack gap-4" aria-labelledby="rep-h">
				<div class="flex flex-wrap items-start gap-2">
					<div class="grow">
						<h3 id="rep-h" class="m-0 text-base">{tr("Report")} <span class="badge badge-soft {STATUS[r.status].tone} badge-sm align-middle">{tr(STATUS[r.status].label)}</span></h3>
						<p class="m-0 small muted">{tr("{0} by {1} · {2} cores{3} · {4} questions, {5} s each{6}", [when(r.started_at), r.admin_name || tr("an admin"), rep.machine.cpus, rep.machine.mem_total_mb > 0 ? tr(" · {0} memory", [fmtMB(rep.machine.mem_total_mb)]) : '', r.plan.questions, r.plan.think_seconds, r.plan.one_network ? tr(" · one network") : ''])}</p>
					</div>
					<button class="btn btn-sm" onclick={() => print(r)}><Icon name="download" size={14} />{tr("Print or PDF")}</button>
					<button class="btn btn-sm" onclick={() => saveFile(fileName(r, 'csv'), reportCSV(r))}>{tr("CSV")}</button>
					<button class="btn btn-sm btn-ghost" onclick={() => saveFile(fileName(r, 'json'), JSON.stringify(r, null, 2), 'application/json')}>{tr("JSON")}</button>
					<button class="btn btn-sm btn-ghost" aria-label={tr("Close the report")} onclick={() => (open = null)}><Icon name="x" size={16} /></button>
				</div>
				<p class="verdict m-0">{verdict(r)}</p>
				{#if rep.steps.length && top}
					<div class="tiles">
						<div class="tile"><span class="small muted">{tr("Largest class that passed")}</span><strong>{rep.max ? rep.max.toLocaleString(localeTag()) : 'none'}</strong></div>
						<div class="tile"><span class="small muted">{tr("Saving an answer (1 in 20)")}</span><strong>{fmtMs(top.ops.save?.p95)}</strong></div>
						<div class="tile"><span class="small muted">{tr("Signing in (1 in 20)")}</span><strong>{fmtMs(top.ops.login?.p95)}</strong></div>
						<div class="tile"><span class="small muted">{tr("Processor at its busiest")}</span><strong>{fmtPct(top.cpu_peak)}</strong></div>
						<div class="tile"><span class="small muted">{tr("Least free memory")}</span><strong>{fmtMB(top.mem_free_min_mb)}</strong></div>
						<div class="tile"><span class="small muted">{tr("Quiz traffic per student")}</span><strong>{tr("{0} KB", [top.kb_per_student])}</strong></div>
					</div>
					<div class="table-wrap">
						<table class="table table-sm">
							<thead><tr><th>{tr("Students")}</th><th>{tr("Result")}</th><th>{tr("Sign-in")}</th><th>{tr("Join")}</th><th>{tr("Save answer")}<br /><span class="muted font-normal">{tr("median · 1 in 20 · 1 in 100")}</span></th><th>{tr("Failed")}</th><th>{tr("Processor")}</th><th>{tr("Free memory")}</th><th>{tr("Database")}</th><th>{tr("Traffic peak")}</th></tr></thead>
							<tbody>
								{#each rep.steps as s (s.students)}
									<tr>
										<td class="tabular font-semibold">{s.students.toLocaleString(localeTag())}</td>
										<td>{#if s.passed}<span class="badge badge-soft badge-success badge-sm">{tr("passed")}</span>{:else}<span class="badge badge-soft badge-warning badge-sm">{tr("too many")}</span>{/if}</td>
										<td class="tabular">{fmtMs(s.ops.login?.p95)}</td>
										<td class="tabular">{fmtMs(s.ops.join?.p95)}</td>
										<td class="tabular">{fmtMs(s.ops.save?.p50)} · {fmtMs(s.ops.save?.p95)} · {fmtMs(s.ops.save?.p99)}</td>
										<td class="tabular">{failures(s)}{s.drops ? tr(" · {0} dropped", [s.drops]) : ''}</td>
										<td class="tabular">{fmtPct(s.cpu_peak)}</td>
										<td class="tabular">{fmtMB(s.mem_free_min_mb)}</td>
										<td class="tabular">{tr("{0} of {1}{2}", [s.db_busy_peak, rep.db_max, s.db_waits ? tr(" · {0} waits", [s.db_waits]) : ''])}</td>
										<td class="tabular">{tr("{0} Mbit/s", [s.peak_down_mbps])}</td>
									</tr>
									{#if s.reasons?.length}
										<tr><td colspan="10" class="small text-warning">{tr("Why: {0}.", [s.reasons.join('; ')])}{#if s.first_error && Object.keys(s.first_error).length} {tr("First error: {0}", [Object.entries(s.first_error).map(([k, v]) => `${k}: ${v}`)[0]])}{/if}</td></tr>
									{/if}
								{/each}
							</tbody>
						</table>
					</div>
					<div class="vstack gap-1">
						<h4 class="m-0 text-sm">{tr("The internet connection")}</h4>
						{#if rep.page_kb}
							<p class="m-0 small">{tr("The app is about {0} KB on a first visit ({1} files, compressed). At {2} Mbit/s, if every student opens it for the first time at the same moment:", [Math.round(rep.page_kb), rep.page_files, r.plan.link_mbps])}
								{#each rep.steps as s, i (s.students)}{tr("{0}{1} students in {2}", [i ? ', ' : ' ', s.students.toLocaleString(localeTag()), fmtSecs(firstLoadSeconds(s.students, rep.page_kb, r.plan.link_mbps))])}{/each}{tr(". Their browsers keep it afterwards.")}</p>
						{:else}
							<p class="m-0 small muted">{tr("This server doesn’t serve the app’s pages itself, so the first download wasn’t measured.")}</p>
						{/if}
						<p class="m-0 small">{tr("During the quiz each student used about {0} KB; the busiest second carried {1} Mbit/s, well within {2} Mbit/s. This test runs inside the server, so it doesn’t include the speed of the internet, the school’s Wi-Fi or the proxy in front.", [top.kb_per_student, top.peak_down_mbps, r.plan.link_mbps])}</p>
					</div>
					{#each advice(r) as a, i (i)}<p class="alert alert-soft alert-info m-0 small"><Icon name="info" size={16} />{a}</p>{/each}
				{/if}
				{#if rep.error}<p class="alert alert-soft alert-error m-0 small">{rep.error}</p>{/if}
			</section>
		{/if}

		<section class="vstack gap-2" aria-labelledby="runs-h">
			<h3 id="runs-h" class="m-0 text-base">{tr("Earlier tests")}</h3>
			{#if st.runs.length}
				<div class="table-wrap card card-border bg-base-100">
					<table class="table table-sm">
						<thead><tr><th>{tr("Started")}</th><th>{tr("Steps")}</th><th>{tr("Result")}</th><th>{tr("Largest class")}</th><th><span class="sr-only">{tr("Actions")}</span></th></tr></thead>
						<tbody>
							{#each st.runs as r (r.id)}
								<tr class:active={open?.id === r.id}>
									<td class="small">{when(r.started_at)}<br /><span class="muted">{r.admin_name}</span></td>
									<td class="small tabular">{stepsLine(r.plan)}{r.plan.one_network ? tr(" · one network") : ''}</td>
									<td><span class="badge badge-soft {STATUS[r.status].tone} badge-sm">{tr(STATUS[r.status].label)}</span></td>
									<td class="tabular">{r.max ? r.max.toLocaleString(localeTag()) : '–'}</td>
									<td class="text-right whitespace-nowrap">
										{#if r.status !== 'running'}
											<button class="btn btn-xs" onclick={() => show(r.id)}>{tr("Report")}</button>
											<button class="btn btn-xs btn-ghost" aria-label={tr("Delete the report of {0}", [when(r.started_at)])} onclick={() => remove(r)}><Icon name="trash" size={13} /></button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<p class="m-0 small muted">{tr("No tests yet.")}</p>
			{/if}
		</section>
	</div>
{/if}

<style>
	.tiles { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr)); }
	.tile { display: flex; flex-direction: column; gap: 0.15rem; padding: 0.6rem 0.75rem; border: 1px solid var(--color-base-300); background: var(--color-base-100); }
	.tile strong { font-size: 1.15rem; font-variant-numeric: tabular-nums; }
	.verdict { font-size: 1.05rem; font-weight: 600; }
	tr.active { background: var(--color-base-200); }
</style>
