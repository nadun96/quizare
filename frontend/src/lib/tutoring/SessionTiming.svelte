<script lang="ts">
	// A scheduled session's timing, for the lead teacher before it starts
	// (D-84, TS-FR-85, TS-FR-87): move it (everyone is told), change how long
	// it runs, how early students come in and the reminders, or cancel it or
	// the rest of its series.
	import { goto } from '$app/navigation';
	import { ApiError } from '../api';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import type { TutorClient } from './client';
	import { localInput, REMINDER_CHOICES, type TutoringSession } from './types';

	let { client, session }: { client: TutorClient; session: TutoringSession } = $props();
	const path = $derived('/api/sessions/' + session.id);
	let start = $state('');
	let duration = $state(60);
	let early = $state(10);
	let reminders = $state<number[]>([]);
	let errors = $state<Record<string, string>>({});
	let loadedFor = '';
	$effect(() => {
		// Fill the form from the session, once per session (not on every update while editing).
		if (loadedFor === session.id) return;
		loadedFor = session.id;
		start = session.scheduled_at ? localInput(session.scheduled_at, session.time_zone) : '';
		duration = session.duration_min;
		early = session.open_early_min;
		reminders = [...session.reminders];
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		try {
			await client.put(path + '/timing', { start: start || undefined, duration_min: duration, open_early_min: early, reminders });
			toast(session.scheduled_at && localInput(session.scheduled_at, session.time_zone) !== start ? 'Moved: everyone is told by email' : 'Saved');
			loadedFor = '';
		} catch (err) {
			if (err instanceof ApiError) errors = Object.keys(err.fields).length ? err.fields : { _: err.message };
		}
	}
	async function cancel(series: boolean) {
		const ok = await confirmDialog({
			title: series ? 'Cancel this and the rest of the series?' : 'Cancel this session?',
			body: 'Everyone in the classroom is told by email, and nobody can join it.',
			confirm: series ? 'Cancel the rest' : 'Cancel session',
			danger: true
		});
		if (!ok) return;
		try {
			const r = await client.post<{ cancelled: number }>(path + '/cancel', { series });
			toast(r.cancelled === 1 ? 'Cancelled' : `${r.cancelled} sessions cancelled`);
			goto('/t/tutoring');
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'That didn’t work', 'error');
		}
	}
	const toggle = (m: number, on: boolean) => (reminders = on ? [...new Set([...reminders, m])] : reminders.filter((x) => x !== m));
</script>

<form class="vstack gap-3" onsubmit={save} aria-label="Schedule">
	<div class="grid gap-3 sm:grid-cols-3">
		<div>
			<label for="tm-start">Starts ({session.time_zone})</label>
			<input id="tm-start" class="input input-sm w-full" type="datetime-local" bind:value={start} aria-invalid={!!errors.start} />
			{#if errors.start}<p class="field-error">{errors.start}</p>{/if}
		</div>
		<div>
			<label for="tm-dur">Length (minutes)</label>
			<input id="tm-dur" class="input input-sm w-full" type="number" min="5" max="600" bind:value={duration} aria-invalid={!!errors.duration_min} />
		</div>
		<div>
			<label for="tm-early">Students come in (minutes before)</label>
			<input id="tm-early" class="input input-sm w-full" type="number" min="0" max="120" bind:value={early} aria-invalid={!!errors.open_early_min} />
		</div>
	</div>
	<fieldset>
		<legend class="text-sm">Reminders by email</legend>
		<div class="row gap-3 flex-wrap">
			{#each REMINDER_CHOICES as r (r.minutes)}
				<label class="row font-normal small"><input type="checkbox" class="checkbox checkbox-sm" checked={reminders.includes(r.minutes)} onchange={(e) => toggle(r.minutes, e.currentTarget.checked)} /> {r.label}</label>
			{/each}
		</div>
	</fieldset>
	{#each Object.entries(errors).filter(([k]) => k !== 'start') as [k, msg] (k)}<p class="field-error m-0">{msg}</p>{/each}
	<div class="row gap-2 flex-wrap">
		<button class="btn btn-sm btn-primary">Save</button>
		<span class="spacer"></span>
		<button type="button" class="btn btn-sm btn-error btn-outline" onclick={() => cancel(false)}>Cancel this session</button>
		{#if session.series_id}<button type="button" class="btn btn-sm btn-error btn-outline" onclick={() => cancel(true)}>Cancel the rest of the series</button>{/if}
	</div>
</form>
