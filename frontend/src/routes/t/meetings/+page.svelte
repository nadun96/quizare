<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Meetings (D-69): small peer-to-peer video calls, open to anyone signed
	// in with the code, or to one classroom's students.
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { sizeHint, sizeOptions, type Attendance, type Meeting, type MeetingLimits } from '$lib/meet/mesh';
	import type { Classroom } from '$lib/types';
	import TopicSelect from '$lib/classroom/TopicSelect.svelte';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import IconBtn from '$lib/ui/IconBtn.svelte';
	import OptionGroup from '$lib/ui/OptionGroup.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { toast } from '$lib/ui/toast.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import { count as words } from '$lib/words';

	const ready = requireRole('teacher');
	const meetings = new Paged<Meeting>(() => '/api/teacher/meetings', 'meetings', { url: urlState, sort: 'created', desc: true });
	let classrooms = $state<Classroom[]>([]);
	let creating = $state(false);
	let title = $state('');
	let who = $state<'anyone' | 'classroom'>('anyone');
	let classroomId = $state('');
	let topicId = $state<string | null>(null);
	let size = $state('6');
	// What the admin allows (D-72; 0 is unlimited).
	let limits = $state<MeetingLimits>({ max_people: 8, max_open: 0 });
	const choices = $derived(sizeOptions(limits.max_people));
	let error = $state('');
	let busy = $state(false);
	let open = $state<Record<string, Attendance[] | null>>({});

	$effect(() => {
		if (!ready()) return;
		meetings.load();
		api.get<{ classrooms: Classroom[] }>('/api/teacher/classrooms?size=100&sort=name').then((r) => (classrooms = r.classrooms ?? []));
		api.get<MeetingLimits>('/api/teacher/meeting-limits').then((l) => {
			limits = l;
			size = String(sizeOptions(l.max_people).initial);
		});
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const m = await api.post<Meeting>('/api/teacher/meetings', { title, classroom_id: who === 'classroom' && classroomId ? classroomId : null, topic_id: who === 'classroom' && classroomId ? topicId : null, max_people: Number(size) });
			goto('/m/' + m.code);
		} catch (err) {
			error = err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("Could not open the meeting");
		} finally {
			busy = false;
		}
	}

	async function end(m: Meeting) {
		if (!(await confirmDialog({ title: tr("End “{0}”?", [m.title]), body: tr("Everyone in it leaves, and the link stops working."), confirm: tr("End meeting"), danger: true }))) return;
		await api.post('/api/teacher/meetings/' + m.id + '/end');
		toast(tr("Meeting ended"));
		meetings.load();
	}

	async function attendance(m: Meeting) {
		if (m.id in open) {
			const { [m.id]: _, ...rest } = open;
			open = rest;
			return;
		}
		open = { ...open, [m.id]: null };
		const r = await api.get<{ attendance: Attendance[] }>('/api/teacher/meetings/' + m.id + '/attendance');
		open = { ...open, [m.id]: r.attendance };
	}

	async function copy(m: Meeting) {
		const url = location.origin + '/m/' + m.code;
		try {
			await navigator.clipboard.writeText(url);
			toast(tr("Link copied"));
		} catch {
			toast(url);
		}
	}

	const roomName = (id: string | null) => classrooms.find((c) => c.id === id)?.name ?? 'A classroom';
	const time = (s: string) => new Date(s).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
	const clock = (s: string) => new Date(s).toLocaleTimeString([], { timeStyle: 'short' });
</script>

