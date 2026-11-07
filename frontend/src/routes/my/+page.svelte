<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { promptDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import MyFiles from '$lib/drive/MyFiles.svelte';
	import MyPresentations from '$lib/presentation/MyPresentations.svelte';
	import MyTutorials from '$lib/tutorial/MyTutorials.svelte';
	import MyTrends from '$lib/trends/MyTrends.svelte';
	import MyRecordings from '$lib/tutoring/MyRecordings.svelte';
	import UpcomingTutoring from '$lib/tutoring/UpcomingTutoring.svelte';
	import MyAttendance from '$lib/tutoring/MyAttendance.svelte';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { STATE_BADGE, STATE_ICON, STATE_LABEL } from '$lib/types';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import ProgressCard from '$lib/progress/ProgressCard.svelte';
	import Celebration from '$lib/progress/Celebration.svelte';
	import { news, progress, type Badge } from '$lib/progress/progress.svelte';

	// Their progress (D-97), and a celebration for what's new since they last looked.
	let celebrate = $state<{ levelUp: boolean; badges: Badge[] } | null>(null);
	$effect(() => {
		if (auth.user?.role !== 'student') return;
		progress.load(true).then(() => {
			if (progress.current) celebrate = news(progress.current);
		});
	});

	type Attempt = { attempt_id: string; session_title: string; quiz_title: string; state: string; released: boolean; created_at: string };
	type Cls = { classroom_id: string; name: string; status: string; student_number: string | null; can_edit_id?: boolean };
	// Your own student ID, where the teacher allows it (D-134).
	async function changeID(c: Cls) {
		const v = await promptDialog({ title: c.student_number ? tr("Change your student ID") : tr("Add your student ID"), label: tr("Student ID in {0}", [c.name]), value: c.student_number ?? '', maxlength: 64, confirm: tr("Save"), body: tr("Use the ID your school gave you. Quizzes you’ve already taken keep the ID they were taken with.") });
		if (v === null || v.trim() === (c.student_number ?? '')) return;
		try {
			await api.put(`/api/my/classrooms/${c.classroom_id}/student-number`, { student_number: v });
			toast(tr("Student ID saved"));
			classes.load();
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : tr("Could not save it"), 'error');
		}
	}
	// Results and classrooms, one page at a time (PL-FR-02 "My results").
	const attempts = new Paged<Attempt>(() => '/api/my/attempts', 'attempts', { url: urlState, sort: 'created', desc: true });
	const classes = new Paged<Cls>(() => '/api/my/classrooms', 'classrooms', { url: urlState, prefix: 'cls', sort: 'name' });
	// Running attempts are the newest, so the first page shows them; keep them while paging back.
	let liveNow = $state<Attempt[]>([]);

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) return void goto(loginUrl('/my'));
		attempts.load();
		classes.load();
	});
	const live = (s: string) => ['waiting', 'admitted', 'in_progress', 'paused'].includes(s);
	// "16 Oct, 3:55 PM": short enough to sit on one line on a phone.
	const when = (iso: string) => new Date(iso).toLocaleString(localeTag(), { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
	$effect(() => {
		if (attempts.loaded && attempts.page === 1 && !attempts.q) liveNow = attempts.rows.filter((a) => live(a.state));
	});
</script>

<div class="page-container vstack">
	<div class="row"><h1 style="margin:0">{tr("My quizzes")}</h1><span class="spacer"></span><a class="btn btn-primary" href="/join">{tr("Join with a code")}</a></div>
	{#if auth.user?.role === 'student' && progress.current}<ProgressCard p={progress.current} />{/if}
	<UpcomingTutoring />
	{#each liveNow as a (a.attempt_id)}
		<a class="card card-border interactive live-card p-4 text-inherit no-underline shadow-sm sm:p-5" href={'/attempt/' + a.attempt_id} in:flyIn>
			<div class="flex items-center gap-3">
				<span class="grid size-11 flex-none place-items-center rounded-full bg-primary text-primary-content" aria-hidden="true"><Icon name="play" size={20} /></span>
				<div class="min-w-0 flex-1">
					<p class="small m-0 font-semibold text-primary">{tr("Live now · {0}", [STATE_LABEL[a.state]])}</p>
					<p class="m-0 truncate text-lg font-semibold">{a.session_title}</p>
				</div>
				<span class="btn btn-primary">{tr("Continue")}<Icon name="arrow-right" size={16} /></span>
			</div>
		</a>
	{/each}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		{#if !attempts.loaded}
			<Skeleton card={false} lines={3} />
		{:else if attempts.total === 0 && !attempts.q}
			<EmptyState icon="qr" title={tr("No quizzes yet")}>{tr("Scan the QR code your teacher shows, or")} <a href="/join">{tr("enter the join code")}</a>.</EmptyState>
		{:else}
			<div class="mb-3"><ListSearch list={attempts} placeholder={tr("Search quizzes")} label={tr("Search my quizzes")} /></div>
			<!-- A list, not a table: three columns don't fit a phone, where students use this page. -->
			<ul class="attempts" aria-label={tr("My quizzes")}>
				{#each attempts.rows as a (a.attempt_id)}
					<li>
						<div class="min-w-0">
							<p class="m-0 truncate font-semibold">{a.session_title}</p>
							<p class="small muted m-0"><time datetime={a.created_at}>{when(a.created_at)}</time></p>
						</div>
						<div class="end">
							<span class="badge badge-soft whitespace-nowrap {STATE_BADGE[a.state] ?? ''}"><span aria-hidden="true">{STATE_ICON[a.state] ?? ''}</span> {STATE_LABEL[a.state] ?? a.state}</span>
							{#if live(a.state)}<a class="btn btn-sm btn-primary" href={'/attempt/' + a.attempt_id}>{tr("Continue")}</a>
							{:else if a.released}<a class="btn btn-sm" href={'/my/results/' + a.attempt_id}>{tr("Results")}</a>
							{:else}<span class="small muted whitespace-nowrap">{tr("Results not out yet")}</span>{/if}
						</div>
					</li>
				{:else}<li class="muted">{tr("No quizzes match.")}</li>
				{/each}
			</ul>
			<div class="mt-3"><Pager list={attempts} label={tr("My quizzes")} /></div>
		{/if}
	</div>
	{#if auth.user?.role === 'student'}<MyTrends />{/if}
	<MyTutorials />
	<MyPresentations />
	<MyRecordings meId={auth.user?.id ?? ''} />
	<MyAttendance />
	<h2>{tr("My classrooms")}</h2>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		{#if classes.rows.length}
			<ul class="attempts" aria-label={tr("My classrooms")}>
				{#each classes.rows as c (c.classroom_id)}
					<li>
						<div class="min-w-0">
							<p class="m-0 truncate font-semibold">{#if c.status === 'active'}<a href={'/my/classrooms/' + c.classroom_id}>{c.name}</a>{:else}{c.name}{/if}</p>
							{#if c.student_number || c.can_edit_id}
								<p class="small muted m-0 flex flex-wrap items-center gap-2">
									{#if c.student_number}<span>{tr("Your student ID:")} <strong class="tabular">{c.student_number}</strong></span>{:else}<span>{tr("No student ID yet")}</span>{/if}
									{#if c.can_edit_id}<button class="btn btn-ghost btn-xs" onclick={() => changeID(c)}><Icon name="pencil" size={12} />{c.student_number ? tr("Change") : tr("Add")}</button>{/if}
								</p>
							{/if}
							{#if c.status === 'active'}<MyFiles classroomId={c.classroom_id} />{/if}
						</div>
						{#if c.status === 'pending'}<div class="end"><span class="badge badge-soft badge-warning whitespace-nowrap">{tr("Waiting for approval")}</span></div>{:else}<div class="end"><a class="btn btn-sm" href={'/my/classrooms/' + c.classroom_id}>{tr("Due and completed")}</a></div>{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="muted m-0">{classes.loaded ? tr("You haven’t joined a classroom yet. Your teacher can give you a classroom code.") : tr("Loading…")}</p>
		{/if}
		<Pager list={classes} label={tr("My classrooms")} />
	</div>
</div>

{#if celebrate && progress.current && (celebrate.levelUp || celebrate.badges.length)}
	<Celebration p={progress.current} levelUp={celebrate.levelUp} badges={celebrate.badges} onclose={() => (celebrate = null)} />
{/if}

<style>
	.attempts { list-style: none; margin: 0; padding: 0; }
	.attempts li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 0.5rem 1rem; padding: 0.75rem 0; border-bottom: 1px solid var(--color-base-300); }
	.attempts li:last-child { border-bottom: 0; }
	.attempts li > .min-w-0 { flex: 1 1 12rem; }
	.end { display: flex; align-items: center; gap: 0.5rem; margin-left: auto; }
</style>
