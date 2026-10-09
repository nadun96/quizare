<script lang="ts">
	// The admin's managers page (PL-FR-16): every manager with their features,
	// when and by whom they were made managers, and their last activity, one
	// page at a time. The admin changes, suspends or removes any of them here.
	import { api, ApiError } from '../api';
	import { Paged } from '../paged.svelte';
	import { featureLabel } from '../staff';
	import { urlState } from '../urlstate';
	import { confirmDialog } from '../ui/dialog.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import SortHeader from '../ui/SortHeader.svelte';
	import { toast } from '../ui/toast.svelte';
	import ManagerDialog, { type ManagerRow } from './ManagerDialog.svelte';

	let { onactivity }: { onactivity: (m: ManagerRow) => void } = $props();
	const list = new Paged<ManagerRow>(() => '/api/admin/managers', 'managers', { url: urlState, prefix: 'mgr', sort: 'granted', desc: true });
	let dialog = $state(false);
	let editing = $state<ManagerRow | null>(null);
	list.load();

	function add() {
		editing = null;
		dialog = true;
	}
	function edit(m: ManagerRow) {
		editing = m;
		dialog = true;
	}
	async function setStatus(m: ManagerRow, status: 'active' | 'suspended') {
		try {
			await api.post('/api/admin/users/' + m.id + '/status', { status });
			toast(status === 'suspended' ? `${m.name} is suspended and signed out` : `${m.name} is active again`);
			list.load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'That didn’t work', 'error');
		}
	}
	async function remove(m: ManagerRow) {
		const own = m.role === 'manager';
		const ok = await confirmDialog({
			title: `Remove ${m.name} as a manager?`,
			body: own ? 'This manager-only account has nothing else, so it is suspended and signed out. You can delete it afterwards.' : 'They stay a teacher and lose the “Manage” area at once.',
			confirm: 'Remove manager',
			danger: true
		});
		if (!ok) return;
		await api.del('/api/admin/managers/' + m.id);
		toast(`${m.name} is no longer a manager`);
		list.load();
	}
	const when = (s?: string) => (s ? new Date(s).toLocaleString() : 'Never');
</script>

<div class="vstack">
	<div class="row">
		<ListSearch {list} placeholder="Search name or email" label="Search managers" />
		<span class="spacer"></span>
		<button class="btn btn-primary btn-sm" onclick={add}>Add a manager</button>
	</div>
	<p class="small muted m-0">Managers do the admin features you give them. They can never act on admins or other managers, nor make managers.</p>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
		<table class="table">
			<thead>
				<tr>
					<th><SortHeader {list} key="name" label="Name" /></th>
					<th>Account</th>
					<th>Features</th>
					<th><SortHeader {list} key="granted" label="Made manager" desc /></th>
					<th><SortHeader {list} key="active" label="Last active" /></th>
					<th><span class="sr-only">Actions</span></th>
				</tr>
			</thead>
			<tbody>
				{#each list.rows as m (m.id)}
					<tr>
						<td>{m.name}<br /><span class="small muted">{m.email}</span>{#if m.status !== 'active'} <span class="badge badge-soft danger">{m.status.replace('_', ' ')}</span>{/if}</td>
						<td class="small">{m.role === 'manager' ? 'Manager only' : 'Teacher'}</td>
						<td class="small">{m.manager?.features.length ? m.manager.features.map(featureLabel).join(', ') : 'None yet'}</td>
						<td class="small tabular">{when(m.granted_at)}{#if m.granted_by}<br /><span class="muted">by {m.granted_by}</span>{/if}</td>
						<td class="small tabular">{when(m.last_active_at)}</td>
						<td>
							<div class="flex flex-wrap gap-1">
								<button class="btn btn-sm" onclick={() => edit(m)}>Features</button>
								<button class="btn btn-sm btn-ghost" onclick={() => onactivity(m)}>Activity</button>
								{#if m.status === 'active'}<button class="btn btn-sm btn-ghost" onclick={() => setStatus(m, 'suspended')}>Suspend</button>
								{:else}<button class="btn btn-sm btn-ghost" onclick={() => setStatus(m, 'active')}>Activate</button>{/if}
								<button class="btn btn-sm btn-error btn-outline" onclick={() => remove(m)}>Remove</button>
							</div>
						</td>
					</tr>
				{:else}
					<tr><td colspan="6" class="muted">{!list.loaded ? 'Loading…' : list.q ? 'No managers match.' : 'No managers yet. Add one to share the admin work.'}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
	<Pager {list} label="Managers" />
</div>
<ManagerDialog bind:open={dialog} {editing} onsaved={() => list.load()} />
