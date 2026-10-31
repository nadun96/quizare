<script lang="ts">
	// Each person's dashboard (D-99): their profile at a glance, their classes,
	// and what's live, coming up, overdue or missed, and waiting, each list a
	// page at a time. Tutoring sessions come from the tutoring service.
	import { goto } from '$app/navigation';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import Avatar from '$lib/ui/Avatar.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import DashboardList from '$lib/dashboard/DashboardList.svelte';
	import { LISTS, type Summary } from '$lib/dashboard/dashboard';
	import UpcomingTutoring from '$lib/tutoring/UpcomingTutoring.svelte';
	import InsightsPanel from '$lib/dashboard/InsightsPanel.svelte';
	import { t } from '$lib/i18n/i18n.svelte';
	import LevelRing from '$lib/progress/LevelRing.svelte';
	import { levelFraction, progress } from '$lib/progress/progress.svelte';

	type Cls = { id?: string; classroom_id?: string; name: string; status?: string; archived?: boolean };
	let summary = $state<Summary | null>(null);
	let classes = $state<Paged<Cls> | null>(null);

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) return void goto(loginUrl('/dashboard'));
		if (auth.user.role !== 'student' && auth.user.role !== 'teacher') return void goto(auth.home());
		api.get<Summary>('/api/my/dashboard').then((s) => (summary = s));
		const teacher = auth.user.role === 'teacher';
		const c = new Paged<Cls>(() => (teacher ? '/api/teacher/classrooms' : '/api/my/classrooms'), 'classrooms', { url: urlState, prefix: 'cls', sort: 'name' });
		c.load();
		classes = c;
		if (!teacher) progress.load();
	});
	const teacher = $derived(auth.user?.role === 'teacher');
	const greeting = $derived.by(() => {
		const h = new Date().getHours();
		return h < 12 ? t('dash.morning') : h < 17 ? t('dash.afternoon') : t('dash.evening');
	});

	// "Amaya" from "Amaya Silva", but "Ms Perera" whole: a title alone isn't a name.
	const TITLES = new Set(['mr', 'mrs', 'ms', 'miss', 'dr', 'prof', 'rev', 'sir', 'madam']);
	function callName(full: string) {
		const words = full.trim().split(/\s+/);
		return TITLES.has(words[0]?.toLowerCase().replace(/\.$/, '')) ? words.slice(0, 2).join(' ') : words[0];
	}

	async function resend() {
		try {
			await api.post('/api/auth/me/email/verify');
			toast(t('dash.linkSent'));
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'Could not send the link', 'error');
		}
	}
</script>

<svelte:head><title>Dashboard</title></svelte:head>

{#if auth.user}
	<div class="page-container vstack">
		<header class="hello">
			{#if !teacher && progress.current}
				<a href="/my" class="no-underline" aria-label="Your progress: level {progress.current.level}">
					<LevelRing fraction={levelFraction(progress.current)} level={progress.current.level} size={64} stroke={4}>
						<Avatar id={auth.user.id} name={auth.user.name} avatar={auth.user.avatar} size={54} />
					</LevelRing>
				</a>
			{:else}
				<Avatar id={auth.user.id} name={auth.user.name} avatar={auth.user.avatar} size={56} />
			{/if}
			<div class="min-w-0 flex-1">
				<h1 class="m-0 truncate">{greeting}, {callName(auth.user.name)}</h1>
				<p class="m-0 small muted truncate">{auth.user.email}{' · '}<a href="/account">{t('dash.editProfile')}</a></p>
			</div>
			{#if teacher}<a class="btn btn-primary" href="/t">{t('nav.classrooms')}</a>{:else}<a class="btn btn-primary" href="/join">{t('dash.joinCode')}</a>{/if}
		</header>

		{#if !auth.user.email_verified}
			<div class="alert alert-soft alert-warning" role="status">
				<Icon name="alert" size={18} />
				<span class="flex-1">{t('dash.unverified')}</span>
				<button class="btn btn-sm" onclick={resend}>{t('dash.sendAgain')}</button>
			</div>
		{/if}

		{#if summary}
			<nav class="counts" aria-label={t('dash.summary')}>
				{#each LISTS as l (l.id)}
					<a href="#dash-{l.id}" class="count" class:alert-count={l.id === 'overdue' && summary.counts.overdue > 0}>
						<strong>{summary.counts[l.id] ?? 0}</strong><span>{t(teacher ? l.teacherTitle : l.title)}</span>
					</a>
				{/each}
			</nav>
		{/if}

		<InsightsPanel {teacher} />

		<UpcomingTutoring />

		<div class="grid-2">
			{#each LISTS as l (l.id)}
				<DashboardList name={l.id} title={t(teacher ? l.teacherTitle : l.title)} empty={t(teacher ? l.teacherEmpty : l.empty)} />
			{/each}
		</div>

		<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2" aria-labelledby="dash-classes">
			<h2 id="dash-classes" class="m-0 text-lg">{teacher ? t('dash.yourClassrooms') : t('dash.yourClasses')}</h2>
			{#if !classes || !classes.loaded}
				<Skeleton card={false} lines={2} />
			{:else if !classes.rows.length}
				<p class="m-0 small muted">{teacher ? t('dash.noClassrooms') : t('dash.noClasses')}</p>
			{:else}
				<ul class="classes">
					{#each classes.rows as c (c.id ?? c.classroom_id)}
						<li>
							{#if teacher}<a href={'/t/classrooms/' + c.id}>{c.name}</a>{:else}<span>{c.name}</span>{/if}
							{#if c.status === 'pending'}<span class="badge badge-soft badge-warning badge-sm">{t('dash.waitingApproval')}</span>{/if}
							{#if c.archived}<span class="badge badge-soft badge-sm">{t('dash.archived')}</span>{/if}
						</li>
					{/each}
				</ul>
				<Pager list={classes} label="Classes" />
			{/if}
		</section>
	</div>
{/if}

<style>
	.hello { display: flex; align-items: center; gap: 1rem; flex-wrap: wrap; }
	.hello > div { flex-basis: 14rem; }
	@media (max-width: 560px) { .hello > .btn { width: 100%; } }
	.counts { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 0.5rem; }
	.count { display: grid; gap: 0.1rem; padding: 0.75rem 0.9rem; border-radius: var(--radius-box); background: var(--color-base-100); border: 1px solid var(--color-base-300); text-decoration: none; color: var(--color-base-content); }
	.count strong { font-size: 1.5rem; line-height: 1; font-variant-numeric: tabular-nums; }
	.count span { font-size: 0.8rem; color: color-mix(in oklab, var(--color-base-content) 70%, transparent); }
	.count.alert-count strong { color: var(--color-error); }
	.grid-2 > :global(*) { margin: 0; }
	.grid-2 { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(min(100%, 26rem), 1fr)); align-items: start; }
	.classes { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.4rem; grid-template-columns: repeat(auto-fill, minmax(min(100%, 14rem), 1fr)); }
	.classes li { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; padding: 0.55rem 0.75rem; border-radius: 0.6rem; background: var(--color-base-200); }
	@media (max-width: 560px) { .counts { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
