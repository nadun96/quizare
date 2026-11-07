<script lang="ts" module>
	export type ManagerRow = {
		id: string;
		email: string;
		name: string;
		role: string;
		status: string;
		manager?: { features: string[] };
		granted_at?: string;
		granted_by?: string;
		last_active_at?: string;
	};
	export type Teacher = { id: string; name: string; email: string; role: string; manager?: { features: string[] } };
</script>

<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Making a manager, or changing one's features (PL-FR-10 to PL-FR-12). The
	// admin decides when assigning: an existing teacher who keeps teaching, or
	// a new manager-only account (PO-29). Nothing is ticked by default
	// (PL-NFR-07).
	import { api, ApiError } from '../api';
	import { FEATURES, withImplied, type Feature } from '../staff';
	import { toast } from '../ui/toast.svelte';

	let {
		open = $bindable(false),
		editing = null,
		teacher = null,
		onsaved
	}: { open?: boolean; editing?: ManagerRow | null; teacher?: Teacher | null; onsaved: () => void } = $props();

	let el = $state<HTMLDialogElement>();
	let kind = $state<'teacher' | 'new'>('teacher');
	let picked = $state<Teacher | null>(null);
	let search = $state('');
	let found = $state<Teacher[]>([]);
	let name = $state('');
	let email = $state('');
	let features = $state<Feature[]>([]);
	let errors = $state<Record<string, string>>({});
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (!el) return;
		if (open && !el.open) {
			reset();
			el.showModal();
		} else if (!open && el.open) el.close();
	});

	function reset() {
		errors = {};
		error = '';
		search = '';
		found = [];
		name = '';
		email = '';
		kind = 'teacher';
		picked = teacher;
		features = (editing?.manager?.features ?? []) as Feature[];
		if (!editing && !teacher) findTeachers();
	}

	let timer: ReturnType<typeof setTimeout> | undefined;
	function onSearch() {
		clearTimeout(timer);
		timer = setTimeout(findTeachers, 300);
	}
	async function findTeachers() {
		const q = new URLSearchParams({ role: 'teacher', status: 'active', size: '8', sort: 'name' });
		if (search.trim()) q.set('q', search.trim());
		const res = await api.get<{ users: Teacher[] }>('/api/admin/users?' + q);
		found = res.users.filter((u) => !u.manager);
	}

	function toggle(f: Feature, on: boolean) {
		features = on ? withImplied([...features, f]) : features.filter((x) => x !== f && !(f === 'view_users' && x === 'manage_users'));
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		error = '';
		busy = true;
		try {
			if (editing) {
				await api.put('/api/admin/managers/' + editing.id + '/features', { features });
				toast(tr("Features saved; they apply at once"));
			} else if (kind === 'teacher') {
				if (!picked) {
					error = tr("Choose a teacher.");
					return;
				}
				await api.post('/api/admin/managers', { user_id: picked.id, features });
				toast(tr("{0} is now a manager", [picked.name]));
			} else {
				await api.post('/api/admin/managers', { name, email, features });
				toast(tr("Manager account created; they were emailed a link to set a password"));
			}
			open = false;
			onsaved();
		} catch (err) {
			if (err instanceof ApiError) {
				errors = err.fields;
				if (!Object.keys(err.fields).length) error = err.message;
			} else throw err;
		} finally {
			busy = false;
		}
	}
</script>

<dialog bind:this={el} class="modal modal-bottom sm:modal-middle" aria-labelledby="mgr-h" oncancel={(e) => { e.preventDefault(); open = false; }}>
	<form class="modal-box vstack" onsubmit={save}>
		<h2 id="mgr-h" class="m-0 text-lg">{editing ? tr("{0}'s features", [editing.name]) : tr("Add a manager")}</h2>
		{#if !editing}
			{#if teacher}
				<p class="m-0">{tr("Make")} <strong>{teacher.name}</strong> {tr("({0}) a manager. They keep teaching and get a “Manage” area.", [teacher.email])}</p>
			{:else}
				<fieldset class="vstack gap-1">
					<legend class="small muted">{tr("Who")}</legend>
					<label class="flex items-start gap-2 font-normal"><input type="radio" class="radio radio-sm mt-1" bind:group={kind} value="teacher" /><span>{tr("An existing teacher")} <span class="small muted">{tr("— keeps teaching and gets a “Manage” area")}</span></span></label>
					<label class="flex items-start gap-2 font-normal"><input type="radio" class="radio radio-sm mt-1" bind:group={kind} value="new" /><span>{tr("A new manager-only account")} <span class="small muted">{tr("— no teaching pages; emailed a link to set a password")}</span></span></label>
				</fieldset>
				{#if kind === 'teacher'}
					<div class="vstack gap-1">
						<label for="mgr-search">{tr("Teacher")}</label>
						<input id="mgr-search" class="input w-full" type="search" placeholder={tr("Search name or email")} bind:value={search} oninput={onSearch} autocomplete="off" />
						<ul class="pick" aria-label={tr("Teachers")}>
							{#each found as u (u.id)}
								<li><label class="flex items-center gap-2 font-normal"><input type="radio" class="radio radio-sm" name="mgr-teacher" checked={picked?.id === u.id} onchange={() => (picked = u)} /><span>{u.name} <span class="small muted">{u.email}</span></span></label></li>
							{:else}
								<li class="small muted">{tr("No active teachers match.")}</li>
							{/each}
						</ul>
					</div>
				{:else}
					<div><label for="mgr-name">{tr("Name")}</label><input id="mgr-name" class="input w-full" bind:value={name} required maxlength="100" aria-invalid={!!errors.name} />{#if errors.name}<p class="field-error">{errors.name}</p>{/if}</div>
					<div><label for="mgr-email">{tr("Email")}</label><input id="mgr-email" class="input w-full" type="email" bind:value={email} required aria-invalid={!!errors.email} />{#if errors.email}<p class="field-error">{errors.email}</p>{/if}</div>
				{/if}
			{/if}
		{/if}
		<fieldset class="vstack gap-2">
			<legend class="small muted">{tr("Features")}{#if !editing}{tr(" — a new manager has none until you tick some")}{/if}</legend>
			{#each FEATURES as f (f.id)}
				<label class="flex items-start gap-2 font-normal">
					<input type="checkbox" class="checkbox checkbox-sm mt-1" checked={features.includes(f.id)} onchange={(e) => toggle(f.id, e.currentTarget.checked)} />
					<span>{tr(f.label)}<br /><span class="small muted">{tr(f.hint)}</span></span>
				</label>
				{#if f.warn && features.includes(f.id)}<p class="alert alert-soft alert-warning small m-0" role="alert">{f.warn}</p>{/if}
			{/each}
			{#if errors.features}<p class="field-error">{errors.features}</p>{/if}
		</fieldset>
		<p class="small muted m-0">{tr("Managers can never act on admins or other managers, nor make managers.")}</p>
		{#if error}<p class="alert alert-soft alert-error small m-0">{error}</p>{/if}
		<div class="modal-action">
			<button type="button" class="btn" onclick={() => (open = false)}>{tr("Cancel")}</button>
			<button class="btn btn-primary" disabled={busy}>{editing ? tr("Save") : tr("Make manager")}</button>
		</div>
	</form>
</dialog>

<style>
	.pick { list-style: none; margin: 0; padding: 0.25rem 0; max-height: 14rem; overflow-y: auto; display: grid; gap: 0.35rem; }
	fieldset { border: 0; padding: 0; margin: 0; }
</style>
