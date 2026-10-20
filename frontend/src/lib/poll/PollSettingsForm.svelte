<script lang="ts">
	// How a poll runs: who is identified, who may answer, pacing, and what
	// participants see (D-40). One row per setting: icon-and-label choices,
	// with only the selected choice explained (D-63).
	import Icon from '../ui/Icon.svelte';
	import OptionGroup, { type Choice } from '../ui/OptionGroup.svelte';
	import type { Classroom } from '../types';
	import type { PollSettings } from './types';

	let { settings = $bindable(), classroomId = $bindable(null), classrooms = [], locked = false }: { settings: PollSettings; classroomId?: string | null; classrooms?: Classroom[]; locked?: boolean } = $props();

	$effect(() => {
		if (settings.audience === 'classroom' && settings.identity !== 'identified') settings.audience = 'anyone';
		if (settings.show_results === 'presenter' && settings.pacing !== 'presenter') settings.show_results = 'after_answer';
		if (settings.show_answers === 'presenter' && settings.pacing !== 'presenter') settings.show_answers = 'after_close';
		if (settings.names === 'name' && settings.identity === 'anonymous') settings.names = 'nickname';
		if (settings.groups === 'categories' && settings.identity === 'anonymous') settings.groups = 'manual';
	});

	const presenter = $derived(settings.pacing === 'presenter');
	const anon = $derived(settings.identity === 'anonymous');
	const NEEDS_PRESENTER = 'Needs presenter-led pacing';

	const identity = $derived<Choice[]>([
		{ v: 'anonymous', label: 'Anonymous', icon: 'eye-off', hint: 'No login. Nobody, not even you, can see who answered what.', disabled: locked },
		{ v: 'identified', label: 'Identified', icon: 'user', hint: 'Participants log in; you see names next to answers. Classmates never do.', disabled: locked },
		{ v: 'optional', label: 'They choose', icon: 'users', hint: 'Logged-in participants may put their name to their answers, or stay anonymous.', disabled: locked }
	]);
	const audience = $derived<Choice[]>([
		{ v: 'anyone', label: 'Anyone with the code', icon: 'qr' },
		{ v: 'classroom', label: 'One classroom', icon: 'book', hint: 'Only students enrolled in the classroom below.', disabled: settings.identity !== 'identified', why: 'Needs identified participants' }
	]);
	const pacing: Choice[] = [
		{ v: 'self', label: 'Self-paced', icon: 'menu', hint: 'Everyone sees all questions and answers in any order.' },
		{ v: 'presenter', label: 'Presenter-led', icon: 'play', hint: 'You move everyone through one question at a time from the screen.' }
	];
	const results = $derived<Choice[]>([
		{ v: 'live', label: 'Always', icon: 'chart', hint: 'Participants watch the results update live.' },
		{ v: 'after_answer', label: 'After answering', icon: 'check', hint: 'Each participant sees the results once they’ve answered.' },
		{ v: 'presenter', label: 'When I reveal', icon: 'eye', hint: 'Results appear when you reveal them on the presenter screen.', disabled: !presenter, why: NEEDS_PRESENTER },
		{ v: 'never', label: 'Never', icon: 'eye-off', hint: 'Only you see results.' }
	]);
	const editing: Choice[] = [
		{ v: 'true', label: 'Allowed', icon: 'pencil', hint: 'Participants can change their answers while the poll is open.' },
		{ v: 'false', label: 'Locked', icon: 'check', hint: 'An answer is final once sent.' }
	];
	const scoring: Choice[] = [
		{ v: 'false', label: 'Off', icon: 'x' },
		{ v: 'true', label: 'Score answers', icon: 'trophy', hint: 'Questions with a correct answer earn points (100 by default). Opinion questions stay unscored.' }
	];
	const speed = $derived<Choice[]>([
		{ v: 'false', label: 'Same points', icon: 'check' },
		{ v: 'true', label: 'Faster earns more', icon: 'clock', hint: 'On timed questions, a right answer earns 50–100% of its points: the faster, the more.', disabled: !presenter, why: NEEDS_PRESENTER }
	]);
	const board: Choice[] = [
		{ v: 'presenter', label: 'My screen', icon: 'monitor', hint: 'Only you see the ranking, on the presenter screen and here.' },
		{ v: 'everyone', label: 'Everyone', icon: 'users', hint: 'Participants see the top 10 and their own place, live.' },
		{ v: 'off', label: 'Off', icon: 'eye-off', hint: 'Points are kept for you; nobody sees a ranking.' }
	];
	const answers = $derived<Choice[]>([
		{ v: 'after_answer', label: 'After answering', icon: 'check' },
		{ v: 'presenter', label: 'When I reveal', icon: 'eye', disabled: !presenter, why: NEEDS_PRESENTER },
		{ v: 'after_close', label: 'When it closes', icon: 'clock' },
		{ v: 'never', label: 'Never', icon: 'eye-off' }
	]);
	const names = $derived<Choice[]>([
		{ v: 'nickname', label: 'Nicknames', icon: 'user', hint: 'Participants choose one when they join, or appear as “Participant N”. You can rename anyone.' },
		{ v: 'name', label: 'Real names', icon: 'users', hint: 'Logged-in participants appear under their own names.', disabled: anon, why: 'Needs participants who log in' }
	]);
	const groups = $derived<Choice[]>([
		{ v: 'off', label: 'None', icon: 'x', hint: 'Everyone answers for themselves.' },
		{ v: 'manual', label: 'I choose', icon: 'pencil', hint: 'Create groups and move participants into them on the Groups tab.' },
		{ v: 'random', label: 'Random', icon: 'shuffle', hint: 'Each person who joins goes to the smallest group. You can also shuffle everyone later.' },
		{ v: 'categories', label: 'Categories', icon: 'list', hint: 'One group per category of the poll’s classroom.' + (classroomId ? '' : ' Choose the classroom above.'), disabled: anon, why: 'Needs participants who log in' },
		{ v: 'self', label: 'They choose', icon: 'users', hint: 'Participants pick a group from your list when they join.' }
	]);
	const accept: Choice[] = [
		{ v: 'all', label: 'Everyone’s', icon: 'users', hint: 'Each member answers; the group’s mark combines them.' },
		{ v: 'first', label: 'First answer', icon: 'skip-forward', hint: 'The group’s first answer counts; teammates can’t answer after it.' },
		{ v: 'captain', label: 'Captain’s', icon: 'star', hint: 'Only the captain answers. The first to join is captain; you can change it.' },
		{ v: 'best', label: 'Best', icon: 'trophy', hint: 'Each member answers; the group gets its best member’s mark.' }
	];
	const calc: Choice[] = [
		{ v: 'sum', label: 'Total', icon: 'plus' },
		{ v: 'average', label: 'Average', icon: 'minus', hint: 'Members who don’t answer count as 0.' },
		{ v: 'max', label: 'Highest', icon: 'arrow-up' },
		{ v: 'min', label: 'Lowest', icon: 'arrow-down', hint: 'Members who don’t answer count as 0.' }
	];
