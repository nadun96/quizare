<script lang="ts">
	// A classroom's students with categories (V2-03, D-41): filter by category,
	// search, select several and add them to (or remove them from) a category.
	import { flip } from 'svelte/animate';
	import { api, ApiError } from './api';
	import Icon from './ui/Icon.svelte';
	import Avatar from './ui/Avatar.svelte';
	import IconBtn from './ui/IconBtn.svelte';
	import EmptyState from './ui/EmptyState.svelte';
	import { confirmDialog } from './ui/dialog.svelte';
	import { fadeIn, flipMs } from './ui/motion';
	import { toast } from './ui/toast.svelte';

	export type Enrolment = { id: string; user_id?: string; student_name: string; student_email: string; student_avatar?: string; student_number: string | null; status: string; created_at: string; categories?: string[] };
	export type Category = { id: string; classroom_id: string; name: string; color: number; position: number; members: number };

	let {
		classroomId,
		enrolments,
		onchange,
		oneditnumber,
		onstatus
	}: {
		classroomId: string;
		enrolments: Enrolment[];
		onchange: () => void | Promise<void>;
		oneditnumber: (e: Enrolment) => void;
		onstatus: (e: Enrolment, status: string) => void;
	} = $props();

	let categories = $state<Category[]>([]);
	let filter = $state<string | null>(null); // category id, or null for everyone
	let query = $state('');
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
	const shown = $derived(
		enrolments.filter((e) => {
			if (filter && !e.categories?.includes(filter)) return false;
			const q = query.trim().toLowerCase();
			return !q || e.student_name.toLowerCase().includes(q) || e.student_email.toLowerCase().includes(q) || (e.student_number ?? '').toLowerCase().includes(q);
		})
	);
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
			toast(`Category “${c.name}” created`);
			await loadCategories();
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not create', 'error');
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
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not save', 'error');
		}
	}
	async function remove(c: Category) {
		if (!(await confirmDialog({ title: `Delete “${c.name}”?`, body: 'Students stay in the classroom; only the label is removed.', confirm: 'Delete category', danger: true }))) return;
		await api.del('/api/teacher/categories/' + c.id);
		if (filter === c.id) filter = null;
		editing = null;
		await loadCategories();
		await onchange();
	}
	async function assign(assigned: boolean) {
		const c = byId[target];
		if (!c || !selected.size) return;
		const r = await api.post<{ affected: number }>('/api/teacher/categories/' + c.id + '/members', { enrolment_ids: [...selected], assigned });
		toast(assigned ? `Added ${r.affected} to “${c.name}”` : `Removed ${r.affected} from “${c.name}”`, r.affected ? 'success' : 'info');
		await loadCategories();
		await onchange();
	}
	async function untag(e: Enrolment, c: Category) {
		await api.post('/api/teacher/categories/' + c.id + '/members', { enrolment_ids: [e.id], assigned: false });
		await loadCategories();
		await onchange();
	}
</script>