<div class="page-container vstack">
	<div class="row">
		<h1 class="m-0">{tr("Meetings")}</h1>
		<span class="spacer"></span>
		{#if !creating}<button class="btn btn-primary" onclick={() => (creating = true)}><Icon name="plus" size={16} />{tr("New meeting")}</button>{/if}
	</div>
	<p class="muted m-0">{tr("Small video calls for office hours, group work or a quick catch-up. Video goes straight between the people in the call.")}</p>

	{#if creating}
		<form class="card card-border vstack bg-base-100 p-4 shadow-sm sm:p-6" onsubmit={create} in:flyIn>
			<div><label for="nm-title">{tr("Title")}</label><input id="nm-title" class="input w-full" bind:value={title} required maxlength="120" placeholder={tr("e.g. Thursday office hours")} /></div>
			<div class="vstack gap-1">
				<span class="font-semibold">{tr("Who can join")}</span>
				<OptionGroup label={tr("Who can join")} value={who} onchange={(v) => (who = v as typeof who)} options={[
					{ v: 'anyone', label: 'Anyone with the code', icon: 'link', hint: 'Signed in, with the link or code' },
					{ v: 'classroom', label: 'One classroom', icon: 'users', hint: 'You and its enrolled students' }
				]} />
				{#if who === 'classroom'}
					<select class="select w-full sm:w-80" bind:value={classroomId} required aria-label={tr("Classroom")}>
						<option value="" disabled>{tr("Choose a classroom")}</option>
						{#each classrooms as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
					</select>
					{#if classroomId}<TopicSelect {classroomId} bind:value={topicId} />{/if}
				{/if}
			</div>
			<div class="vstack gap-1">
				<span class="font-semibold">{tr("Size")}</span>
				<OptionGroup label={tr("Size")} value={size} onchange={(v) => (size = v)} options={choices.sizes.map((n) => ({
					v: String(n), label: n ? `Up to ${n}` : 'No limit', icon: n && n <= 4 ? 'user' : 'users', hint: sizeHint(n)
				}))} />
			</div>
			{#if error}<p class="field-error" role="alert">{error}</p>{/if}
			<div class="flex gap-2">
				<button class="btn btn-primary" disabled={busy}><Icon name="video" size={16} />{tr("Open and join")}</button>
				<button type="button" class="btn" onclick={() => (creating = false)}>{tr("Cancel")}</button>
			</div>
		</form>
	{/if}

	<div class="flex flex-wrap items-center gap-3">
		<ListSearch list={meetings} placeholder={tr("Search title or code")} label={tr("Search meetings")} />
	</div>
	{#if !meetings.loaded}
		<div class="auto-grid"><Skeleton lines={2} /><Skeleton lines={2} /></div>
	{:else if meetings.total === 0 && !creating && !meetings.q}
		<div class="card card-border bg-base-100"><EmptyState icon="video" title={tr("No meetings yet")}>{tr("Open one and share its link or code. Students join from the Join page.")}</EmptyState></div>
	{:else}
		<div class="vstack">
			{#each meetings.rows as m, i (m.id)}
				<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-5" in:flyIn={{ delay: Math.min(i, 8) * 40 }}>
					<div class="flex flex-wrap items-start gap-3">
						<span class="grid size-10 flex-none place-items-center rounded-xl bg-primary/10 text-primary" aria-hidden="true"><Icon name={m.status === 'open' ? 'video' : 'video-off'} size={20} /></span>
						<div class="min-w-0 flex-1">
							<h3 class="m-0 truncate">{m.title}</h3>
							<p class="small muted m-0 mt-1">
								{#if m.status === 'open'}<span class="badge badge-soft badge-sm ok">{tr("Open")}</span>{:else}<span class="badge badge-soft badge-sm">{tr("Ended")}</span>{/if}
								{' · '}{m.classroom_id ? roomName(m.classroom_id) : tr("Anyone with the code")}
								{' · '}{m.max_people ? tr("up to {0}", [m.max_people]) : tr("no size limit")}
								{#if m.status === 'open'}{' · '}<span class="tabular">{m.online}</span> {tr("in now")}{/if}
							</p>
							<p class="small muted m-0 mt-1">{tr("Code")} <strong class="tabular">{m.code}</strong>{' · '}{time(m.created_at)}</p>
						</div>
						<div class="flex flex-wrap items-center gap-1">
							{#if m.status === 'open'}
								<a class="btn btn-sm btn-primary" href={'/m/' + m.code}><Icon name="video" size={16} />{tr("Join")}</a>
								<IconBtn icon="copy" label={tr("Copy link")} hint={tr("Copy the link to {0}", [m.title])} onclick={() => copy(m)} />
								<IconBtn icon="x" label={tr("End")} hint={tr("End {0}", [m.title])} onclick={() => end(m)} />
							{/if}
							<button class="btn btn-sm btn-ghost" aria-expanded={m.id in open} onclick={() => attendance(m)}><Icon name="users" size={16} />{tr("Who came")}</button>
						</div>
					</div>
					{#if m.id in open}
						{@const rows = open[m.id]}
						{#if !rows}<Skeleton lines={2} />
						{:else if !rows.length}<p class="small muted m-0 mt-3">{tr("Nobody has joined yet.")}</p>
						{:else}
							<div class="overflow-x-auto mt-3">
								<table class="table table-sm">
									<thead><tr><th>{tr("Name")}</th><th>{tr("First joined")}</th><th>{tr("Last seen")}</th><th>{tr("Joins")}</th></tr></thead>
									<tbody>
										{#each rows as a (a.user_id)}
											<tr><td>{a.name}{#if a.online} <span class="badge badge-soft badge-xs ok">{tr("In now")}</span>{/if}</td><td class="tabular">{clock(a.first_joined)}</td><td class="tabular">{a.online ? '—' : clock(a.last_seen)}</td><td class="tabular">{a.joins}</td></tr>
										{/each}
									</tbody>
								</table>
							</div>
							<p class="small muted m-0 mt-1">{tr("{0} joined.", [words(rows.length, 'person', 'people')])}</p>
						{/if}
					{/if}
				</div>
			{:else}<p class="muted">{tr("No meetings match.")}</p>
			{/each}
		</div>
		<Pager list={meetings} label={tr("Meetings")} />
	{/if}
</div>
