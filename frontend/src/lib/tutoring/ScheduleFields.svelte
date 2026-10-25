<script lang="ts" module>
	import type { Repeat } from './types';
	/** What the schedule part of the form holds. */
	export type ScheduleValue = {
		start: string; // local, 2026-11-02T15:30
		time_zone: string;
		duration_min: number;
		open_early_min: number;
		reminders: number[];
		repeat: Repeat;
		ends: 'count' | 'until';
	};
</script>

<script lang="ts">
	// When a session runs (D-84, TS-FR-85, TS-FR-87, TS-FR-89): the date and
	// time in a time zone, how long, whether it repeats and until when, how
	// early students can come in, and the reminders.
	import { REMINDER_CHOICES, repeatSummary, WEEKDAYS } from './types';

	let { value = $bindable(), errors = {} }: { value: ScheduleValue; errors?: Record<string, string> } = $props();
	const zones: string[] = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [];
	const startDay = $derived(value.start ? new Date(value.start + ':00').getDay() : 1);

	function toggleDay(d: number) {
		const days = new Set(value.repeat.days ?? [startDay]);
		if (days.has(d) && days.size > 1) days.delete(d);
		else days.add(d);
		value.repeat.days = [...days].sort();
	}
	function toggleReminder(m: number, on: boolean) {
		value.reminders = on ? [...new Set([...value.reminders, m])].sort((a, b) => b - a) : value.reminders.filter((x) => x !== m);
	}
	const err = (k: string) => errors[k] ?? errors['repeat.' + k];
</script>

<div class="vstack gap-3">
	<div class="grid gap-3 sm:grid-cols-3">
		<div>
			<label for="sch-start">Starts</label>
			<input id="sch-start" class="input w-full" type="datetime-local" bind:value={value.start} required aria-invalid={!!errors.start} />
			{#if errors.start}<p class="field-error">{errors.start}</p>{/if}
		</div>
		<div>
			<label for="sch-zone">Time zone</label>
			<input id="sch-zone" class="input w-full" list="sch-zones" bind:value={value.time_zone} required aria-invalid={!!errors.time_zone} />
			<datalist id="sch-zones">{#each zones as z (z)}<option value={z}></option>{/each}</datalist>
			{#if errors.time_zone}<p class="field-error">{errors.time_zone}</p>{/if}
		</div>
		<div>
			<label for="sch-dur">Length</label>
			<select id="sch-dur" class="select w-full" bind:value={value.duration_min}>
				{#each [30, 45, 60, 90, 120, 180] as m (m)}<option value={m}>{m < 60 ? `${m} minutes` : m === 60 ? '1 hour' : `${m / 60} hours`}</option>{/each}
			</select>
		</div>
	</div>

	<div class="grid gap-3 sm:grid-cols-3">
		<div>
			<label for="sch-repeat">Repeats</label>
			<select id="sch-repeat" class="select w-full" bind:value={value.repeat.freq}>
				<option value="">Doesn't repeat</option>
				<option value="daily">Every day</option>
				<option value="weekly">Every week</option>
			</select>
		</div>
		{#if value.repeat.freq}
			<div>
				<label for="sch-every">Every</label>
				<div class="join w-full">
					<input id="sch-every" class="input join-item w-20" type="number" min="1" max="12" bind:value={value.repeat.interval} aria-invalid={!!err('interval')} />
					<span class="join-item btn btn-disabled no-animation">{value.repeat.freq === 'daily' ? 'day(s)' : 'week(s)'}</span>
				</div>
			</div>
			<div>
				<label for="sch-ends">Ends</label>
				<div class="join w-full">
					<select id="sch-ends" class="select join-item w-28" bind:value={value.ends}><option value="count">after</option><option value="until">on</option></select>
					{#if value.ends === 'count'}
						<input class="input join-item w-20" type="number" min="1" max="100" bind:value={value.repeat.count} aria-label="Number of sessions" />
						<span class="join-item btn btn-disabled no-animation">times</span>
					{:else}
						<input class="input join-item flex-1" type="date" bind:value={value.repeat.until} aria-label="Last date" />
					{/if}
				</div>
				{#if err('until') || err('count')}<p class="field-error">{err('until') ?? err('count')}</p>{/if}
			</div>
		{/if}
	</div>
	{#if value.repeat.freq === 'weekly'}
		<div class="row gap-1 flex-wrap" role="group" aria-label="On these days">
			{#each WEEKDAYS as d, i (d)}
				{@const on = (value.repeat.days ?? [startDay]).includes(i)}
				<button type="button" class="btn btn-sm" class:btn-primary={on} aria-pressed={on} onclick={() => toggleDay(i)}>{d}</button>
			{/each}
		</div>
	{/if}
	{#if value.repeat.freq}<p class="small muted m-0">Repeats {repeatSummary({ ...value.repeat, count: value.ends === 'count' ? value.repeat.count : undefined, until: value.ends === 'until' ? value.repeat.until : undefined })}. At most 100 sessions, within a year; each can be moved or cancelled on its own.</p>{/if}

	<div class="grid gap-3 sm:grid-cols-2">
		<div>
			<label for="sch-early">Students can come in</label>
			<select id="sch-early" class="select w-full" bind:value={value.open_early_min}>
				{#each [0, 5, 10, 15, 30] as m (m)}<option value={m}>{m ? `${m} minutes before` : 'at the start'}</option>{/each}
			</select>
			<p class="small muted m-0 mt-1">They wait for you to start it.</p>
		</div>
		<fieldset>
			<legend class="text-sm">Reminders by email</legend>
			<div class="row gap-3 flex-wrap">
				{#each REMINDER_CHOICES as r (r.minutes)}
					<label class="row font-normal small"><input type="checkbox" class="checkbox checkbox-sm" checked={value.reminders.includes(r.minutes)} onchange={(e) => toggleReminder(r.minutes, e.currentTarget.checked)} /> {r.label}</label>
				{/each}
			</div>
			{#if errors.reminders}<p class="field-error">{errors.reminders}</p>{/if}
		</fieldset>
	</div>
</div>
