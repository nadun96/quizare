<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto, afterNavigate } from '$app/navigation';
	import { auth } from '$lib/session.svelte';
	import { prefs } from '$lib/ui/prefs.svelte';
	import { flyIn } from '$lib/ui/motion';
	import Icon from '$lib/ui/Icon.svelte';
	import DisplayMenu from '$lib/ui/DisplayMenu.svelte';
	import Avatar from '$lib/ui/Avatar.svelte';
	import Toaster from '$lib/ui/Toaster.svelte';
	import DialogHost from '$lib/ui/DialogHost.svelte';
	import { tutoringConfig } from '$lib/tutoring/client';
	import LevelRing from '$lib/progress/LevelRing.svelte';
	import { levelFraction, progress } from '$lib/progress/progress.svelte';
	import { reduced } from '$lib/ui/motion';
	import type { IconName } from '$lib/ui/Icon.svelte';

	let { children } = $props();

	prefs.load();
	let tutoring = $state(false); // the Tutoring link shows only when tutoring is on (TS-NFR-55)
	onMount(() => {
		auth.load();
	});
	$effect(() => {
		if (auth.user?.role === 'teacher') tutoringConfig().then((c) => (tutoring = c.enabled));
	});
	// Students' level ring around their avatar (D-97).
	$effect(() => {
		if (auth.user?.role === 'student') progress.load();
		else progress.clear();
	});

	// The live quiz and the projector are full-screen experiences without the nav.
	const bare = $derived(page.url.pathname.startsWith('/attempt/') || page.url.pathname.endsWith('/projector') || page.url.pathname.endsWith('/present') || page.url.pathname.startsWith('/r/') || page.url.pathname.startsWith('/live/') || page.url.pathname.startsWith('/m/'));
	const isDocs = $derived(page.url.pathname.startsWith('/docs'));

	type Link = { href: string; label: string; icon: IconName };
	const links = $derived<Link[]>(
		auth.user?.role === 'teacher'
			? [
					{ href: '/dashboard', label: 'Dashboard', icon: 'list' },
					{ href: '/t', label: 'Classrooms', icon: 'users' },
					{ href: '/t/bank', label: 'Question bank', icon: 'book' },
					{ href: '/t/polls', label: 'Polls', icon: 'chart' },
					{ href: '/t/meetings', label: 'Meetings', icon: 'video' },
					...(tutoring ? [{ href: '/t/tutoring', label: 'Tutoring', icon: 'monitor' as const }] : []),
				]
			: auth.user?.role === 'student'
				? [{ href: '/dashboard', label: 'Dashboard', icon: 'list' }, { href: '/my', label: 'My quizzes', icon: 'star' }, { href: '/join', label: 'Join', icon: 'qr' }]
				: auth.user?.role === 'admin'
					? [{ href: '/admin', label: 'Admin', icon: 'flag' }]
					: auth.user?.role === 'manager'
						? [{ href: '/admin', label: 'Manage', icon: 'flag' }]
						: []
	);
	// Phones: the first four in a tab bar along the bottom, the rest under More.
	const tabs = $derived(links.length > 5 ? links.slice(0, 4) : links);
	const more = $derived(links.length > 5 ? links.slice(4) : []);

	// The pill's highlight slides to the current page (it doesn't with reduced motion).
	let pill = $state<HTMLElement>();
	let mark = $state<{ left: number; width: number } | null>(null);
	$effect(() => {
		void page.url.pathname;
		void links.length;
		const el = pill?.querySelector<HTMLElement>('[aria-current="page"]');
		mark = el ? { left: el.offsetLeft, width: el.offsetWidth } : null;
	});
	// Classrooms (/t) owns every teacher page that no other item does.
	const under = (href: string) => page.url.pathname === href || page.url.pathname.startsWith(href + '/');
	const current = (href: string) => (href === '/t' ? under('/t') && !links.some((l) => l.href !== '/t' && under(l.href)) : under(href));

	// <details> menus are CSS-only; close them after a choice or navigation.
	let menus: HTMLDetailsElement[] = $state([]);
	afterNavigate(() => menus.forEach((m) => m && (m.open = false)));
	function closeOthers(e: Event) {
		const t = e.currentTarget as HTMLDetailsElement;
		if (t.open) menus.forEach((m) => m && m !== t && (m.open = false));
	}
	// Escape or a click outside closes an open menu (and Escape returns focus to it).
	function menuKeys(e: KeyboardEvent) {
		if (e.key !== 'Escape') return;
		const open = menus.find((m) => m?.open);
		if (!open) return;
		open.open = false;
		open.querySelector('summary')?.focus();
	}
	function menuClicks(e: MouseEvent) {
		for (const m of menus) if (m?.open && !m.contains(e.target as Node)) m.open = false;
	}

	async function logout() {
		progress.clear();
		await auth.logout();
		goto('/login');
	}
