<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	import DirectoryPanel from '$lib/admin/DirectoryPanel.svelte';
	import SsoPanel from '$lib/admin/SsoPanel.svelte';
	import DrivePanel from '$lib/admin/DrivePanel.svelte';
	import MeetingLimits from '$lib/admin/MeetingLimits.svelte';
	import RowMenu from '$lib/ui/RowMenu.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { api, ApiError } from '$lib/api';
	import { requireStaff } from '$lib/guard.svelte';
	import { auth } from '$lib/session.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import type { Effective, Overrides } from '$lib/types';
	import { Paged } from '$lib/paged.svelte';
	import { urlState } from '$lib/urlstate';
	import ListSearch from '$lib/ui/ListSearch.svelte';
	import Pager from '$lib/ui/Pager.svelte';
	import SortHeader from '$lib/ui/SortHeader.svelte';
	import { can, canActOn, consoleTabs, isAdmin, roleLabel, type ConsoleTab } from '$lib/staff';
	import ManagerList from '$lib/admin/ManagerList.svelte';
	import RecordingsPanel from '$lib/admin/RecordingsPanel.svelte';
	import BudgetPanel from '$lib/admin/BudgetPanel.svelte';
	import MonitorPanel from '$lib/admin/MonitorPanel.svelte';
	import StoragePanel from '$lib/admin/StoragePanel.svelte';
	import BackupsPanel from '$lib/admin/BackupsPanel.svelte';
	import LivePanel from '$lib/admin/LivePanel.svelte';
	import ClassesPanel from '$lib/admin/ClassesPanel.svelte';
	import EmailPanel from '$lib/admin/EmailPanel.svelte';
	import LocalAIPanel from '$lib/admin/LocalAIPanel.svelte';
	import CapacityPanel from '$lib/admin/CapacityPanel.svelte';
	import SignInLimits from '$lib/admin/SignInLimits.svelte';
	import Explore from '$lib/insight/Explore.svelte';
	import ManagerDialog, { type ManagerRow, type Teacher } from '$lib/admin/ManagerDialog.svelte';
	import { grouped, toolFrom, TOOLS } from '$lib/admin/tools';
	import Icon from '$lib/ui/Icon.svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { untrack } from 'svelte';

	// The admin console, for admins and managers (D-56). A manager sees only
	// the tools and actions for the features they were given; the server
	// checks each one again (PL-NFR-06). It opens on a page of tiles, one per
	// tool, grouped (D-118); a tool is ?tool=… in the address, so Back returns
	// to the tiles and a tool can be bookmarked.
	type U = { id: string; email: string; name: string; role: string; status: string; email_verified: boolean; created_at?: string; manager?: { features: string[] }; directory?: boolean };
	type Ev = { id: number; actor_name: string; actor_role?: string; action: string; target_type: string; target_id: string; details: unknown; created_at: string };
	const ready = requireStaff();
	const tabs = $derived(consoleTabs(auth.user));
	let role = $state(Paged.fromUrl(urlState, 'role'));
	let status = $state(Paged.fromUrl(urlState, 'status'));
	let actor = $state(Paged.fromUrl(urlState, 'actor', 'log'));
	let actorRole = $state(Paged.fromUrl(urlState, 'actor_role', 'log'));
	let actorName = $state('');
	// Users and the audit log, one page at a time (PL-FR-01).
	const users = new Paged<U>(() => '/api/admin/users', 'users', { url: urlState, sort: 'created', desc: true, filters: () => ({ role, status }) });
	const events = new Paged<Ev>(() => '/api/admin/audit', 'events', { url: urlState, prefix: 'log', sort: 'time', desc: true, filters: () => ({ actor, actor_role: actorRole }) });
	let usage = $state<any>(null);
	let policy = $state({ require_teacher_approval: false });
	let platform = $state<{ overrides: Overrides; effective: Effective }>({ overrides: {}, effective: {} });
	let notice = $state('');
	let errors = $state<Record<string, string>>({});
	let makeOpen = $state(false);
	let makeTeacher = $state<Teacher | null>(null);
	// The open tool: from the address; a shared audit link (log…=) opens the
	// audit log; someone with a single tool goes straight to it.
	const tab = $derived<ConsoleTab | null>(
		toolFrom(page.url.searchParams.get('tool'), tabs) ?? ((actor || actorRole) && tabs.includes('audit') ? 'audit' : tabs.length === 1 ? tabs[0] : null)
	);
	const go = (t: ConsoleTab | null) => goto(t ? '/admin?tool=' + t : '/admin');
	let find = $state('');
	const groups = $derived(grouped(tabs, find));
	// What needs a look, on the tiles: people waiting for approval, things live now.
	let counts = $state<{ pending?: number; live?: number }>({});
	async function loadCounts() {
		const c: typeof counts = {};
		if (tabs.includes('users')) c.pending = (await api.get<{ total: number }>('/api/admin/users?status=pending_approval&size=10').catch(() => null))?.total;
		if (tabs.includes('usage')) c.live = (await api.get<{ live_sessions: number }>('/api/admin/usage').catch(() => null))?.live_sessions;
		counts = c;
	}
	const badge = (t: ConsoleTab) => (t === 'users' && counts.pending ? `${counts.pending} to approve` : t === 'live' && counts.live ? `${counts.live} live` : '');

	const loadUsers = () => users.load();
	$effect(() => {
		if (!ready()) return;
		const t = tab;
		untrack(() => (t ? open(t) : loadCounts()));
	});
	async function open(t: ConsoleTab) {
		if (t === 'users') await users.load();
		if (t === 'usage') usage = await api.get('/api/admin/usage');
		if (t === 'settings') {
			if (can(auth.user, 'approval_policy')) policy = await api.get('/api/admin/auth-policy');
			if (can(auth.user, 'settings')) platform = await api.get('/api/admin/settings');
		}
		if (t === 'audit') await events.load();
	}
	async function setStatus(u: U, s: string) {
		try {
			await api.post('/api/admin/users/' + u.id + '/status', { status: s });
			loadUsers();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : '';
		}
	}
	async function del(u: U) {
		if (!(await confirmDialog({ title: tr("Delete {0}?", [u.name]), body: tr("Their personal data is removed; results stay anonymised."), confirm: tr("Delete user"), danger: true }))) return;
		try {
			await api.del('/api/admin/users/' + u.id);
			loadUsers();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : '';
		}
	}
	// Changing a role (D-101): admins any but their own; managers students and teachers only.
	const ROLE_WORDS: Record<string, string> = {
		student: 'They lose their teacher pages. A teacher who still owns classrooms stays a teacher until those move to another teacher (Classes).',
		teacher: 'They can create classrooms, quizzes, polls and meetings. Their own enrolments as a student stay.',
		admin: 'They get the whole admin console, every setting and every account. Give this only to someone you trust with all of it.'
	};
	async function changeRole(u: U, to: 'student' | 'teacher' | 'admin') {
		if (!(await confirmDialog({ title: tr("Make {0} {1} {2}?", [u.name, to === 'admin' ? 'an' : 'a', to]), body: ROLE_WORDS[to], confirm: tr("Make {0}", [to]), danger: to === 'admin' }))) return;
		try {
			await api.post('/api/admin/users/' + u.id + '/role', { role: to });
			notice = '';
			loadUsers();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : '';
		}
	}
	function roleActions(u: U) {
		if (!canActOn(auth.user, u)) return [];
		const out: { label: string; icon: 'user' | 'book' | 'flag'; onclick: () => void; danger?: boolean }[] = [];
		if (u.role !== 'student' && u.role !== 'admin' && !u.manager) out.push({ label: 'Make student', icon: 'user', onclick: () => changeRole(u, 'student') });
		if (u.role !== 'teacher') out.push({ label: 'Make teacher', icon: 'book', onclick: () => changeRole(u, 'teacher') });
		if (isAdmin(auth.user) && u.role !== 'admin') out.push({ label: 'Make admin', icon: 'flag', danger: true, onclick: () => changeRole(u, 'admin') });
		if (isAdmin(auth.user) && u.role === 'admin') out.push({ label: 'Make student', icon: 'user', onclick: () => changeRole(u, 'student') });
		return out;
	}
	function makeManager(u: U) {
		makeTeacher = u;
		makeOpen = true;
	}
	function activity(m: ManagerRow) {
		actor = m.id;
		actorName = m.name;
		actorRole = '';
		events.refilter();
		go('audit');
	}
	function clearActor() {
		actor = '';
		actorName = '';
		events.refilter();
	}
	async function savePolicy() {
		await api.put('/api/admin/auth-policy', policy);
		toast(tr("Settings saved"));
	}
	async function savePlatform(o: Overrides) {
		errors = {};
		try {
			platform = await api.put('/api/admin/settings', o);
		} catch (e) {
			if (e instanceof ApiError) errors = e.fields;
			throw e;
		}
	}
