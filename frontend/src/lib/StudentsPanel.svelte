<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A classroom's students with categories (V2-03, D-41): filter by category,
	// search, select several and add them to (or remove them from) a category.
	// Students come one page at a time from the server, which filters,
	// searches and sorts across all of them (PL-FR-01 to PL-FR-03).
	import { flip } from 'svelte/animate';
	import { api, ApiError } from './api';
	import Icon from './ui/Icon.svelte';
	import Avatar from './ui/Avatar.svelte';
	import IconBtn from './ui/IconBtn.svelte';
	import EmptyState from './ui/EmptyState.svelte';
	import { confirmDialog } from './ui/dialog.svelte';
	import { fadeIn, flipMs } from './ui/motion';
	import { toast } from './ui/toast.svelte';
	import { Paged } from './paged.svelte';
	import { urlState } from './urlstate';
	import ListSearch from './ui/ListSearch.svelte';
	import Pager from './ui/Pager.svelte';
	import SortHeader from './ui/SortHeader.svelte';

	export type Enrolment = { gender?: string; number_bad?: boolean; id: string; user_id?: string; student_name: string; student_email: string; student_avatar?: string; student_number: string | null; status: string; created_at: string; categories?: string[] };
	export type Category = { id: string; classroom_id: string; name: string; color: number; position: number; members: number };

	let {
		classroomId,
		count = $bindable(0),
		oneditnumber,
		onstatus,
		canRemove = true
	}: {
		classroomId: string;
		/** Students in the classroom, for the tab label. */
		count?: number;
		oneditnumber: (e: Enrolment) => Promise<void>;
		onstatus: (e: Enrolment, status: string) => Promise<void>;
		/** Only the classroom's owner removes students (D-121). */
		canRemove?: boolean;
	} = $props();

	let categories = $state<Category[]>([]);
	let filter = $state<string | null>(Paged.fromUrl(urlState, 'category', 'st') || null); // category id, or null for everyone
	let status = $state(Paged.fromUrl(urlState, 'status', 'st'));
	const list = new Paged<Enrolment>(() => '/api/teacher/classrooms/' + classroomId + '/enrolments', 'enrolments', {
		url: urlState, prefix: 'st', sort: 'joined', filters: () => ({ category: filter ?? undefined, status: status || undefined })
	});
	// Students waiting for approval (D-161).
	let waiting = $state(0);
	async function countWaiting() {
		waiting = (await api.get<{ total: number }>(`/api/teacher/classrooms/${classroomId}/enrolments?status=pending&size=1`)).total;
	}
	async function reload() {
		await list.load();
		if (!filter && !list.q && !status) count = list.total;
		await countWaiting();
	}
	async function setStatusFilter(v: string) {
		status = v;
		selected = new Set();
		await list.refilter();
	}
	async function decide(approve: boolean, ids: string[] = []) {
		const n = ids.length || waiting;
		const ok = await confirmDialog(approve
			? { title: tr("Approve {0}?", [n === 1 ? tr("1 student") : tr("{0} students", [n])]), body: tr("They join the classroom and see its work."), confirm: tr("Approve") }
			: { title: tr("Turn away {0}?", [n === 1 ? tr("1 student") : tr("{0} students", [n])]), body: tr("They can’t join this classroom with its code again."), confirm: tr("Turn away"), danger: true });
		if (!ok) return;
		try {
			const r = await api.post<{ changed: number }>(`/api/teacher/classrooms/${classroomId}/enrolments/pending`, { status: approve ? 'active' : 'rejected', enrolment_ids: ids });
			toast(approve ? tr("Approved: {0}", [r.changed]) : tr("Turned away: {0}", [r.changed]));
			selected = new Set();
			await reload();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work. Try again."), 'error');
		}
	}
	// The classroom's student ID format (D-162).
	let idRule = $state({ pattern: '', hint: '' });
	let idOpen = $state(false);
	let idBusy = $state(false);
	$effect(() => {
		if (classroomId) api.get<{ pattern: string; hint: string }>(`/api/teacher/classrooms/${classroomId}/student-ids`).then((r) => (idRule = r)).catch(() => {});
	});
	async function saveIdRule(e: SubmitEvent) {
		e.preventDefault();
		idBusy = true;
		try {
			const r = await api.put<{ pattern: string; hint: string; not_matching: number }>(`/api/teacher/classrooms/${classroomId}/student-ids`, idRule);
			idRule = { pattern: r.pattern, hint: r.hint };
			toast(r.not_matching ? tr("Saved. {0} current student IDs don’t match; they’re marked below.", [r.not_matching]) : tr("Saved"));
			idOpen = false;
			await reload();
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("That didn’t work. Try again."), 'error');
		} finally {
			idBusy = false;
		}
	}
	async function setGender(en: Enrolment, g: string) {
		try {
			await api.patch('/api/teacher/enrolments/' + en.id, { gender: g });
			en.gender = g;
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("That didn’t work. Try again."), 'error');
		}
	}
	const badIds = $derived(list.rows.filter((e) => e.number_bad).length);
	const selectedWaiting = $derived(list.rows.filter((e) => selected.has(e.id) && e.status === 'pending').map((e) => e.id));
	$effect(() => {
		if (classroomId) reload();
	});
	async function setFilter(id: string | null) {
		filter = id;
		selected = new Set();
		await list.refilter();
		if (!filter && !list.q && !status) count = list.total;
	}
	let selected = $state<Set<string>>(new Set());
	let newName = $state('');
	let editing = $state<Category | null>(null);
	let target = $state('');

	async function loadCategories() {
		categories = (await api.get<{ categories: Category[] }>('/api/teacher/classrooms/' + classroomId + '/categories')).categories;
	}
	$effect(() => {
		if (classroomId) loadCategories();
	});

	const byId = $derived(Object.fromEntries(categories.map((c) => [c.id, c])));
	const shown = $derived(list.rows);
	const allShownSelected = $derived(shown.length > 0 && shown.every((e) => selected.has(e.id)));

	function toggle(id: string) {
		const s = new Set(selected);
		if (s.has(id)) s.delete(id);
		else s.add(id);
		selected = s;
	}
	function toggleAll() {
		selected = allShownSelected ? new Set() : new Set(shown.map((e) => e.id));
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		try {
			const c = await api.post<Category>('/api/teacher/classrooms/' + classroomId + '/categories', { name: newName });
			newName = '';
			toast(tr("Group “{0}” made", [c.name]));
			await loadCategories();
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("Could not create"), 'error');
		}
	}
	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		if (!editing) return;
		try {
			await api.patch('/api/teacher/categories/' + editing.id, { name: editing.name, color: editing.color });
			editing = null;
			await loadCategories();
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("Could not save"), 'error');
		}
	}
	async function remove(c: Category) {
		if (!(await confirmDialog({ title: tr("Delete “{0}”?", [c.name]), body: tr("Its students stay in the classroom and in their other groups."), confirm: tr("Delete group"), danger: true }))) return;
		await api.del('/api/teacher/categories/' + c.id);
		editing = null;
		await loadCategories();
		if (filter === c.id) await setFilter(null);
		else await reload();
	}
	async function assign(assigned: boolean) {
		const c = byId[target];
		if (!c || !selected.size) return;
		const r = await api.post<{ affected: number }>('/api/teacher/categories/' + c.id + '/members', { enrolment_ids: [...selected], assigned });
		toast(assigned ? tr("Added {0} to “{1}”", [r.affected, c.name]) : tr("Removed {0} from “{1}”", [r.affected, c.name]), r.affected ? 'success' : 'info');
		await loadCategories();
		await reload();
	}
	async function untag(e: Enrolment, c: Category) {
		await api.post('/api/teacher/categories/' + c.id + '/members', { enrolment_ids: [e.id], assigned: false });
		await loadCategories();
		await reload();
	}
	async function setStatus(e: Enrolment, s: string) {
		await onstatus(e, s);
		await reload();
	}
	async function editNumber(e: Enrolment) {
		await oneditnumber(e);
		await reload();
	}
