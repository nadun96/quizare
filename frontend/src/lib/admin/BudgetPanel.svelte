<script lang="ts">
	// Tutoring's bandwidth budget, for the admin (D-81, TS-FR-31, TS-FR-33,
	// PO-9): how much upload all sessions may use together, the hard limit
	// nothing goes above, one session's share, and how many screens, cameras
	// and students sharing are allowed. Changes apply at once. Below, what
	// each live session sends now (estimated).
	import { ApiError } from '../api';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import { mbps, meter, STEP_LABELS, type Budget, type BudgetOverview } from '../tutoring/types';
	import { toast } from '../ui/toast.svelte';

	const client = new TutorClient();
	let enabled = $state<boolean | null>(null);
	let ov = $state<BudgetOverview | null>(null);
	let form = $state<Budget | null>(null);
	let errors = $state<Record<string, string>>({});
	const m = $derived(ov ? meter(ov.total_mbps, ov.budget.total_mbps) : null);

	async function load() {
		ov = await client.get<BudgetOverview>('/api/admin/budget');
		form ??= { ...ov.budget };
	}
	$effect(() => {
		let t: ReturnType<typeof setInterval> | undefined;
		tutoringConfig().then((c) => {
			enabled = c.enabled;
			if (!c.enabled) return;
			load().catch((e) => toast(e instanceof ApiError ? e.message : 'Tutoring didn’t answer', 'error'));
			t = setInterval(() => load().catch(() => {}), 5000);
		});
		return () => clearInterval(t);
	});
	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!form) return;
		try {
			form = await client.put<Budget>('/api/admin/budget', form);
			errors = {};
			await load();
			toast('Budget saved: it applies now');
		} catch (err) {
			if (err instanceof ApiError) errors = err.fields;
			toast(err instanceof ApiError ? err.message : 'Could not save', 'error');
		}
	}
	const fields: { key: keyof Budget; label: string; hint: string; step?: string }[] = [
		{ key: 'total_mbps', label: 'Budget (Mbit/s)', hint: 'All sessions together; keep some of the server’s port free', step: '1' },
		{ key: 'hard_cap_mbps', label: 'Hard limit (Mbit/s)', hint: 'Nothing goes above it, even when a teacher insists', step: '1' },
		{ key: 'session_mbps', label: 'One session at most (Mbit/s)', hint: '0: any of what’s free', step: '1' },
		{ key: 'max_teacher_tracks', label: 'Screens and cameras per broadcaster', hint: '1 to 8' },
		{ key: 'max_student_cameras', label: 'Students sharing a camera at once', hint: 'At least 2' },
		{ key: 'max_student_screens', label: 'Students sharing a screen at once', hint: 'At least 2' }
	];
</script>

{#if enabled === true && ov && form && m}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3">
		<h2 class="m-0 text-lg">Bandwidth</h2>
		<p class="m-0">Live sessions send about <strong class="tabular">{mbps(ov.total_mbps)}</strong> of {mbps(ov.budget.total_mbps)} ({m.pct}%).{#if m.level === 'over'}{' '}<span class="text-error font-semibold">Over budget: the service is switching video off, starting with the busiest session.</span>{/if}</p>
		<progress class="progress w-full" class:progress-warning={m.level === 'warn'} class:progress-error={m.level === 'over'} value={m.pct} max="100" aria-label="Tutoring against its budget"></progress>
		{#if ov.sessions.length}
			<table class="table table-sm">
				<thead><tr><th>Session</th><th>Teacher</th><th class="text-right">Watching</th><th class="text-right">Sends (est.)</th><th>Switched off</th></tr></thead>
				<tbody>
					{#each ov.sessions as s (s.id)}
						<tr><td>{s.title}</td><td>{s.teacher_name}</td><td class="tabular text-right">{s.viewers}</td><td class="tabular text-right">{mbps(s.mbps)}</td><td class="small">{s.auto_off.map((x) => STEP_LABELS[x]).join(', ')}</td></tr>
					{/each}
				</tbody>
			</table>
		{:else}
			<p class="small muted m-0">No session is live.</p>
		{/if}
		<form class="budget" onsubmit={save}>
			{#each fields as f (f.key)}
				<label class="small">{f.label}
					<input class="input input-sm" type="number" min="0" step={f.step ?? '1'} bind:value={form[f.key]} aria-invalid={!!errors[f.key]} aria-describedby={'hint-' + f.key} />
					<span id={'hint-' + f.key} class={errors[f.key] ? 'text-error' : 'muted'}>{errors[f.key] ?? f.hint}</span>
				</label>
			{/each}
			<div class="self-end"><button class="btn btn-sm btn-primary">Save</button></div>
		</form>
		<p class="small muted m-0">Figures are estimates: each screen, camera or voice times how many receive it, at typical rates (a readable screen about 1.2 Mbit/s, a camera 0.4).</p>
	</div>
{/if}

<style>
	.budget { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr)); }
	.budget label { display: flex; flex-direction: column; gap: 0.25rem; }
</style>
