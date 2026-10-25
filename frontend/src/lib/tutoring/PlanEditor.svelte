<script lang="ts">
	// A session's plan (D-81, TS-FR-08): how many students, what the
	// broadcaster sends, how many students may share at once, and video in
	// groups. The forecast is checked against what the server has free now;
	// teachers can keep plans they use often as presets.
	import { untrack } from 'svelte';
	import { ApiError } from '../api';
	import { confirmDialog, promptDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import type { TutorClient } from './client';
	import { mbps, meter, type Plan, type PlanView, type Preset } from './types';

	let { client, sessionId, plan, readonly = false }: { client: TutorClient; sessionId: string; plan: Plan; readonly?: boolean } = $props();
	// The form starts from the session's plan; later pushes don't overwrite what's being edited.
	let draft = $state<Plan>(untrack(() => ({ ...plan })));
	let pv = $state<PlanView | null>(null);
	let errors = $state<Record<string, string>>({});
	let presets = $state<Preset[]>([]);
	let saved = $state(untrack(() => JSON.stringify(plan)));
	const dirty = $derived(JSON.stringify(draft) !== saved);
	const m = $derived(pv ? meter(pv.forecast.mbps, pv.limit_mbps) : null);

	// The forecast follows the form, a moment after the last change.
	$effect(() => {
		const body = $state.snapshot(draft);
		const t = setTimeout(() => {
			client.post<PlanView>('/api/plans/estimate', body).then(
				(v) => ((pv = v), (errors = {})),
				(e) => e instanceof ApiError && (errors = e.fields)
			);
		}, 250);
		return () => clearTimeout(t);
	});
	$effect(() => {
		if (!readonly) client.get<{ presets: Preset[] }>('/api/plan-presets').then((r) => (presets = r.presets), () => {});
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		try {
			pv = await client.put<PlanView>(`/api/sessions/${sessionId}/plan`, draft);
			saved = JSON.stringify(draft);
			errors = {};
			toast('Plan saved');
		} catch (err) {
			if (err instanceof ApiError) errors = err.fields;
			toast(err instanceof ApiError ? err.message : 'Could not save', 'error');
		}
	}
	function apply(id: string) {
		const p = presets.find((x) => x.id === id);
		if (p) draft = { ...p.plan };
	}
	async function saveAs() {
		const name = await promptDialog({ title: 'Save this plan', confirm: 'Save', label: 'Name', maxlength: 60, value: '' });
		if (!name?.trim()) return;
		try {
			const p = await client.post<Preset>('/api/plan-presets', { name, plan: draft });
			presets = [...presets.filter((x) => x.name !== p.name), p].sort((a, b) => a.name.localeCompare(b.name));
			toast(`Saved as “${p.name}”`);
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not save', 'error');
		}
	}
	async function remove(p: Preset) {
		if (!(await confirmDialog({ title: `Delete the plan “${p.name}”?`, confirm: 'Delete', danger: true }))) return;
		await client.del(`/api/plan-presets/${p.id}`).then(() => (presets = presets.filter((x) => x.id !== p.id)), () => toast('Could not delete', 'error'));
	}
	let picked = $state('');
</script>

<form class="vstack gap-3" onsubmit={save} aria-label="Session plan">
	{#if presets.length}
		<div class="row gap-2 flex-wrap">
			<label class="small" for="preset">Start from</label>
			<select id="preset" class="select select-sm w-auto" bind:value={picked} onchange={() => apply(picked)} disabled={readonly}>
				<option value="">A saved plan…</option>
				{#each presets as p (p.id)}<option value={p.id}>{p.name}</option>{/each}
			</select>
			{#if picked}{@const p = presets.find((x) => x.id === picked)}{#if p}<button type="button" class="btn btn-ghost btn-xs" onclick={() => remove(p)}>Delete “{p.name}”</button>{/if}{/if}
		</div>
	{/if}
	<fieldset class="plan" disabled={readonly}>
		<label class="small">Students expected <input class="input input-sm" type="number" min="1" max="500" bind:value={draft.students} aria-invalid={!!errors.students} /></label>
		<label class="small">Screens <input class="input input-sm" type="number" min="0" max="8" bind:value={draft.screens} aria-invalid={!!errors.screens} /></label>
		<label class="small">Cameras <input class="input input-sm" type="number" min="0" max="8" bind:value={draft.cameras} /></label>
		<label class="small">Screen quality
			<select class="select select-sm" bind:value={draft.quality}><option value="readable">Readable (slides, code)</option><option value="smooth">Smooth (video, small classes)</option></select>
		</label>
		<label class="small">Students sharing a camera <input class="input input-sm" type="number" min="0" max="50" bind:value={draft.student_cameras} aria-invalid={!!errors.student_cameras} /></label>
		<label class="small">Students sharing a screen <input class="input input-sm" type="number" min="0" max="50" bind:value={draft.student_screens} aria-invalid={!!errors.student_screens} /></label>
		<label class="small">Students speaking at once <input class="input input-sm" type="number" min="0" max="50" bind:value={draft.student_mics} /></label>
		<label class="row font-normal small self-end"><input type="checkbox" class="toggle toggle-sm" bind:checked={draft.group_video} /> Cameras in breakout groups</label>
	</fieldset>
	{#each Object.entries(errors) as [k, msg] (k)}<p class="small text-error m-0">{msg}</p>{/each}

	{#if pv && m}
		<div class="vstack gap-1" aria-live="polite">
			<p class="m-0 small">Needs about <strong>{mbps(pv.forecast.mbps)}</strong> of the {mbps(pv.limit_mbps)} free now{#if pv.budget.session_mbps}{' '}(a session may use up to {mbps(pv.budget.session_mbps)}){/if}.</p>
			<progress class="progress w-full" class:progress-warning={m.level === 'warn'} class:progress-error={m.level === 'over'} value={m.pct} max="100" aria-label="Forecast against what's free"></progress>
			{#if !pv.fits}
				<p class="small m-0 text-error">Too much for what’s free{pv.advice?.length ? ': ' + pv.advice.join(', or ') : ''}.{pv.forecast.mbps <= pv.hard_free_mbps ? ' You can still start it; other sessions may get worse video.' : ' It can’t start like this.'}</p>
			{/if}
			<details class="small"><summary class="muted">How it adds up (estimated)</summary>
				<ul class="m-0 mt-1 ps-4">{#each pv.forecast.items as it (it.name)}<li>{it.name}: <span class="tabular">{mbps(it.mbps)}</span></li>{/each}</ul>
			</details>
		</div>
	{/if}
	{#if !readonly}
		<div class="row gap-2 flex-wrap">
			<button class="btn btn-sm btn-primary" disabled={!dirty}>Save plan</button>
			<button type="button" class="btn btn-sm" onclick={saveAs}>Save as a preset…</button>
		</div>
	{/if}
</form>

<style>
	.plan { display: grid; gap: 0.5rem 0.75rem; grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr)); border: 0; padding: 0; margin: 0; }
	.plan label:not(.row) { display: flex; flex-direction: column; gap: 0.25rem; }
</style>
