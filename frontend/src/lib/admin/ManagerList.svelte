<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
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
			toast(status === 'suspended' ? tr("{0} is suspended and signed out", [m.name]) : tr("{0} is active again", [m.name]));
			list.load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work"), 'error');
		}
	}
	async function remove(m: ManagerRow) {
		const own = m.role === 'manager';
		const ok = await confirmDialog({
			title: tr("Remove {0} as a manager?", [m.name]),
			body: own ? tr("This manager-only account has nothing else, so it is suspended and signed out. You can delete it afterwards.") : tr("They stay a teacher and lose the “Manage” area at once."),
			confirm: tr("Remove manager"),
			danger: true
		});
		if (!ok) return;
		await api.del('/api/admin/managers/' + m.id);
		toast(tr("{0} is no longer a manager", [m.name]));
		list.load();
	}
	const when = (s?: string) => (s ? new Date(s).toLocaleString(localeTag()) : 'Never');
</script>

<div class="vstack">
	<div class="row">
		<ListSearch {list} placeholder={tr("Search name or email")} label={tr("Search managers")} />
		<span class="spacer"></span>
		<button class="btn btn-primary btn-sm" onclick={add}>{tr("Add a manager")}</button>
	</div>
	<p class="small muted m-0">{tr("Managers do the admin features you give them. They can never act on admins or other managers, nor make managers.")}</p>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
		<table class="table">
			<thead>
				<tr>
					<th><SortHeader {list} key="name" label={tr("Name")} /></th>
					<th>{tr("Account")}</th>
					<th>{tr("Features")}</th>
					<th><SortHeader {list} key="granted" label={tr("Made manager")} desc /></th>
					<th><SortHeader {list} key="active" label={tr("Last active")} /></th>
					<th><span class="sr-only">{tr("Actions")}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each list.rows as m (m.id)}
					<tr>
						<td>{m.name}<br /><span class="small muted">{m.email}</span>{#if m.status !== 'active'}{' '}<span class="badge badge-soft danger">{m.status.replace('_', ' ')}</span>{/if}</td>
						<td class="small">{m.role === 'manager' ? tr("Manager only") : tr("Teacher")}</td>
						<td class="small">{m.manager?.features.length ? m.manager.features.map(featureLabel).join(', ') : tr("None yet")}</td>
						<td class="small tabular">{when(m.granted_at)}{#if m.granted_by}<br /><span class="muted">{tr("by {0}", [m.granted_by])}</span>{/if}</td>
						<td class="small tabular">{when(m.last_active_at)}</td>
						<td>
							<div class="flex flex-wrap gap-1">
								<button class="btn btn-sm" onclick={() => edit(m)}>{tr("Features")}</button>
								<button class="btn btn-sm btn-ghost" onclick={() => onactivity(m)}>{tr("Activity")}</button>
								{#if m.status === 'active'}<button class="btn btn-sm btn-ghost" onclick={() => setStatus(m, 'suspended')}>{tr("Suspend")}</button>
								{:else}<button class="btn btn-sm btn-ghost" onclick={() => setStatus(m, 'active')}>{tr("Activate")}</button>{/if}
								<button class="btn btn-sm btn-error btn-outline" onclick={() => remove(m)}>{tr("Remove")}</button>
							</div>
						</td>
					</tr>
				{:else}
					<tr><td colspan="6" class="muted">{!list.loaded ? tr("Loading…") : list.q ? tr("No managers match.") : tr("No managers yet. Add one to share the admin work.")}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
	<Pager {list} label={tr("Managers")} />
</div>
<ManagerDialog bind:open={dialog} {editing} onsaved={() => list.load()} />