</script>

<svelte:window onkeydown={menuKeys} onclick={menuClicks} />

<a class="skip-link btn btn-primary btn-sm" href="#main">Skip to content</a>

{#if !bare}
	<header class="topbar sticky top-0 z-30">
		<div class="page-container flex items-center gap-2 py-2">
			<a class="brand flex min-w-0 items-center gap-2 whitespace-nowrap text-base font-bold no-underline sm:text-lg" href={auth.user ? auth.home() : '/'}>
				<img class="brand-mark size-8" src="/favicon-64.png" width="32" height="32" alt="" aria-hidden="true" />
				<span class="brand-name">Classroom Quiz</span>
			</a>
			{#if links.length}
				<nav class="pill ml-3 hidden sm:flex" aria-label="Main" bind:this={pill}>
					{#if mark}<span class="pill-mark" class:glide={!reduced()} style:transform="translateX({mark.left}px)" style:width="{mark.width}px" aria-hidden="true"></span>{/if}
					{#each links as l (l.href)}
						<a class="pill-link" class:on={current(l.href)} aria-current={current(l.href) ? 'page' : undefined} href={l.href}>
							<Icon name={l.icon} size={16} /><span class="pill-label">{l.label}</span>
						</a>
					{/each}
				</nav>
			{/if}
			<span class="spacer"></span>
			<details class="dropdown dropdown-end" bind:this={menus[1]} ontoggle={closeOthers}>
				<summary class="btn btn-ghost btn-circle" aria-label="Display settings"><Icon name="sun" size={20} /></summary>
				<div class="dropdown-content z-40 mt-2 w-80 max-w-[calc(100vw-2rem)] max-h-[calc(100dvh-5rem)] overflow-y-auto overscroll-contain rounded-box border border-base-300 bg-base-100 p-4 shadow-lg">
					<DisplayMenu idPrefix="nav" />
				</div>
			</details>
			{#if auth.user}
				<details class="dropdown dropdown-end" bind:this={menus[2]} ontoggle={closeOthers}>
					<summary class="btn btn-ghost h-auto min-h-0 gap-2 rounded-full px-1.5 py-1" aria-label={auth.user.role === 'student' && progress.current ? `Account menu, level ${progress.current.level}` : 'Account menu'}>
						{#if auth.user.role === 'student' && progress.current}
							<LevelRing fraction={levelFraction(progress.current)} level={progress.current.level} size={38}>
								<Avatar id={auth.user.id} name={auth.user.name} avatar={auth.user.avatar} size={30} />
							</LevelRing>
						{:else}
							<Avatar id={auth.user.id} name={auth.user.name} avatar={auth.user.avatar} size={32} />
						{/if}
						<span class="hidden max-w-40 truncate md:inline">{auth.user.name}</span>
					</summary>
					<ul class="menu dropdown-content z-40 mt-2 w-60 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
						<li class="menu-title truncate">{auth.user.email}</li>
						{#if auth.user.role === 'student' && progress.current}
							<li><a href="/my"><Icon name="sparkles" size={16} />Level {progress.current.level}, {progress.current.xp} XP</a></li>
						{/if}
						<li><a href="/account"><Icon name="user" size={16} />Profile and account</a></li>
						{#if auth.user.role === 'teacher'}
							<li><a href="/t/settings"><Icon name="pencil" size={16} />Teaching settings</a></li>
							{#if auth.user.manager}<li><a href="/admin"><Icon name="flag" size={16} />Manage the platform</a></li>{/if}
						{/if}
						<li><button onclick={logout}><Icon name="logout" size={16} />Log out</button></li>
					</ul>
				</details>
			{:else if auth.loaded}
				<a class="btn btn-ghost btn-sm" href="/login">Log in</a>
				<a class="btn btn-primary btn-sm" href="/register">Sign up</a>
			{/if}
		</div>
	</header>
	{#if links.length}
		<nav class="tabbar" aria-label="Main">
			{#each tabs as l (l.href)}
				<a class="tab-link" class:on={current(l.href)} aria-current={current(l.href) ? 'page' : undefined} href={l.href}>
					<span class="tab-icon"><Icon name={l.icon} size={20} /></span><span class="tab-label">{l.label}</span>
				</a>
			{/each}
			{#if more.length}
				<details class="dropdown dropdown-top dropdown-end tab-more" bind:this={menus[0]} ontoggle={closeOthers}>
					<summary class="tab-link" class:on={more.some((l) => current(l.href))}><span class="tab-icon"><Icon name="more" size={20} /></span><span class="tab-label">More</span></summary>
					<ul class="menu dropdown-content z-40 mb-2 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
						{#each more as l (l.href)}<li><a href={l.href} aria-current={current(l.href) ? 'page' : undefined} class:menu-active={current(l.href)}><Icon name={l.icon} size={16} />{l.label}</a></li>{/each}
					</ul>
				</details>
			{/if}
		</nav>
	{/if}
{/if}

<main id="main" tabindex="-1" class:with-tabbar={!bare && links.length > 0}>
	{#if bare || isDocs}
		{@render children()}
	{:else}
		{#key page.url.pathname}
			<div in:flyIn={{ y: 6, duration: 200 }}>{@render children()}</div>
		{/key}
	{/if}
</main>

{#if !bare && !isDocs}
	<footer class="page-container small muted pb-8 pt-8">
		<a href="/docs/overview">Developer docs</a> · <a href="/api/docs" target="_blank" rel="noopener">API reference</a> · <a href="/docs/ux-research">Design rules</a>
	</footer>
{/if}

<Toaster />
<DialogHost />

<style>
	.skip-link { position: absolute; left: 0.5rem; top: -4rem; z-index: 60; }
	.skip-link:focus { top: 0.5rem; }
	main:focus { outline: none; }
	.brand { color: var(--color-base-content); }
	@media (max-width: 359px) { .brand-name { display: none; } }
	/* The bar: frosted, with a soft hairline. */
	.topbar { background: color-mix(in oklab, var(--color-base-100) 82%, transparent); backdrop-filter: saturate(1.4) blur(12px); border-bottom: 1px solid color-mix(in oklab, var(--color-base-300) 70%, transparent); }
	/* The pill: one rounded track, the current page lifted out of it. */
	.pill { position: relative; align-items: center; gap: 0.125rem; padding: 0.25rem; border-radius: 9999px; background: var(--color-base-200); border: 1px solid color-mix(in oklab, var(--color-base-300) 60%, transparent); }
	.pill-mark { position: absolute; top: 0.25rem; bottom: 0.25rem; left: 0; border-radius: 9999px; background: var(--color-base-100);
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.08), 0 0 0 1px color-mix(in oklab, var(--color-primary) 22%, transparent); }
	.pill-mark.glide { transition: transform 280ms cubic-bezier(0.22, 1, 0.36, 1), width 280ms cubic-bezier(0.22, 1, 0.36, 1); }
	.pill-link { position: relative; z-index: 1; display: inline-flex; align-items: center; gap: 0.4rem; padding: 0.4rem 0.8rem; border-radius: 9999px;
		font-size: 0.875rem; font-weight: 600; text-decoration: none; color: color-mix(in oklab, var(--color-base-content) 75%, transparent); white-space: nowrap; }
	.pill-link:hover { color: var(--color-base-content); }
	.pill-link.on { color: var(--color-primary); }
	@media (max-width: 1100px) { .pill-link :global(svg) { display: none; } }
	@media (max-width: 860px) { .pill-link { padding: 0.4rem 0.6rem; } }
	/* Phones: a tab bar along the bottom, within the thumb's reach. */
	.tabbar { position: fixed; z-index: 30; left: 0.5rem; right: 0.5rem; bottom: calc(0.5rem + env(safe-area-inset-bottom)); display: flex; justify-content: space-around; padding: 0.3rem;
		border-radius: 1.25rem; background: color-mix(in oklab, var(--color-base-100) 90%, transparent); backdrop-filter: saturate(1.4) blur(12px);
		border: 1px solid color-mix(in oklab, var(--color-base-300) 70%, transparent); box-shadow: 0 8px 24px rgb(0 0 0 / 0.12); }
	.tab-link { flex: 1; display: grid; justify-items: center; gap: 0.1rem; padding: 0.25rem 0.25rem 0.2rem; border-radius: 0.9rem; text-decoration: none; cursor: pointer;
		color: color-mix(in oklab, var(--color-base-content) 72%, transparent); min-width: 0; }
	.tab-more { flex: 1; display: grid; }
	.tab-icon { display: grid; place-items: center; width: 3rem; height: 1.75rem; border-radius: 9999px; }
	.tab-link.on { color: var(--color-primary); }
	.tab-link.on .tab-icon { background: color-mix(in oklab, var(--color-primary) 14%, transparent); }
	.tab-label { font-size: 0.7rem; font-weight: 600; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	@media (min-width: 640px) { .tabbar { display: none; } }
	@media (max-width: 639px) { main.with-tabbar { padding-bottom: 5.5rem; } }
	details > summary { list-style: none; }
	details > summary::-webkit-details-marker { display: none; }
</style>
