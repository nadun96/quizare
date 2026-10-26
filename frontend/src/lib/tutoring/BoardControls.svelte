<script lang="ts">
	// The whiteboard's controls, for the teachers (D-85, TS-FR-66): the lead
	// teacher shows or hides it for everyone and decides who draws besides the
	// teachers: nobody, everyone, or chosen students.
	import { ApiError } from '../api';
	import { toast } from '../ui/toast.svelte';
	import type { TutorClient } from './client';
	import type { TutoringView } from './types';

	let { client, view }: { client: TutorClient; view: TutoringView } = $props();
	const lead = $derived(view.me.role === 'teacher');
	const s = $derived(view.session);
	const students = $derived((view.participants ?? []).filter((p) => p.role === 'student' && p.state === 'admitted'));
	const chosen = $derived(new Set(s.board_users ?? []));

	async function set(open: boolean, mode = s.board_mode, participants = [...chosen]) {
		try {
			await client.put(`/api/sessions/${s.id}/board/access`, { open, mode, participants });
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	type Mode = TutoringView['session']['board_mode'];
	const MODES: [Mode, string][] = [['teacher', 'Teachers only'], ['everyone', 'Everyone'], ['selected', 'Teachers and students I choose']];
	const pick = (id: string, on: boolean) => set(s.board_open, 'selected', on ? [...chosen, id] : [...chosen].filter((x) => x !== id));
</script>

<section class="vstack gap-3" aria-label="Whiteboard">
	{#if lead}
		<label class="row font-normal"><input type="checkbox" class="toggle toggle-sm" checked={s.board_open} onchange={(e) => set(e.currentTarget.checked)} /> Show the whiteboard to everyone</label>
		<fieldset class="vstack gap-1">
			<legend class="text-sm font-semibold">Who draws</legend>
			{#each MODES as [m, l] (m)}
				<label class="row font-normal small"><input type="radio" class="radio radio-sm" name="board-mode" checked={s.board_mode === m} onchange={() => set(s.board_open, m)} /> {l}</label>
			{/each}
		</fieldset>
		{#if s.board_mode === 'selected'}
			<div class="vstack gap-1">
				{#each students as p (p.user_id)}
					<label class="row font-normal small"><input type="checkbox" class="checkbox checkbox-sm" checked={chosen.has(p.user_id)} onchange={(e) => pick(p.user_id, e.currentTarget.checked)} /> {p.name}</label>
				{:else}
					<p class="small muted m-0">No students are in yet.</p>
				{/each}
			</div>
		{/if}
	{:else}
		<p class="small m-0">{s.board_open ? 'The whiteboard is showing. You can draw on it.' : 'The lead teacher shows the whiteboard.'}</p>
	{/if}
	<p class="small muted m-0">The board sits beside or around the broadcast, as the layout says. Anyone can make it full screen or save it as a picture. Its marks stay while it's hidden.</p>
</section>