</script>

<div class="vstack">
	<!-- Groups (D-41, D-158): filter chips, create, edit -->
	<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-5">
		<div class="flex flex-wrap items-center gap-2">
			<span class="small font-semibold mr-1">{tr("Groups")}</span>
			<button class="chip btn btn-sm rounded-full" class:btn-primary={filter === null} class:btn-soft={filter === null} aria-pressed={filter === null} onclick={() => setFilter(null)}>{tr("All")} <span class="muted tabular">{count}</span></button>
			{#each categories as c (c.id)}
				<span class="chip-wrap" animate:flip={{ duration: flipMs() }}>
					<button class="chip btn btn-sm rounded-full" class:btn-primary={filter === c.id} class:btn-soft={filter === c.id} aria-pressed={filter === c.id} onclick={() => setFilter(filter === c.id ? null : c.id)}>
						<span class="dot" style:background="var(--cat-{c.color})" aria-hidden="true"></span>{c.name} <span class="muted tabular">{c.members}</span>
					</button>
					<IconBtn icon="pencil" label={tr("Edit")} hint={tr("Edit {0}", [c.name])} class="btn-ghost btn-xs" size={13} onclick={() => (editing = { ...c })} />
				</span>
			{/each}
			<form class="flex items-center gap-1" onsubmit={create}>
				<input class="input input-sm w-40" bind:value={newName} maxlength="60" placeholder={tr("New group")} aria-label={tr("New group name")} />
				<button class="btn btn-sm" disabled={!newName.trim()}><Icon name="plus" size={14} />{tr("Add")}</button>
			</form>
		</div>
		{#if editing}
			<form class="edit mt-3 flex flex-wrap items-center gap-2" onsubmit={saveEdit} in:fadeIn>
				<input class="input input-sm w-48" bind:value={editing.name} maxlength="60" aria-label={tr("Group name")} />
				<div class="flex gap-1" role="radiogroup" aria-label={tr("Colour")}>
					{#each [1, 2, 3, 4, 5, 6, 7, 8] as n (n)}
						<button type="button" role="radio" aria-checked={editing.color === n} aria-label={tr("Colour {0}", [n])} class="swatch" class:on={editing.color === n} style:background="var(--cat-{n})" onclick={() => (editing!.color = n)}></button>
					{/each}
				</div>
				<button class="btn btn-primary btn-sm">{tr("Save")}</button>
				<button type="button" class="btn btn-ghost btn-sm" onclick={() => (editing = null)}>{tr("Cancel")}</button>
				<span class="spacer"></span>
				<button type="button" class="btn btn-ghost btn-sm text-error" onclick={() => remove(editing!)}>{tr("Delete")}</button>
			</form>
		{/if}
	</div>

	<details class="card card-border bg-base-100 p-4" bind:open={idOpen}>
		<summary class="cursor-pointer small"><strong>{tr("Student ID format")}</strong> <span class="muted">{idRule.pattern ? tr("like {0}", [idRule.hint || idRule.pattern]) : tr("any")}</span></summary>
		<form class="vstack gap-2 mt-3" onsubmit={saveIdRule}>
			<p class="m-0 small muted">{tr("A pattern (regular expression) every student ID must match in full. Students can’t join or save an ID that doesn’t; IDs already given are marked. Leave it empty to allow any ID.")}</p>
			<div class="flex flex-wrap items-end gap-3">
				<label class="vstack gap-1 small">{tr("Pattern")}<input class="input input-sm w-72 font-mono" bind:value={idRule.pattern} maxlength="200" placeholder={tr("MGT/\\d{4}/\\d{3}")} /></label>
				<label class="vstack gap-1 small">{tr("Example shown to students")}<input class="input input-sm w-48" bind:value={idRule.hint} maxlength="100" placeholder={tr("MGT/2024/447")} /></label>
				<button class="btn btn-sm btn-primary" disabled={idBusy}>{tr("Save")}</button>
			</div>
			<p class="m-0 text-xs muted">{tr("For example:")} <code>{'MGT/\\d{4}/\\d{3}'}</code> {tr("is MGT/, 4 digits, /, 3 digits;")} <code>{'[A-Z]{3}\\d+'}</code> {tr("is 3 capital letters then digits.")}</p>
		</form>
	</details>

	{#if waiting}
		<div class="alert alert-soft alert-warning flex flex-wrap items-center gap-2" role="status">
			<Icon name="clock" size={18} />
			<span class="grow">{waiting === 1 ? tr("1 student is waiting for approval.") : tr("{0} students are waiting for approval.", [waiting])}</span>
			{#if status !== 'pending'}<button class="btn btn-sm" onclick={() => setStatusFilter('pending')}>{tr("Show them")}</button>{/if}
			<button class="btn btn-sm btn-primary" onclick={() => decide(true)}><Icon name="check" size={15} />{tr("Approve all")}</button>
			<button class="btn btn-sm btn-ghost" onclick={() => decide(false)}>{tr("Turn all away")}</button>
		</div>
	{/if}

	<!-- Search and bulk actions -->
	<div class="flex flex-wrap items-center gap-2">
		<ListSearch {list} placeholder={tr("Search name, email or student ID")} label={tr("Search students")} />
		<select class="select select-sm w-auto" value={status} onchange={(e) => setStatusFilter(e.currentTarget.value)} aria-label={tr("Status")}>
			<option value="">{tr("Everyone")}</option>
			<option value="pending">{tr("Waiting for approval")}</option>
			<option value="active">{tr("Active")}</option>
			<option value="rejected">{tr("Turned away")}</option>
			<option value="removed">{tr("Removed")}</option>
		</select>
		<span class="spacer"></span>
		{#if selected.size}
			<span class="small" in:fadeIn><strong>{selected.size}</strong> {tr("selected")}</span>
			{#if selectedWaiting.length}<button class="btn btn-sm btn-primary" onclick={() => decide(true, selectedWaiting)}>{tr("Approve {0}", [selectedWaiting.length])}</button>{/if}
			<select class="select select-sm w-auto" bind:value={target} aria-label={tr("Group")}>
				<option value="">{tr("Group…")}</option>
				{#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
			</select>
			<button class="btn btn-sm btn-primary" disabled={!target} onclick={() => assign(true)}>{tr("Add to")}</button>
			<button class="btn btn-sm" disabled={!target} onclick={() => assign(false)}>{tr("Remove from")}</button>
			<button class="btn btn-sm btn-ghost" onclick={() => (selected = new Set())}>{tr("Clear")}</button>
		{/if}
	</div>

	<div class="card card-border bg-base-100 p-0 shadow-sm table-wrap">
		{#if list.loaded && count === 0 && !filter && !list.q}
			<EmptyState icon="users" title={tr("No students yet")}>{tr("Share the join code from the Join code tab.")}</EmptyState>
		{:else}
			<table class="table">
				<thead>
					<tr>
						<th class="w-10"><input type="checkbox" class="checkbox checkbox-sm" checked={allShownSelected} onchange={toggleAll} aria-label={tr("Select all on this page")} /></th>
						<th><SortHeader {list} key="name" label={tr("Name")} /></th><th><SortHeader {list} key="number" label={tr("Student ID")} /></th><th>{tr("Gender")}</th><th>{tr("Groups")}</th><th><SortHeader {list} key="status" label={tr("Status")} /></th><th><span class="sr-only">{tr("Actions")}</span></th>
					</tr>
				</thead>
				<tbody>
					{#each shown as en (en.id)}
						<tr class:sel={selected.has(en.id)}>
							<td><input type="checkbox" class="checkbox checkbox-sm" checked={selected.has(en.id)} onchange={() => toggle(en.id)} aria-label={tr("Select {0}", [en.student_name])} /></td>
							<td><span class="flex items-center gap-2"><Avatar id={en.user_id ?? ''} name={en.student_name} avatar={en.student_avatar} size={30} /><span class="min-w-0">{en.student_name}<br /><span class="small muted">{en.student_email}</span></span></span></td>
							<td class="whitespace-nowrap">{en.student_number ?? '—'}{#if en.number_bad} <span class="badge badge-warning badge-soft badge-sm" title={tr("Doesn’t match the classroom’s student ID format")}>{tr("check")}</span>{/if} <button class="btn btn-ghost btn-xs" onclick={() => editNumber(en)}>{tr("Edit")}</button></td>
							<td>
								<select class="select select-xs w-auto" value={en.gender ?? ''} onchange={(e) => setGender(en, e.currentTarget.value)} aria-label={tr("Gender of {0}", [en.student_name])}>
									<option value="">{tr("Not given")}</option><option value="female">{tr("Female")}</option><option value="male">{tr("Male")}</option>
								</select>
							</td>
							<td>
								<div class="flex flex-wrap gap-1">
									{#each en.categories ?? [] as cid (cid)}
										{@const c = byId[cid]}
										{#if c}
											<span class="badge badge-soft gap-1 pr-0.5"><span class="dot" style:background="var(--cat-{c.color})" aria-hidden="true"></span>{c.name}<button class="btn btn-ghost btn-xs btn-circle" aria-label={tr("Remove {0} from {1}", [en.student_name, c.name])} onclick={() => untag(en, c)}><Icon name="x" size={11} /></button></span>
										{/if}
									{/each}
								</div>
							</td>
							<td><span class="badge badge-soft {en.status === 'active' ? 'ok' : en.status === 'pending' ? 'warn' : ''}">{en.status}</span></td>
							<td class="whitespace-nowrap">
								{#if en.status !== 'active'}<button class="btn btn-sm" onclick={() => setStatus(en, 'active')}>{tr("Approve")}</button>{/if}
								{#if en.status === 'pending'}<button class="btn btn-sm" onclick={() => setStatus(en, 'rejected')}>{tr("Reject")}</button>{/if}
								{#if en.status === 'active' && canRemove}<button class="btn btn-sm btn-error btn-outline" onclick={() => setStatus(en, 'removed')}>{tr("Remove")}</button>{/if}
							</td>
						</tr>
					{:else}
						<tr><td colspan="7" class="muted">{list.loaded ? tr("No students match.") : tr("Loading…")}</td></tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</div>
	<Pager {list} label={tr("Students")} />
</div>

<style>
	.chip-wrap { display: inline-flex; align-items: center; }
	.dot { width: 0.6rem; height: 0.6rem; border-radius: 999px; flex: none; }
	.swatch { width: 1.6rem; height: 1.6rem; border-radius: 999px; border: 2px solid var(--color-base-100); box-shadow: 0 0 0 1px var(--color-base-300); cursor: pointer; }
	.swatch.on { box-shadow: 0 0 0 2px var(--color-base-content); }
	tr.sel { background: color-mix(in oklab, var(--color-primary) 7%, transparent); }
</style>
