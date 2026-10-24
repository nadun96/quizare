<script lang="ts">
	// How big meetings may be, and how many each teacher may keep open (D-72).
	// 0 means unlimited.
	import { api, ApiError } from '../api';
	import type { MeetingLimits } from '../meet/mesh';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';

	let l = $state<MeetingLimits | null>(null);
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	$effect(() => {
		api.get<MeetingLimits>('/api/admin/meeting-limits').then((r) => (l = r));
	});
	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!l) return;
		saving = true;
		errors = {};
		try {
			l = await api.put<MeetingLimits>('/api/admin/meeting-limits', { max_people: Number(l.max_people), max_open: Number(l.max_open) });
			toast('Meeting limits saved');
		} catch (err) {
			if (err instanceof ApiError) errors = err.fields;
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not save', 'error');
		} finally {
			saving = false;
		}
	}
	const say = (n: number, one: string, many: string) => (Number(n) === 0 ? 'no limit' : `${n} ${Number(n) === 1 ? one : many}`);
</script>

{#if l}
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={save}>
		<h3 class="m-0 flex items-center gap-2"><Icon name="video" size={18} />Meetings</h3>
		<p class="small muted m-0">Video goes straight between the people in a meeting, each sending to every other one, so large meetings need good connections. <strong>0</strong> means no limit.</p>
		<div class="flex flex-wrap gap-4">
			<div>
				<label for="ml-people" class="font-semibold">People in one meeting</label>
				<input id="ml-people" class="input w-32" class:input-error={errors.max_people} type="number" min="0" max="1000" bind:value={l.max_people} aria-describedby="ml-people-h" />
				<p class="small muted m-0" id="ml-people-h">Now: {say(l.max_people, 'person', 'people')}. Lowering it applies to open meetings at once.</p>
			</div>
			<div>
				<label for="ml-open" class="font-semibold">Open meetings per teacher</label>
				<input id="ml-open" class="input w-32" class:input-error={errors.max_open} type="number" min="0" max="1000" bind:value={l.max_open} aria-describedby="ml-open-h" />
				<p class="small muted m-0" id="ml-open-h">Now: {say(l.max_open, 'meeting', 'meetings')}.</p>
			</div>
		</div>
		<div><button class="btn" disabled={saving}>Save</button></div>
	</form>
{/if}
