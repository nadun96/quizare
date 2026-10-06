<script lang="ts">
	// How a poll runs: who is identified, who may answer, pacing, and what
	// participants see (D-40). Each choice says what it means for students.
	import Icon, { type IconName } from '../ui/Icon.svelte';
	import type { Classroom } from '../types';
	import type { PollSettings } from './types';

	let { settings = $bindable(), classroomId = $bindable(null), classrooms = [], locked = false }: { settings: PollSettings; classroomId?: string | null; classrooms?: Classroom[]; locked?: boolean } = $props();

	const identities: { v: PollSettings['identity']; icon: IconName; title: string; body: string }[] = [
		{ v: 'anonymous', icon: 'eye-off', title: 'Anonymous', body: 'No login. Nobody, not even you, can see who answered what.' },
		{ v: 'identified', icon: 'user', title: 'Identified', body: 'Participants log in; you see names next to answers. Classmates never do.' },
		{ v: 'optional', icon: 'users', title: 'Participant chooses', body: 'Logged-in participants may put their name to their answers, or stay anonymous.' }
	];
	$effect(() => {
		if (settings.audience === 'classroom' && settings.identity !== 'identified') settings.audience = 'anyone';
		if (settings.show_results === 'presenter' && settings.pacing !== 'presenter') settings.show_results = 'after_answer';
		if (settings.show_answers === 'presenter' && settings.pacing !== 'presenter') settings.show_answers = 'after_close';
		if (settings.names === 'name' && settings.identity === 'anonymous') settings.names = 'nickname';
		if (settings.groups === 'categories' && settings.identity === 'anonymous') settings.groups = 'manual';
	});
	const FORMATION: [PollSettings['groups'], string][] = [['off', 'No groups'], ['manual', 'I put people in groups'], ['random', 'At random'], ['categories', 'From classroom categories'], ['self', 'Participants choose']];
	const FORMATION_HINT: Record<PollSettings['groups'], string> = {
		off: 'Everyone answers for themselves.',
		manual: 'Create groups and move participants into them on the Groups tab.',
		random: 'Each person who joins goes to the smallest group. You can also shuffle everyone on the Groups tab.',
		categories: "One group per category of the poll's classroom; logged-in students join their category's group.",
		self: 'Participants pick a group from your list when they join.'
	};
	const ACCEPT: [PollSettings['group_acceptance'], string, string][] = [
		['all', 'Every answer counts', "Each member answers; the group's mark combines them."],
		['first', 'First answer', "The group's first answer counts; teammates can't answer after it."],
		['captain', 'Captain answers', 'Only the captain answers for the group. The first to join is captain; you can change it.'],
		['best', 'Best answer', "Each member answers; the group gets its best member's mark."]
	];
	const CALC: [PollSettings['group_calc'], string][] = [['sum', 'Total'], ['average', 'Average'], ['max', 'Highest'], ['min', 'Lowest']];
	const ANSWERS: [PollSettings['show_answers'], string][] = [['after_answer', 'Right after answering'], ['presenter', 'When I reveal'], ['after_close', 'When the poll closes'], ['never', 'Never']];
	const BOARD: [PollSettings['leaderboard'], string, string][] = [
		['presenter', 'On my screen', 'Only you see the ranking, on the presenter screen and here.'],
		['everyone', 'Everyone', 'Participants see the top 10 and their own place, live.'],
		['off', 'Off', 'Points are kept for you; nobody sees a ranking.']
	];
</script>