</script>

{#snippet row(label: string, hint: string, body: import('svelte').Snippet)}
	<div class="srow">
		<div class="slabel"><span class="font-semibold">{label}</span>{#if hint}<span class="small muted">{hint}</span>{/if}</div>
		<div class="scontrol">{@render body()}</div>
	</div>
{/snippet}

<div class="vstack">
	<section class="block">
		<h3 class="b-title"><Icon name="users" size={16} />Who takes part</h3>
		{#snippet identityC()}<OptionGroup label="Identity" options={identity} value={settings.identity} onchange={(v) => (settings.identity = v as PollSettings['identity'])} />{/snippet}
		{@render row('Identity', locked ? 'Fixed now that people have joined. Reset the responses to change it.' : '', identityC)}
		{#snippet audienceC()}
			<OptionGroup label="Who can answer" options={audience} value={settings.audience} onchange={(v) => (settings.audience = v as PollSettings['audience'])} />
			{#if settings.audience === 'classroom' || classrooms.length}
				<select class="select select-sm w-full max-w-xs" aria-label="Classroom" value={classroomId ?? ''} onchange={(e) => (classroomId = e.currentTarget.value || null)}>
					<option value="">{settings.audience === 'classroom' ? 'Choose a classroom' : 'No classroom'}</option>
					{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
				</select>
			{/if}
		{/snippet}
		{@render row('Who can answer', settings.audience === 'classroom' ? '' : 'A classroom here is only for your own organising.', audienceC)}
	</section>

	<section class="block">
		<h3 class="b-title"><Icon name="play" size={16} />How it runs</h3>
		{#snippet pacingC()}<OptionGroup label="Pacing" options={pacing} value={settings.pacing} onchange={(v) => (settings.pacing = v as PollSettings['pacing'])} />{/snippet}
		{@render row('Pacing', '', pacingC)}
		{#snippet resultsC()}<OptionGroup label="Participants see results" options={results} value={settings.show_results} onchange={(v) => (settings.show_results = v as PollSettings['show_results'])} />{/snippet}
		{@render row('Participants see results', 'You always see live results. Participants never see names.', resultsC)}
		{#snippet editC()}<OptionGroup label="Changing answers" options={editing} value={String(settings.allow_edit)} onchange={(v) => (settings.allow_edit = v === 'true')} />{/snippet}
		{@render row('Changing answers', '', editC)}
	</section>

	<section class="block">
		<h3 class="b-title"><Icon name="trophy" size={16} />Competition</h3>
		{#snippet scoreC()}<OptionGroup label="Points" options={scoring} value={String(settings.scoring)} onchange={(v) => (settings.scoring = v === 'true')} />{/snippet}
		{@render row('Points', '', scoreC)}
		{#if settings.scoring}
			{#snippet speedC()}<OptionGroup label="Speed" options={speed} value={String(settings.speed_bonus)} onchange={(v) => (settings.speed_bonus = v === 'true')} />{/snippet}
			{@render row('Speed', '', speedC)}
			{#snippet boardC()}<OptionGroup label="Leaderboard" options={board} value={settings.leaderboard} onchange={(v) => (settings.leaderboard = v as PollSettings['leaderboard'])} />{/snippet}
			{@render row('Leaderboard', '', boardC)}
			{#snippet answersC()}<OptionGroup label="Correct answers shown" options={answers} value={settings.show_answers} onchange={(v) => (settings.show_answers = v as PollSettings['show_answers'])} />{/snippet}
			{@render row('Correct answers shown', '', answersC)}
			{#snippet namesC()}<OptionGroup label="Names on the leaderboard" options={names} value={settings.names} onchange={(v) => (settings.names = v as PollSettings['names'])} />{/snippet}
			{@render row('Names on the leaderboard', '', namesC)}
		{/if}
	</section>

	<section class="block">
		<h3 class="b-title"><Icon name="users" size={16} />Groups</h3>
		{#snippet groupsC()}<OptionGroup label="Groups" options={groups} value={settings.groups} onchange={(v) => (settings.groups = v as PollSettings['groups'])} />{/snippet}
		{@render row('Groups', '', groupsC)}
		{#if settings.groups !== 'off'}
			{#snippet acceptC()}<OptionGroup label="Answers that count" options={accept} value={settings.group_acceptance} onchange={(v) => (settings.group_acceptance = v as PollSettings['group_acceptance'])} />{/snippet}
			{@render row('Answers that count', '', acceptC)}
			{#if settings.group_acceptance === 'all'}
				{#snippet calcC()}<OptionGroup label="Group mark" options={calc} value={settings.group_calc} onchange={(v) => (settings.group_calc = v as PollSettings['group_calc'])} />{/snippet}
				{@render row('Group mark', '', calcC)}
			{/if}
			{#if !settings.scoring}<p class="small muted m-0 pt-2">Turn on <strong>Score answers</strong> for a group leaderboard.</p>{/if}
		{/if}
	</section>
</div>

<style>
	.block { display: grid; }
	.b-title { display: flex; align-items: center; gap: 0.45rem; margin: 0 0 0.25rem; font-size: 1rem; }
	.b-title :global(svg) { color: var(--color-primary); }
	.srow { display: grid; grid-template-columns: minmax(0, 13rem) minmax(0, 1fr); gap: 0.5rem 1.25rem; align-items: start; padding: 0.75rem 0; border-top: 1px solid var(--color-base-300); }
	@media (max-width: 40rem) { .srow { grid-template-columns: minmax(0, 1fr); } }
	.slabel { display: grid; gap: 0.15rem; padding-top: 0.4rem; }
	.scontrol { display: grid; gap: 0.5rem; justify-items: start; min-width: 0; }
</style>
