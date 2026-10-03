<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import type { Effective, Overrides } from '$lib/types';

	type U = { id: string; email: string; name: string; role: string; status: string; email_verified: boolean };
	type Ev = { id: number; actor_name: string; action: string; target_type: string; target_id: string; details: unknown; created_at: string };
	const ready = requireRole('admin');
	let tab = $state<'users' | 'usage' | 'settings' | 'audit'>('users');
	let users = $state<U[]>([]);
	let q = $state('');
	let role = $state('');
	let status = $state('');
	let usage = $state<any>(null);
	let policy = $state({ require_teacher_approval: false });
	let platform = $state<{ overrides: Overrides; effective: Effective }>({ overrides: {}, effective: {} });
	let events = $state<Ev[]>([]);
	let notice = $state('');
	let errors = $state<Record<string, string>>({});

	async function loadUsers() {
		const p = new URLSearchParams({ q, role, status });
		users = (await api.get<{ users: U[] }>('/api/admin/users?' + p)).users ?? [];
	}
	$effect(() => {
		void role;
		void status;
		if (ready()) loadUsers();
	});
	async function open(t: typeof tab) {
		tab = t;
		if (t === 'usage') usage = await api.get('/api/admin/usage');
		if (t === 'settings') {
			policy = await api.get('/api/admin/auth-policy');
			platform = await api.get('/api/admin/settings');
		}
		if (t === 'audit') events = (await api.get<{ events: Ev[] }>('/api/admin/audit?limit=100')).events ?? [];
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
		if (!confirm(`Delete ${u.name}? Their personal data is removed; results stay anonymised.`)) return;
		await api.del('/api/admin/users/' + u.id);
		loadUsers();
	}
	async function savePolicy() {
		await api.put('/api/admin/auth-policy', policy);
		notice = 'Saved';
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

<div class="container stack">
	<h1>Admin</h1>
	{#if notice}<p class="alert small">{notice}</p>{/if}
	<div class="tabs">
		<button class:active={tab === 'users'} onclick={() => open('users')}>Users</button>
		<button class:active={tab === 'usage'} onclick={() => open('usage')}>Usage</button>
		<button class:active={tab === 'settings'} onclick={() => open('settings')}>Platform settings</button>
		<button class:active={tab === 'audit'} onclick={() => open('audit')}>Audit log</button>
	</div>
	{#if tab === 'users'}
		<form class="row" onsubmit={(e) => { e.preventDefault(); loadUsers(); }}>
			<input style="flex:1;min-width:12rem" placeholder="Search name or email" bind:value={q} aria-label="Search" />
			<select style="width:9rem" bind:value={role} aria-label="Role"><option value="">All roles</option><option>student</option><option>teacher</option><option>admin</option></select>
			<select style="width:11rem" bind:value={status} aria-label="Status"><option value="">All statuses</option><option value="active">active</option><option value="pending_approval">pending approval</option><option value="suspended">suspended</option></select>
			<button>Search</button>
		</form>
		<div class="card table-wrap">
			<table><thead><tr><th>Name</th><th>Role</th><th>Status</th><th></th></tr></thead><tbody>
				{#each users as u (u.id)}
					<tr>
						<td>{u.name}<br /><span class="small muted">{u.email}{u.email_verified ? '' : ' · unverified'}</span></td>
						<td>{u.role}</td>
						<td><span class="badge {u.status === 'active' ? 'ok' : u.status === 'suspended' ? 'danger' : 'warn'}">{u.status.replace('_', ' ')}</span></td>
						<td class="row">
							{#if u.status === 'pending_approval'}<button class="small primary" onclick={() => setStatus(u, 'active')}>Approve</button>
							{:else if u.status === 'suspended'}<button class="small" onclick={() => setStatus(u, 'active')}>Activate</button>
							{:else}<button class="small" onclick={() => setStatus(u, 'suspended')}>Suspend</button>{/if}
							<button class="small danger" onclick={() => del(u)}>Delete</button>
						</td>
					</tr>
				{/each}
			</tbody></table>
		</div>
	{:else if tab === 'usage' && usage}
		<div class="grid">
			{#each [['Teachers', usage.users_by_role.teacher ?? 0], ['Students', usage.users_by_role.student ?? 0], ['Classrooms', usage.classrooms], ['Quizzes', usage.quizzes], ['Sessions (30 days)', usage.sessions_30d], ['Live now', usage.live_sessions], ['Attempts (30 days)', usage.attempts_30d], ['Violations (30 days)', usage.violations_30d], ['Teachers with LLM keys', usage.teachers_with_llm_keys]] as [l, v] (l)}
				<div class="card"><div class="small muted">{l}</div><div style="font-size:1.5rem;font-weight:700">{v}</div></div>
			{/each}
		</div>
		<div class="card small"><strong>Background jobs</strong><br />{Object.entries(usage.jobs_by_queue_state).map(([k, v]) => `${k}: ${v}`).join(' · ') || 'none'}</div>
	{:else if tab === 'settings'}
		<div class="card stack">
			<label class="row" style="font-weight:400"><input type="checkbox" bind:checked={policy.require_teacher_approval} /> New teacher accounts need admin approval</label>
			<div><button onclick={savePolicy}>Save</button></div>
		</div>
		<h2>Platform defaults</h2>
		<div class="card"><SettingsEditor level="platform" value={platform.overrides} effective={platform.effective} onsave={savePlatform} {errors} /></div>
	{:else if tab === 'audit'}
		<div class="card table-wrap">
			<table><thead><tr><th>When</th><th>Who</th><th>Action</th><th>Target</th></tr></thead><tbody>
				{#each events as e (e.id)}
					<tr><td class="small">{new Date(e.created_at).toLocaleString()}</td><td>{e.actor_name || 'system'}</td><td>{e.action.replaceAll('_', ' ')}</td><td class="small">{e.target_type} {e.target_id}</td></tr>
				{/each}
			</tbody></table>
		</div>
	{/if}
</div>