<div class="vstack">
	<!-- Categories: filter chips, create, edit -->
	<div class="card card-border bg-base-100 p-4 shadow-sm sm:p-5">
		<div class="flex flex-wrap items-center gap-2">
			<span class="small font-semibold mr-1">Categories</span>
			<button class="chip btn btn-sm rounded-full" class:btn-primary={filter === null} class:btn-soft={filter === null} aria-pressed={filter === null} onclick={() => (filter = null)}>All <span class="muted tabular">{enrolments.length}</span></button>
			{#each categories as c (c.id)}
				<span class="chip-wrap" animate:flip={{ duration: flipMs() }}>
					<button class="chip btn btn-sm rounded-full" class:btn-primary={filter === c.id} class:btn-soft={filter === c.id} aria-pressed={filter === c.id} onclick={() => (filter = filter === c.id ? null : c.id)}>
						<span class="dot" style:background="var(--cat-{c.color})" aria-hidden="true"></span>{c.name} <span class="muted tabular">{c.members}</span>
					</button>
					<IconBtn icon="pencil" label="Edit" hint="Edit {c.name}" class="btn-ghost btn-xs" size={13} onclick={() => (editing = { ...c })} />
				</span>
			{/each}
			<form class="flex items-center gap-1" onsubmit={create}>
				<input class="input input-sm w-40" bind:value={newName} maxlength="60" placeholder="New category" aria-label="New category name" />
				<button class="btn btn-sm" disabled={!newName.trim()}><Icon name="plus" size={14} />Add</button>
			</form>
		</div>
		{#if editing}
			<form class="edit mt-3 flex flex-wrap items-center gap-2" onsubmit={saveEdit} in:fadeIn>
				<input class="input input-sm w-48" bind:value={editing.name} maxlength="60" aria-label="Category name" />
				<div class="flex gap-1" role="radiogroup" aria-label="Colour">
					{#each [1, 2, 3, 4, 5, 6, 7, 8] as n (n)}
						<button type="button" role="radio" aria-checked={editing.color === n} aria-label="Colour {n}" class="swatch" class:on={editing.color === n} style:background="var(--cat-{n})" onclick={() => (editing!.color = n)}></button>
					{/each}
				</div>
				<button class="btn btn-primary btn-sm">Save</button>
				<button type="button" class="btn btn-ghost btn-sm" onclick={() => (editing = null)}>Cancel</button>
				<span class="spacer"></span>
				<button type="button" class="btn btn-ghost btn-sm text-error" onclick={() => remove(editing!)}>Delete</button>
			</form>
		{/if}
	</div>

	<!-- Search and bulk actions -->
	<div class="flex flex-wrap items-center gap-2">
		<input class="input input-sm w-full max-w-xs" type="search" bind:value={query} placeholder="Search name, email or student ID" aria-label="Search students" />
		<span class="spacer"></span>
		{#if selected.size}
			<span class="small" in:fadeIn><strong>{selected.size}</strong> selected</span>
			<select class="select select-sm w-auto" bind:value={target} aria-label="Category">
				<option value="">Category…</option>
				{#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
			</select>
			<button class="btn btn-sm btn-primary" disabled={!target} onclick={() => assign(true)}>Add to</button>
			<button class="btn btn-sm" disabled={!target} onclick={() => assign(false)}>Remove from</button>
			<button class="btn btn-sm btn-ghost" onclick={() => (selected = new Set())}>Clear</button>
		{/if}
	</div>

	<div class="card card-border bg-base-100 p-0 shadow-sm table-wrap">
		{#if enrolments.length === 0}
			<EmptyState icon="users" title="No students yet">Share the join code from the Join code tab.</EmptyState>
		{:else}
			<table class="table">
				<thead>
					<tr>
						<th class="w-10"><input type="checkbox" class="checkbox checkbox-sm" checked={allShownSelected} onchange={toggleAll} aria-label="Select all shown" /></th>
						<th>Name</th><th>Student ID</th><th>Categories</th><th>Status</th><th></th>
					</tr>
				</thead>
				<tbody>
					{#each shown as en (en.id)}
						<tr class:sel={selected.has(en.id)}>
							<td><input type="checkbox" class="checkbox checkbox-sm" checked={selected.has(en.id)} onchange={() => toggle(en.id)} aria-label="Select {en.student_name}" /></td>
							<td><span class="flex items-center gap-2"><Avatar id={en.user_id ?? ''} name={en.student_name} avatar={en.student_avatar} size={30} /><span class="min-w-0">{en.student_name}<br /><span class="small muted">{en.student_email}</span></span></span></td>
							<td class="whitespace-nowrap">{en.student_number ?? '—'} <button class="btn btn-ghost btn-xs" onclick={() => oneditnumber(en)}>Edit</button></td>
							<td>
								<div class="flex flex-wrap gap-1">
									{#each en.categories ?? [] as cid (cid)}
										{@const c = byId[cid]}
										{#if c}
											<span class="badge badge-soft gap-1 pr-0.5"><span class="dot" style:background="var(--cat-{c.color})" aria-hidden="true"></span>{c.name}<button class="btn btn-ghost btn-xs btn-circle" aria-label="Remove {en.student_name} from {c.name}" onclick={() => untag(en, c)}><Icon name="x" size={11} /></button></span>
										{/if}
									{/each}
								</div>
							</td>
							<td><span class="badge badge-soft {en.status === 'active' ? 'ok' : en.status === 'pending' ? 'warn' : ''}">{en.status}</span></td>
							<td class="whitespace-nowrap">
								{#if en.status !== 'active'}<button class="btn btn-sm" onclick={() => onstatus(en, 'active')}>Approve</button>{/if}
								{#if en.status === 'pending'}<button class="btn btn-sm" onclick={() => onstatus(en, 'rejected')}>Reject</button>{/if}
								{#if en.status === 'active'}<button class="btn btn-sm btn-error btn-outline" onclick={() => onstatus(en, 'removed')}>Remove</button>{/if}
							</td>
						</tr>
					{:else}
						<tr><td colspan="6" class="muted">No students match.</td></tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</div>
</div>

<style>
	.chip-wrap { display: inline-flex; align-items: center; }
	.dot { width: 0.6rem; height: 0.6rem; border-radius: 999px; flex: none; }
	.swatch { width: 1.6rem; height: 1.6rem; border-radius: 999px; border: 2px solid var(--color-base-100); box-shadow: 0 0 0 1px var(--color-base-300); cursor: pointer; }
	.swatch.on { box-shadow: 0 0 0 2px var(--color-base-content); }
	tr.sel { background: color-mix(in oklab, var(--color-primary) 7%, transparent); }
</style>