<div class="vstack">
	<fieldset class="group" disabled={locked}>
		<legend>Identity</legend>
		{#if locked}<p class="small muted m-0 mb-2">People have joined, so identity is fixed. Reset the responses to change it.</p>{/if}
		<div class="cards">
			{#each identities as o (o.v)}
				<label class="opt" class:on={settings.identity === o.v}>
					<input type="radio" class="sr-only" name="identity" value={o.v} bind:group={settings.identity} />
					<span class="opt-icon" aria-hidden="true"><Icon name={o.icon} size={18} /></span>
					<span><span class="block font-semibold">{o.title}</span><span class="small muted block">{o.body}</span></span>
				</label>
			{/each}
		</div>
	</fieldset>

	<fieldset class="group">
		<legend>Who can answer</legend>
		<div class="join flex-wrap">
			<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.audience === 'anyone'} onclick={() => (settings.audience = 'anyone')}>Anyone with the code</button>
			<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.audience === 'classroom'} disabled={settings.identity !== 'identified'} onclick={() => (settings.audience = 'classroom')}>Students of one classroom</button>
		</div>
		{#if settings.identity !== 'identified'}<p class="small muted m-0 mt-1">Classroom-only polls need identified participants.</p>{/if}
		{#if settings.audience === 'classroom' || classrooms.length}
			<div class="mt-2 max-w-md">
				<label for="ps-class">Classroom {settings.audience === 'classroom' ? '' : '(optional, for your own organising)'}</label>
				<select id="ps-class" class="select select-sm w-full" value={classroomId ?? ''} onchange={(e) => (classroomId = e.currentTarget.value || null)}>
					<option value="">None</option>
					{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
				</select>
			</div>
		{/if}
	</fieldset>

	<fieldset class="group">
		<legend>Pacing</legend>
		<div class="cards two">
			<label class="opt" class:on={settings.pacing === 'self'}>
				<input type="radio" class="sr-only" name="pacing" value="self" bind:group={settings.pacing} />
				<span class="opt-icon" aria-hidden="true"><Icon name="menu" size={18} /></span>
				<span><span class="block font-semibold">Self-paced</span><span class="small muted block">Everyone sees all questions and answers in any order.</span></span>
			</label>
			<label class="opt" class:on={settings.pacing === 'presenter'}>
				<input type="radio" class="sr-only" name="pacing" value="presenter" bind:group={settings.pacing} />
				<span class="opt-icon" aria-hidden="true"><Icon name="play" size={18} /></span>
				<span><span class="block font-semibold">Presenter-led</span><span class="small muted block">You move everyone through one question at a time from the screen.</span></span>
			</label>
		</div>
	</fieldset>

	<fieldset class="group">
		<legend>Results for participants</legend>
		<div class="join flex-wrap">
			{#each [['live', 'Live, always'], ['after_answer', 'After they answer'], ['presenter', 'When I reveal'], ['never', 'Never']] as [v, l] (v)}
				<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.show_results === v} disabled={v === 'presenter' && settings.pacing !== 'presenter'} onclick={() => (settings.show_results = v as PollSettings['show_results'])}>{l}</button>
			{/each}
		</div>
		<p class="small muted m-0 mt-1">You always see live results. Participants never see names.</p>
		<label class="mt-2 flex items-center gap-2 font-normal"><input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={settings.allow_edit} />Participants can change their answers while the poll is open</label>
	</fieldset>

	<fieldset class="group">
		<legend>Competition</legend>
		<label class="flex items-start gap-2 font-normal">
			<input type="checkbox" class="toggle toggle-sm toggle-primary mt-0.5" bind:checked={settings.scoring} />
			<span><span class="font-semibold">Score answers</span><span class="small muted block">Questions with a correct answer earn points (100 by default). Opinion questions stay unscored.</span></span>
		</label>
		{#if settings.scoring}
			<div class="vstack mt-3 border-l-2 border-base-300 pl-4">
				<label class="flex items-start gap-2 font-normal">
					<input type="checkbox" class="toggle toggle-sm toggle-primary mt-0.5" bind:checked={settings.speed_bonus} disabled={settings.pacing !== 'presenter'} />
					<span><span class="font-semibold">Reward speed</span><span class="small muted block">{settings.pacing === 'presenter' ? 'On questions with a time limit, a right answer earns 50–100% of its points: the faster, the more.' : 'Needs presenter-led pacing, so everyone starts each question together.'}</span></span>
				</label>
				<div>
					<p class="small font-semibold m-0 mb-1">Leaderboard</p>
					<div class="cards three">
						{#each BOARD as [v, t, d] (v)}
							<label class="opt compact" class:on={settings.leaderboard === v}>
								<input type="radio" class="sr-only" name="leaderboard" value={v} bind:group={settings.leaderboard} />
								<span><span class="block font-semibold">{t}</span><span class="small muted block">{d}</span></span>
							</label>
						{/each}
					</div>
				</div>
				<div>
					<p class="small font-semibold m-0 mb-1">Show participants the correct answer</p>
					<div class="join flex-wrap">
						{#each ANSWERS as [v, l] (v)}
							<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.show_answers === v} disabled={v === 'presenter' && settings.pacing !== 'presenter'} onclick={() => (settings.show_answers = v)}>{l}</button>
						{/each}
					</div>
				</div>
				<div>
					<p class="small font-semibold m-0 mb-1">Names on the leaderboard</p>
					<div class="join flex-wrap">
						<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.names === 'nickname'} onclick={() => (settings.names = 'nickname')}>Nicknames</button>
						<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.names === 'name'} disabled={settings.identity === 'anonymous'} onclick={() => (settings.names = 'name')}>Real names of logged-in participants</button>
					</div>
					<p class="small muted m-0 mt-1">Participants choose a nickname when they join; without one they appear as "Participant N". You can rename anyone.</p>
				</div>
			</div>
		{/if}
	</fieldset>

	<fieldset class="group">
		<legend>Groups</legend>
		<div class="join flex-wrap">
			{#each FORMATION as [v, l] (v)}
				<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.groups === v} disabled={v === 'categories' && settings.identity === 'anonymous'} onclick={() => (settings.groups = v)}>{l}</button>
			{/each}
		</div>
		<p class="small muted m-0 mt-1">{FORMATION_HINT[settings.groups]}{settings.groups === 'categories' && !classroomId ? ' Choose the classroom above.' : ''}{settings.identity === 'anonymous' ? ' Categories need participants who log in.' : ''}</p>
		{#if settings.groups !== 'off'}
			<div class="vstack mt-3 border-l-2 border-base-300 pl-4">
				<div>
					<p class="small font-semibold m-0 mb-1">Which answers count for the group</p>
					<div class="cards two">
						{#each ACCEPT as [v, t, d] (v)}
							<label class="opt compact" class:on={settings.group_acceptance === v}>
								<input type="radio" class="sr-only" name="group_acceptance" value={v} bind:group={settings.group_acceptance} />
								<span><span class="block font-semibold">{t}</span><span class="small muted block">{d}</span></span>
							</label>
						{/each}
					</div>
				</div>
				{#if settings.group_acceptance === 'all'}
					<div>
						<p class="small font-semibold m-0 mb-1">Group mark</p>
						<div class="join flex-wrap">
							{#each CALC as [v, l] (v)}
								<button type="button" class="btn btn-sm join-item" class:btn-primary={settings.group_calc === v} onclick={() => (settings.group_calc = v)}>{l}</button>
							{/each}
						</div>
						<p class="small muted m-0 mt-1">Average and lowest count members who didn't answer as 0.</p>
					</div>
				{/if}
				{#if !settings.scoring}<p class="small muted m-0">Turn on <strong>Score answers</strong> for a group leaderboard.</p>{/if}
			</div>
		{/if}
	</fieldset>
</div>

<style>
	.group { border: 0; padding: 0; margin: 0; }
	.group legend { font-weight: 650; margin-bottom: 0.4rem; padding: 0; }
	.cards.three { grid-template-columns: repeat(auto-fit, minmax(min(11rem, 100%), 1fr)); }
	.opt.compact { padding: 0.6rem 0.75rem; }
	.cards { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fit, minmax(min(14rem, 100%), 1fr)); }
	.opt { display: flex; gap: 0.75rem; align-items: flex-start; margin: 0; padding: 0.75rem; border-radius: var(--radius-box); border: 1.5px solid var(--color-base-300); background: var(--color-base-100); cursor: pointer; font-weight: 400; transition: border-color var(--motion-fast), background-color var(--motion-fast); }
	.opt:hover { border-color: color-mix(in oklab, var(--color-primary) 45%, var(--color-base-300)); }
	.opt:has(input:focus-visible) { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.opt.on { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 7%, var(--color-base-100)); }
	.opt-icon { width: 2.1rem; height: 2.1rem; flex: none; display: grid; place-items: center; border-radius: 0.55rem; background: var(--color-base-200); color: var(--color-muted); }
	.on .opt-icon { background: var(--color-primary); color: var(--color-primary-content); }
	fieldset:disabled .opt { opacity: 0.6; cursor: not-allowed; }
</style>
