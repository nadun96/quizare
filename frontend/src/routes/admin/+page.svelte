<script lang="ts">
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
	import StoragePanel from '$lib/admin/StoragePanel.svelte';
	import BackupsPanel from '$lib/admin/BackupsPanel.svelte';
	import ManagerDialog, { type ManagerRow, type Teacher } from '$lib/admin/ManagerDialog.svelte';

	// The admin console, for admins and managers (D-56). A manager sees only
	// the tabs and actions for the features they were given; the server
	// checks each one again (PL-NFR-06).
	type U = { id: string; email: string; name: string; role: string; status: string; email_verified: boolean; created_at?: string; manager?: { features: string[] } };
	type Ev = { id: number; actor_name: string; actor_role?: string; action: string; target_type: string; target_id: string; details: unknown; created_at: string };
	const ready = requireStaff();
	const tabs = $derived(consoleTabs(auth.user));
	const labels: Record<ConsoleTab, string> = { users: 'Users', usage: 'Usage', settings: 'Platform settings', audit: 'Audit log', storage: 'Storage', backups: 'Backups', managers: 'Managers' };
	let tab = $state<ConsoleTab | null>(null);
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

	const loadUsers = () => users.load();
	$effect(() => {
		// A shared audit link (log…=) opens the audit log; otherwise the first tab.
		if (ready() && !tab && tabs.length) open(actor || actorRole ? (tabs.includes('audit') ? 'audit' : tabs[0]) : tabs[0]);
	});
	async function open(t: ConsoleTab) {
		tab = t;
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
		if (!(await confirmDialog({ title: `Delete ${u.name}?`, body: 'Their personal data is removed; results stay anonymised.', confirm: 'Delete user', danger: true }))) return;
		try {
			await api.del('/api/admin/users/' + u.id);
			loadUsers();
		} catch (e) {
			notice = e instanceof ApiError ? e.message : '';
		}
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
		tab = 'audit';
	}
	function clearActor() {
		actor = '';
		actorName = '';
		events.refilter();
	}
	async function savePolicy() {
		await api.put('/api/admin/auth-policy', policy);
		toast('Settings saved');
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
	<h1>{isAdmin(auth.user) ? 'Admin' : 'Manage'}</h1>
	{#if notice}<p class="alert alert-soft alert-warning small">{notice}</p>{/if}
	{#if ready() && !tabs.length}
		<p class="alert alert-soft alert-info">You're a manager, but an admin hasn't given you any features yet.</p>
	{/if}
	{#if tabs.length > 1}
		<div class="tabs tabs-border tabs-scroll" role="tablist">
			{#each tabs as t (t)}
				<button class="tab" role="tab" aria-selected={tab === t} class:tab-active={tab === t} onclick={() => open(t)}>{labels[t]}</button>
			{/each}
		</div>
	{/if}
	{#if tab === 'users'}
		<div class="row">
			<ListSearch list={users} placeholder="Search name or email" label="Search users" />
			<select class="select select-sm w-full" style="width:9rem" bind:value={role} onchange={() => users.refilter()} aria-label="Role"><option value="">All roles</option><option>student</option><option>teacher</option><option>manager</option><option>admin</option></select>
			<select class="select select-sm w-full" style="width:11rem" bind:value={status} onchange={() => users.refilter()} aria-label="Status"><option value="">All statuses</option><option value="active">active</option><option value="pending_approval">pending approval</option><option value="suspended">suspended</option></select>
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table"><thead><tr><th><SortHeader list={users} key="name" label="Name" /></th><th><SortHeader list={users} key="role" label="Role" /></th><th><SortHeader list={users} key="status" label="Status" /></th><th><SortHeader list={users} key="created" label="Joined" desc /></th><th><span class="sr-only">Actions</span></th></tr></thead><tbody>
				{#each users.rows as u (u.id)}
					<tr>
						<td>{u.name}<br /><span class="small muted">{u.email}{u.email_verified ? '' : ' · unverified'}</span></td>
						<td>{roleLabel(u)}</td>
						<td><span class="badge badge-soft {u.status === 'active' ? 'ok' : u.status === 'suspended' ? 'danger' : 'warn'}">{u.status.replace('_', ' ')}</span></td>
						<td class="small tabular">{u.created_at ? new Date(u.created_at).toLocaleDateString() : ''}</td>
						<td class="row">
							{#if canActOn(auth.user, u)}
								{#if u.status === 'pending_approval'}<button class="btn btn-sm btn-primary" onclick={() => setStatus(u, 'active')}>Approve</button>
								{:else if u.status === 'suspended'}<button class="btn btn-sm" onclick={() => setStatus(u, 'active')}>Activate</button>
								{:else}<button class="btn btn-sm" onclick={() => setStatus(u, 'suspended')}>Suspend</button>{/if}
								<button class="btn btn-sm btn-error btn-outline" onclick={() => del(u)}>Delete</button>
							{/if}
							{#if isAdmin(auth.user) && u.role === 'teacher' && u.status === 'active' && !u.manager}
								<button class="btn btn-sm btn-ghost" onclick={() => makeManager(u)}>Make manager</button>
							{/if}
						</td>
					</tr>
				{:else}<tr><td colspan="5" class="muted">{users.loaded ? (users.q ? `No users match “${users.q}”.` : 'No users.') : 'Loading…'}</td></tr>
				{/each}
			</tbody></table>
		</div>
		<Pager list={users} label="Users" />
	{:else if tab === 'usage' && usage}
		<div class="auto-grid">
			{#each [['Teachers', usage.users_by_role.teacher ?? 0], ['Students', usage.users_by_role.student ?? 0], ['Classrooms', usage.classrooms], ['Quizzes', usage.quizzes], ['Sessions (30 days)', usage.sessions_30d], ['Live now', usage.live_sessions], ['Attempts (30 days)', usage.attempts_30d], ['Violations (30 days)', usage.violations_30d], ['Teachers with LLM keys', usage.teachers_with_llm_keys]] as [l, v] (l)}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><div class="small muted">{l}</div><div style="font-size:1.5rem;font-weight:700">{v}</div></div>
			{/each}
		</div>
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 small"><strong>Background jobs</strong><br />{Object.entries(usage.jobs_by_queue_state).map(([k, v]) => `${k}: ${v}`).join(' · ') || 'none'}</div>
	{:else if tab === 'settings'}
		{#if can(auth.user, 'approval_policy')}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
				<label class="row" style="font-weight:400"><input class="checkbox" type="checkbox" bind:checked={policy.require_teacher_approval} /> New teacher accounts need approval</label>
				<div><button class="btn" onclick={savePolicy}>Save</button></div>
			</div>
		{/if}
		{#if can(auth.user, 'settings')}
			<h2>Platform defaults</h2>
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><SettingsEditor level="platform" value={platform.overrides} effective={platform.effective} onsave={savePlatform} {errors} /></div>
		{/if}
	{:else if tab === 'audit'}
		<div class="row">
			<ListSearch list={events} placeholder="Search action, target or person" label="Search the audit log" />
			<select class="select select-sm w-full" style="width:10rem" bind:value={actorRole} onchange={() => events.refilter()} aria-label="Acting as"><option value="">Anyone</option><option value="admin">Admins</option><option value="manager">Managers</option></select>
		</div>
		{#if actor}
			<p class="m-0 small">Showing what <strong>{actorName || 'one person'}</strong> did. <button class="btn btn-xs btn-ghost" onclick={clearActor}>Show everyone</button></p>
		{/if}
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table"><thead><tr><th><SortHeader list={events} key="time" label="When" desc /></th><th>Who</th><th>Action</th><th>Target</th></tr></thead><tbody>
				{#each events.rows as e (e.id)}
					<tr><td class="small">{new Date(e.created_at).toLocaleString()}</td><td>{e.actor_name || 'system'}{#if e.actor_role === 'manager'} <span class="badge badge-soft badge-sm">manager</span>{/if}</td><td>{e.action.replaceAll('_', ' ')}</td><td class="small">{e.target_type} {e.target_id}</td></tr>
				{:else}<tr><td colspan="4" class="muted">{events.loaded ? 'No events.' : 'Loading…'}</td></tr>
				{/each}
			</tbody></table>
		</div>
		<Pager list={events} label="Audit log" />
	{:else if tab === 'storage'}
		<StoragePanel canStorage={can(auth.user, 'storage')} canCleanup={can(auth.user, 'cleanup')} />
	{:else if tab === 'backups'}
		<BackupsPanel />
	{:else if tab === 'managers'}
		<ManagerList onactivity={activity} />
	{/if}
</div>
<ManagerDialog bind:open={makeOpen} teacher={makeTeacher} onsaved={loadUsers} />
