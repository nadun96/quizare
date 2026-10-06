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
	import Toaster from '$lib/ui/Toaster.svelte';
	import DialogHost from '$lib/ui/DialogHost.svelte';

	let { children } = $props();

	prefs.load();
	onMount(() => {
		auth.load();
	});

	// The live quiz and the projector are full-screen experiences without the nav.
	const bare = $derived(page.url.pathname.startsWith('/attempt/') || page.url.pathname.endsWith('/projector') || page.url.pathname.endsWith('/present') || page.url.pathname.startsWith('/r/') || page.url.pathname.startsWith('/live/'));
	const isDocs = $derived(page.url.pathname.startsWith('/docs'));

	type Link = { href: string; label: string };
	const links = $derived<Link[]>(
		auth.user?.role === 'teacher'
			? [{ href: '/t', label: 'Classrooms' }, { href: '/t/polls', label: 'Polls' }, { href: '/t/settings', label: 'Settings' }]
			: auth.user?.role === 'student'
				? [{ href: '/my', label: 'My quizzes' }, { href: '/join', label: 'Join' }]
				: auth.user?.role === 'admin'
					? [{ href: '/admin', label: 'Admin' }]
					: []
	);
	const current = (href: string) => page.url.pathname === href || (href !== '/t' && page.url.pathname.startsWith(href + '/')) || (href === '/t' && page.url.pathname.startsWith('/t/') && !page.url.pathname.startsWith('/t/settings') && !page.url.pathname.startsWith('/t/polls'));

	// <details> menus are CSS-only; close them after a choice or navigation.
	let menus: HTMLDetailsElement[] = $state([]);
	afterNavigate(() => menus.forEach((m) => m && (m.open = false)));
	function closeOthers(e: Event) {
		const t = e.currentTarget as HTMLDetailsElement;
		if (t.open) menus.forEach((m) => m && m !== t && (m.open = false));
	}

	async function logout() {
		await auth.logout();
		goto('/login');
	}
</script>

<a class="skip-link btn btn-primary btn-sm" href="#main">Skip to content</a>

{#if !bare}
	<header class="navbar sticky top-0 z-30 border-b border-base-300 bg-base-100/90 backdrop-blur supports-[backdrop-filter]:bg-base-100/75">
		<div class="page-container flex items-center gap-2 py-0">
			{#if links.length}
				<details class="dropdown sm:hidden" bind:this={menus[0]} ontoggle={closeOthers}>
					<summary class="btn btn-ghost btn-square" aria-label="Menu"><Icon name="menu" size={22} /></summary>
					<ul class="menu dropdown-content z-40 mt-2 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
						{#each links as l (l.href)}<li><a href={l.href} aria-current={current(l.href) ? 'page' : undefined} class:menu-active={current(l.href)}>{l.label}</a></li>{/each}
					</ul>
				</details>
			{/if}
			<a class="brand flex min-w-0 items-center gap-2 whitespace-nowrap text-base font-bold no-underline sm:text-lg" href={auth.user ? auth.home() : '/'}>
				<span class="brand-mark grid size-8 place-items-center rounded-lg bg-primary text-primary-content" aria-hidden="true"><Icon name="qr" size={18} /></span>
				<span class="brand-name">Classroom Quiz</span>
			</a>
			<nav class="ml-4 hidden gap-1 sm:flex" aria-label="Main">
				{#each links as l (l.href)}
					<a class="btn btn-ghost btn-sm" class:nav-active={current(l.href)} aria-current={current(l.href) ? 'page' : undefined} href={l.href}>{l.label}</a>
				{/each}
			</nav>
			<span class="spacer"></span>
			<details class="dropdown dropdown-end" bind:this={menus[1]} ontoggle={closeOthers}>
				<summary class="btn btn-ghost btn-square" aria-label="Display settings" title="Display settings"><Icon name="sliders" size={20} /></summary>
				<div class="dropdown-content z-40 mt-2 w-72 rounded-box border border-base-300 bg-base-100 p-4 shadow-lg">
					<DisplayMenu idPrefix="nav" />
				</div>
			</details>
			{#if auth.user}
				<details class="dropdown dropdown-end" bind:this={menus[2]} ontoggle={closeOthers}>
					<summary class="btn btn-ghost gap-2 px-2" aria-label="Account menu">
						<span class="avatar avatar-placeholder"><span class="grid size-8 place-items-center rounded-full bg-neutral text-sm leading-none text-neutral-content">{auth.user.name.slice(0, 1).toUpperCase()}</span></span>
						<span class="hidden max-w-40 truncate md:inline">{auth.user.name}</span>
					</summary>
					<ul class="menu dropdown-content z-40 mt-2 w-56 rounded-box border border-base-300 bg-base-100 p-2 shadow-lg">
						<li class="menu-title truncate">{auth.user.email}</li>
						<li><a href="/account"><Icon name="user" size={16} />Account</a></li>
						<li><button onclick={logout}><Icon name="logout" size={16} />Log out</button></li>
					</ul>
				</details>
			{:else if auth.loaded}
				<a class="btn btn-ghost btn-sm" href="/login">Log in</a>
				<a class="btn btn-primary btn-sm" href="/register">Sign up</a>
			{/if}
		</div>
	</header>
{/if}

<main id="main" tabindex="-1">
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
	.nav-active { background: color-mix(in oklab, var(--color-primary) 12%, transparent); color: var(--color-primary); }
	details > summary { list-style: none; }
	details > summary::-webkit-details-marker { display: none; }
</style>