</script>

<div class="page-container vstack">
	{#if notice}<p class="alert alert-soft alert-warning small m-0">{notice}</p>{/if}
	{#if ready() && !tabs.length}
		<h1>{isAdmin(auth.user) ? tr("Admin") : tr("Manage")}</h1>
		<p class="alert alert-soft alert-info">{tr("You're a manager, but an admin hasn't given you any features yet.")}</p>
	{:else if !tab}
		<!-- The tiles (D-118). -->
		<div class="flex flex-wrap items-end gap-3">
			<div class="grow">
				<h1 class="m-0">{isAdmin(auth.user) ? tr("Admin") : tr("Manage")}</h1>
				<p class="m-0 muted">{tr("Choose what to work on.")}</p>
			</div>
			<label class="input input-sm w-full sm:w-72">
				<Icon name="search" size={15} />
				<span class="sr-only">{tr("Find a tool")}</span>
				<input type="search" placeholder={tr("Find a tool")} bind:value={find} />
			</label>
		</div>
		{#each groups as g (g.key)}
			<section class="vstack gap-2" aria-labelledby="g-{g.key}">
				<h2 id="g-{g.key}" class="group-h">{tr(g.label)}</h2>
				<div class="tiles">
					{#each g.tools as t (t.key)}
						{@const b = badge(t.key)}
						<a class="tile g-{g.key}" href="/admin?tool={t.key}">
							<span class="tile-ico" aria-hidden="true"><Icon name={t.icon} size={26} /></span>
							<span class="tile-text"><span class="tile-name">{tr(t.label)}</span><span class="small muted">{t.blurb}</span></span>
							{#if b}<span class="badge badge-warning badge-sm tile-badge">{b}</span>{/if}
						</a>
					{/each}
				</div>
			</section>
		{:else}
			<p class="muted">{tr("No tool matches “{0}”.", [find])}</p>
		{/each}
	{:else}
		{@const cur = TOOLS[tab]}
		<div class="console" class:with-rail={tabs.length > 1}>
			{#if tabs.length > 1}
				<!-- Every tool one click away, as icons (D-118). -->
				<nav class="rail" aria-label={tr("Admin tools")}>
					<a class="rail-btn" href="/admin" title={tr("All tools")} aria-label={tr("All tools")}><Icon name="grid" size={20} /></a>
					{#each grouped(tabs) as g (g.key)}
						<span class="rail-sep"></span>
						{#each g.tools as t (t.key)}
							<a class="rail-btn g-{g.key}" class:on={t.key === tab} href="/admin?tool={t.key}" title={tr(t.label)} aria-label={tr(t.label)} aria-current={t.key === tab ? 'page' : undefined}><Icon name={t.icon} size={20} /></a>
						{/each}
					{/each}
				</nav>
			{/if}
			<div class="vstack min-w-0">
				<header class="flex flex-wrap items-center gap-3">
					{#if tabs.length > 1}<a class="btn btn-ghost btn-sm" href="/admin"><Icon name="arrow-left" size={16} />{tr("All tools")}</a>{/if}
					<span class="tile-ico g-{cur.group} small-ico" aria-hidden="true"><Icon name={cur.icon} size={20} /></span>
					<div class="min-w-0">
						<h1 class="m-0 text-2xl">{tr(cur.label)}</h1>
						<p class="m-0 small muted">{cur.blurb}</p>
					</div>
				</header>
			{#if tab === 'users'}
				<div class="row">
					<ListSearch list={users} placeholder={tr("Search name or email")} label={tr("Search users")} />
					<select class="select select-sm w-full" style="width:9rem" bind:value={role} onchange={() => users.refilter()} aria-label={tr("Role")}><option value="">{tr("All roles")}</option><option>{tr("student")}</option><option>{tr("teacher")}</option><option>{tr("manager")}</option><option>{tr("admin")}</option></select>
					<select class="select select-sm w-full" style="width:11rem" bind:value={status} onchange={() => users.refilter()} aria-label={tr("Status")}><option value="">{tr("All statuses")}</option><option value="active">{tr("active")}</option><option value="pending_approval">{tr("pending approval")}</option><option value="suspended">{tr("suspended")}</option></select>
				</div>
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table"><thead><tr><th><SortHeader list={users} key="name" label={tr("Name")} /></th><th><SortHeader list={users} key="role" label={tr("Role")} /></th><th><SortHeader list={users} key="status" label={tr("Status")} /></th><th><SortHeader list={users} key="created" label={tr("Joined")} desc /></th><th><span class="sr-only">{tr("Actions")}</span></th></tr></thead><tbody>
						{#each users.rows as u (u.id)}
							<tr>
								<td>{u.name}{#if u.directory}{' '}<span class="badge badge-soft badge-sm">{tr("School account")}</span>{/if}<br /><span class="small muted">{u.email}{u.email_verified ? '' : tr(" · unverified")}</span></td>
								<td>{roleLabel(u)}</td>
								<td><span class="badge badge-soft {u.status === 'active' ? 'ok' : u.status === 'suspended' ? 'danger' : 'warn'}">{u.status.replace('_', ' ')}</span></td>
								<td class="small tabular">{u.created_at ? new Date(u.created_at).toLocaleDateString(localeTag()) : ''}</td>
								<td>
									<div class="flex items-center justify-end gap-1">
										{#if canActOn(auth.user, u) && u.status === 'pending_approval'}<button class="btn btn-sm btn-primary" onclick={() => setStatus(u, 'active')}>{tr("Approve")}</button>{/if}
										{#if canActOn(auth.user, u) || (isAdmin(auth.user) && u.role === 'teacher' && u.status === 'active' && !u.manager)}
											<RowMenu hint={tr("More actions for {0}", [u.name])} actions={[
												...(isAdmin(auth.user) && u.role === 'teacher' && u.status === 'active' && !u.manager ? [{ label: 'Make manager', icon: 'users' as const, onclick: () => makeManager(u) }] : []),
												...roleActions(u),
												...(canActOn(auth.user, u)
													? [
															u.status === 'suspended'
																? { label: 'Activate', icon: 'check' as const, onclick: () => setStatus(u, 'active') }
																: { label: 'Suspend', icon: 'pause' as const, onclick: () => setStatus(u, 'suspended') },
															{ label: 'Delete account', icon: 'trash' as const, danger: true, onclick: () => del(u) }
														]
													: [])
											]} />
										{/if}
									</div>
								</td>
							</tr>
						{:else}<tr><td colspan="5" class="muted">{users.loaded ? (users.q ? tr("No users match “{0}”.", [users.q]) : tr("No users.")) : tr("Loading…")}</td></tr>
						{/each}
					</tbody></table>
				</div>
				<Pager list={users} label={tr("Users")} />
			{:else if tab === 'usage' && usage}
				<div class="auto-grid">
					{#each [['Teachers', usage.users_by_role.teacher ?? 0], ['Students', usage.users_by_role.student ?? 0], ['Classrooms', usage.classrooms], ['Quizzes', usage.quizzes], ['Sessions (30 days)', usage.sessions_30d], ['Live now', usage.live_sessions], ['Attempts (30 days)', usage.attempts_30d], ['Violations (30 days)', usage.violations_30d], ['Teachers with LLM keys', usage.teachers_with_llm_keys]] as [l, v] (l)}
						<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><div class="small muted">{l}</div><div style="font-size:1.5rem;font-weight:700">{v}</div></div>
					{/each}
				</div>
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 small"><strong>{tr("Background jobs")}</strong><br />{Object.entries(usage.jobs_by_queue_state).map(([k, v]) => `${k}: ${v}`).join(' · ') || 'none'}</div>
			{:else if tab === 'settings'}
				{#if can(auth.user, 'approval_policy')}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<label class="row" style="font-weight:400"><input class="checkbox" type="checkbox" bind:checked={policy.require_teacher_approval} /> {tr("New teacher accounts need approval")}</label>
						<div><button class="btn" onclick={savePolicy}>{tr("Save")}</button></div>
					</div>
				{/if}
				{#if can(auth.user, 'settings')}
					<h2>{tr("Platform defaults")}</h2>
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><SettingsEditor level="platform" value={platform.overrides} effective={platform.effective} onsave={savePlatform} {errors} /></div>
					<MeetingLimits />
				{/if}
				{#if isAdmin(auth.user)}<SignInLimits />{/if}
			{:else if tab === 'audit'}
				<div class="row">
					<ListSearch list={events} placeholder={tr("Search action, target or person")} label={tr("Search the audit log")} />
					<select class="select select-sm w-full" style="width:10rem" bind:value={actorRole} onchange={() => events.refilter()} aria-label={tr("Acting as")}><option value="">{tr("Anyone")}</option><option value="admin">{tr("Admins")}</option><option value="manager">{tr("Managers")}</option></select>
				</div>
				{#if actor}
					<p class="m-0 small">{tr("Showing what")} <strong>{actorName || tr("one person")}</strong> {tr("did.")} <button class="btn btn-xs btn-ghost" onclick={clearActor}>{tr("Show everyone")}</button></p>
				{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table"><thead><tr><th><SortHeader list={events} key="time" label={tr("When")} desc /></th><th>{tr("Who")}</th><th>{tr("Action")}</th><th>{tr("Target")}</th></tr></thead><tbody>
						{#each events.rows as e (e.id)}
							<tr><td class="small">{new Date(e.created_at).toLocaleString(localeTag())}</td><td>{e.actor_name || 'system'}{#if e.actor_role === 'manager'}{' '}<span class="badge badge-soft badge-sm">{tr("manager")}</span>{/if}</td><td>{e.action.replaceAll('_', ' ')}</td><td class="small">{e.target_type} {e.target_id}</td></tr>
						{:else}<tr><td colspan="4" class="muted">{events.loaded ? tr("No events.") : tr("Loading…")}</td></tr>
						{/each}
					</tbody></table>
				</div>
				<Pager list={events} label={tr("Audit log")} />
			{:else if tab === 'storage'}
				<StoragePanel canStorage={can(auth.user, 'storage')} canCleanup={can(auth.user, 'cleanup')} />
			{:else if tab === 'backups'}
				<BackupsPanel />
			{:else if tab === 'live'}
				<LivePanel />
			{:else if tab === 'classes'}
				<ClassesPanel />
			{:else if tab === 'email'}
				<EmailPanel />
			{:else if tab === 'explore'}
				<Explore />
			{:else if tab === 'ai'}
				<LocalAIPanel />
			{:else if tab === 'capacity'}
				<CapacityPanel />
			{:else if tab === 'signin'}
				<div class="vstack">
					<section class="vstack" aria-labelledby="sso-h"><h2 id="sso-h" class="m-0 text-lg">{tr("Other accounts (OpenID Connect)")}</h2><SsoPanel /></section>
					<section class="vstack" aria-labelledby="dir-h"><h2 id="dir-h" class="m-0 mt-4 text-lg">{tr("School directory (Active Directory, LDAP)")}</h2><DirectoryPanel /></section>
				</div>
			{:else if tab === 'drive'}
				<DrivePanel />
			{:else if tab === 'recordings'}
				<div class="vstack">
					<BudgetPanel />
					<MonitorPanel />
					<RecordingsPanel meId={auth.user?.id ?? ''} />
				</div>
			{:else if tab === 'managers'}
				<ManagerList onactivity={activity} />
			{/if}
			</div>
		</div>
	{/if}
</div>
<ManagerDialog bind:open={makeOpen} teacher={makeTeacher} onsaved={loadUsers} />

<style>
	.group-h { margin: 0.5rem 0 0; font-size: 0.8rem; font-weight: 600; letter-spacing: 0.06em; text-transform: uppercase; color: var(--color-muted); }
	.tiles { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr)); }
	.tile { position: relative; display: flex; align-items: flex-start; gap: 0.85rem; padding: 1rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); color: inherit; text-decoration: none; transition: border-color 0.15s, box-shadow 0.15s, translate 0.15s; }
	.tile:hover { border-color: var(--g); box-shadow: 0 6px 18px rgb(0 0 0 / 0.08); translate: 0 -1px; }
	.tile:focus-visible { outline: 2px solid var(--g); outline-offset: 2px; }
	.tile-text { display: flex; flex-direction: column; gap: 0.15rem; min-width: 0; }
	.tile-name { font-weight: 600; }
	.tile-badge { position: absolute; top: 0.5rem; right: 0.5rem; }
	.tile-ico { display: grid; place-items: center; flex: none; width: 3rem; height: 3rem; color: var(--g); background: color-mix(in oklab, var(--g) 14%, transparent); }
	.small-ico { width: 2.5rem; height: 2.5rem; }
	/* One colour per group, so the eye finds a group again. */
	.g-people { --g: var(--color-primary); }
	.g-activity { --g: var(--color-info); }
	.g-messages { --g: var(--color-secondary); }
	.g-settings { --g: var(--color-accent); }
	.g-system { --g: var(--color-warning); }

	.console { display: grid; gap: 1.25rem; }
	.console.with-rail { grid-template-columns: auto minmax(0, 1fr); align-items: start; }
	.rail { position: sticky; top: 4.5rem; display: flex; flex-direction: column; gap: 0.25rem; padding: 0.35rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); }
	.rail-btn { display: grid; place-items: center; width: 2.5rem; height: 2.5rem; color: var(--color-muted); }
	.rail-btn:hover { color: var(--g, var(--color-base-content)); background: var(--color-base-200); }
	.rail-btn.on { color: var(--g); background: color-mix(in oklab, var(--g) 14%, transparent); box-shadow: inset 3px 0 0 var(--g); }
	.rail-sep { height: 1px; margin: 0.15rem 0.25rem; background: var(--color-base-300); }
	/* Phones: an app-style grid, icon over name, two or three across. */
	@media (max-width: 640px) {
		.tiles { grid-template-columns: repeat(auto-fill, minmax(6.5rem, 1fr)); gap: 0.5rem; }
		.tile { flex-direction: column; align-items: center; text-align: center; gap: 0.4rem; padding: 0.75rem 0.4rem; }
		.tile-text .muted { display: none; }
		.tile-name { font-size: 0.85rem; line-height: 1.2; }
		.tile-badge { top: 0.25rem; right: 0.25rem; }
	}
	/* Phones and small windows: the "All tools" button is the way back. */
	@media (max-width: 1023px) {
		.console.with-rail { grid-template-columns: minmax(0, 1fr); }
		.rail { display: none; }
	}
</style>
