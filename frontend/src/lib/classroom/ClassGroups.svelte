<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A classroom's groups (D-158): make them, put students in them (a student
	// can be in several), choose each group's leader and assistant leaders, and
	// let students choose their own groups for a time. Quizzes, polls,
	// tutoring and the class report use these groups.
	import { api, ApiError } from '$lib/api';
	import { fromLocalInput, toLocalInput } from '$lib/tutorial/tutorial';
	import { confirmDialog, promptDialog } from '$lib/ui/dialog.svelte';
	import EmptyState from '$lib/ui/EmptyState.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import RowMenu from '$lib/ui/RowMenu.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { byRole, policyState, ROLES, shareOut, ungrouped, type ClassGroup, type GroupPerson, type GroupsView, type Role } from './groups';

	let { classroomId }: { classroomId: string } = $props();
	let view = $state<GroupsView | null>(null);
	let busy = $state(false);
	let newName = $state('');
	// Random groups (D-162).
	let randomOpen = $state(false);
	let rCount = $state(4);
	let rMax = $state<number | null>(null);
	let rBalance = $state(true);
	let rPrefix = $state('Group');
	let adding = $state<Record<string, string>>({});
	let q = $state('');
	// The policy being edited.
	let selfJoin = $state(false);
	let opens = $state('');
	let closes = $state('');
	let perStudent = $state(1);
	let maxSize = $state<number | null>(null);

	const base = $derived(`/api/teacher/classrooms/${classroomId}`);
	const loose = $derived(view ? ungrouped(view.students, view.groups) : []);
	const phase = $derived(view ? policyState(view.policy) : 'off');
	const needle = $derived(q.trim().toLowerCase());
	const shown = $derived(
		(view?.groups ?? []).filter((g) => !needle || g.name.toLowerCase().includes(needle) || (g.public_name ?? '').toLowerCase().includes(needle) || g.people.some((p) => p.name.toLowerCase().includes(needle)))
	);

	async function load() {
		const v = await api.get<GroupsView>(`${base}/groups`);
		view = v;
		selfJoin = v.policy.self_join;
		opens = toLocalInput(v.policy.opens_at);
		closes = toLocalInput(v.policy.closes_at);
		perStudent = v.policy.max_per_student;
		maxSize = v.policy.max_size;
	}
	$effect(() => {
		if (classroomId) load();
	});

	function problem(e: unknown, fallback: string) {
		toast(e instanceof ApiError ? (Object.values(e.fields ?? {})[0] ?? e.message) : fallback, 'error');
	}
	async function run(act: () => Promise<unknown>, done?: string) {
		busy = true;
		try {
			await act();
			if (done) toast(done);
			await load();
		} catch (e) {
			problem(e, tr("That didn’t work. Try again."));
		} finally {
			busy = false;
		}
	}

	const genderCounts = $derived.by(() => {
		const out = { female: 0, male: 0, unknown: 0 };
		for (const s of view?.students ?? []) out[(s.gender as 'female' | 'male') || 'unknown'] = (out[(s.gender as 'female' | 'male') || 'unknown'] ?? 0) + 1;
		return out;
	});
	function makeRandom(e: SubmitEvent) {
		e.preventDefault();
		run(async () => {
			const r = await api.post<{ groups: string[]; placed: number; left: number }>(`${base}/groups/random`, { count: Number(rCount), max: Number(rMax) || 0, balance: rBalance, prefix: rPrefix });
			randomOpen = false;
			toast(r.left ? tr("{0} groups made; {1} students placed, {2} didn’t fit", [r.groups.length, r.placed, r.left]) : tr("{0} groups made; {1} students placed", [r.groups.length, r.placed]));
		});
	}
	function create(e: SubmitEvent) {
		e.preventDefault();
		const name = newName.trim();
		if (!name) return;
		run(async () => {
			await api.post(`${base}/categories`, { name });
			newName = '';
		}, tr("Group “{0}” made", [name]));
	}
	async function rename(g: ClassGroup) {
		const name = await promptDialog({ title: tr("Rename {0}", [g.name]), label: tr("Group name"), value: g.name, maxlength: 60 });
		if (name && name.trim() !== g.name) run(() => api.patch(`/api/teacher/categories/${g.id}`, { name: name.trim(), color: g.color }));
	}
	function recolor(g: ClassGroup, color: number) {
		run(() => api.patch(`/api/teacher/categories/${g.id}`, { name: g.name, color }));
	}
	async function remove(g: ClassGroup) {
		const ok = await confirmDialog({ title: tr("Delete “{0}”?", [g.name]), body: tr("Its students stay in the classroom and in their other groups."), confirm: tr("Delete group"), danger: true });
		if (ok) run(() => api.del(`/api/teacher/categories/${g.id}`), tr("Deleted"));
	}
	function add(g: ClassGroup) {
		const en = adding[g.id];
		if (!en) return;
		run(async () => {
			await api.post(`/api/teacher/categories/${g.id}/members`, { enrolment_ids: [en], assigned: true });
			adding[g.id] = '';
		});
	}
	function take(g: ClassGroup, p: GroupPerson) {
		run(() => api.post(`/api/teacher/categories/${g.id}/members`, { enrolment_ids: [p.enrolment_id], assigned: false }));
	}
	function setRole(g: ClassGroup, p: GroupPerson, role: Role) {
		run(() => api.put(`/api/teacher/categories/${g.id}/members/${p.enrolment_id}/role`, { role }));
	}
	function clearName(g: ClassGroup) {
		run(() => api.del(`/api/teacher/categories/${g.id}/public-name`), tr("Back to “{0}”", [g.name]));
	}
	async function share() {
		if (!view || !view.groups.length || !loose.length) return;
		const ok = await confirmDialog({ title: tr("Share out {0} students?", [loose.length]), body: tr("Students in no group go at random to the smallest groups, so the groups end up even."), confirm: tr("Share out") });
		if (!ok) return;
		const plan = shareOut(view.groups.map((g) => ({ id: g.id, size: g.members })), loose.map((s) => s.enrolment_id!));
		run(async () => {
			for (const [id, ens] of plan) if (ens.length) await api.post(`/api/teacher/categories/${id}/members`, { enrolment_ids: ens, assigned: true });
		}, tr("Shared out"));
	}
	function savePolicy(e: SubmitEvent) {
		e.preventDefault();
		run(() => api.put(`${base}/group-policy`, { self_join: selfJoin, opens_at: fromLocalInput(opens) ?? null, closes_at: fromLocalInput(closes) ?? null, max_per_student: perStudent, max_size: maxSize || null }), tr("Saved"));
	}
	const when = (s: string | null) => (s ? new Date(s).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '');
	const roleLabel = (r: Role) => tr(ROLES.find((x) => x.v === r)?.label ?? r);
</script>

{#if !view}
	<Skeleton lines={4} />
{:else}
	<div class="vstack">
		<p class="m-0 small muted">{tr("Groups belong to the classroom. A student can be in several. Quizzes, polls and tutoring can play group against group, with each group’s leader as its captain, and the class report gives each group its marks.")}</p>

		<form class="card card-border bg-base-100 p-4 vstack gap-3" onsubmit={savePolicy} aria-label={tr("Students choosing groups")}>
			<div class="flex flex-wrap items-center gap-2">
				<h2 class="m-0 text-base grow">{tr("Students choosing groups")}</h2>
				{#if phase === 'open'}<span class="badge badge-success badge-soft">{tr("Open now")}{#if view.policy.closes_at}{' · '}{tr("until {0}", [when(view.policy.closes_at)])}{/if}</span>
				{:else if phase === 'soon'}<span class="badge badge-info badge-soft">{tr("Opens {0}", [when(view.policy.opens_at)])}</span>
				{:else if phase === 'closed'}<span class="badge badge-ghost">{tr("Closed")}</span>
				{:else}<span class="badge badge-ghost">{tr("Off")}</span>{/if}
			</div>
			<label class="flex items-center gap-2 small"><input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={selfJoin} />{tr("Let students join groups themselves")}</label>
			{#if selfJoin}
				<div class="flex flex-wrap items-end gap-3">
					<label class="vstack gap-1 small">{tr("From")}<input class="input input-sm w-auto" type="datetime-local" bind:value={opens} /></label>
					<label class="vstack gap-1 small">{tr("Until")}<input class="input input-sm w-auto" type="datetime-local" bind:value={closes} min={opens || undefined} /></label>
					<label class="vstack gap-1 small">{tr("Groups each student may choose")}<input class="input input-sm w-24 tabular" type="number" min="1" max="50" required bind:value={perStudent} /></label>
					<label class="vstack gap-1 small">{tr("Most students in a group")}<input class="input input-sm w-28 tabular" type="number" min="1" max="1000" placeholder={tr("No limit")} bind:value={maxSize} /></label>
				</div>
				<p class="m-0 text-xs muted">{tr("Leave the times empty to open now and stay open until you turn it off. Students can leave a group they chose while choosing is open; groups you put them in stay.")}</p>
			{/if}
			<div><button class="btn btn-primary btn-sm" disabled={busy}>{tr("Save")}</button></div>
		</form>

		<div class="flex flex-wrap items-center gap-2">
			<form class="flex items-center gap-1" onsubmit={create}>
				<input class="input input-sm w-48" bind:value={newName} maxlength="60" placeholder={tr("New group")} aria-label={tr("New group name")} />
				<button class="btn btn-sm" disabled={busy || !newName.trim()}><Icon name="plus" size={14} />{tr("Add group")}</button>
			</form>
			<button class="btn btn-sm" disabled={busy || !view.groups.length || !loose.length} onclick={share}><Icon name="shuffle" size={15} />{tr("Share out students in no group")}</button>
			<button class="btn btn-sm" aria-expanded={randomOpen} onclick={() => (randomOpen = !randomOpen)}><Icon name="shuffle" size={15} />{tr("Random groups")}</button>
			<span class="grow"></span>
			{#if view.groups.length}<input class="input input-sm w-full sm:w-56" type="search" bind:value={q} placeholder={tr("Group or student")} aria-label={tr("Search groups")} />{/if}
		</div>
		{#if randomOpen}
			<form class="card card-border bg-base-100 p-4 vstack gap-3" onsubmit={makeRandom} aria-label={tr("Random groups")}>
				<p class="m-0 small muted">{tr("Makes new groups and shares all {0} students among them at random. Students keep their other groups. Those who don’t fit stay out for you to place; you can move anyone afterwards, past the limit.", [view.students.length])}</p>
				<div class="flex flex-wrap items-end gap-3">
					<label class="vstack gap-1 small">{tr("Groups")}<input class="input input-sm w-24 tabular" type="number" min="2" max="50" required bind:value={rCount} /></label>
					<label class="vstack gap-1 small">{tr("Most a group")}<input class="input input-sm w-28 tabular" type="number" min="1" max="1000" placeholder={tr("No limit")} bind:value={rMax} /></label>
					<label class="vstack gap-1 small">{tr("Named")}<input class="input input-sm w-32" maxlength="50" bind:value={rPrefix} /></label>
					<label class="flex items-center gap-2 small pb-1"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={rBalance} />{tr("Spread genders evenly")}</label>
				</div>
				<p class="m-0 text-xs muted">{tr("In this classroom: {0} female, {1} male, {2} not given.", [genderCounts.female, genderCounts.male, genderCounts.unknown])}{#if rCount > 0}{' '}{tr("About {0} a group.", [Math.ceil(view.students.length / Number(rCount))])}{/if}</p>
				<div class="flex gap-2"><button class="btn btn-primary btn-sm" disabled={busy}>{tr("Make the groups")}</button><button type="button" class="btn btn-ghost btn-sm" onclick={() => (randomOpen = false)}>{tr("Cancel")}</button></div>
			</form>
		{/if}
		{#if loose.length && view.groups.length}<p class="m-0 small text-warning">{tr("In no group: {0}", [loose.map((s) => s.name).join(', ')])}</p>{/if}

		{#if !view.groups.length}
			<div class="card card-border bg-base-100"><EmptyState icon="users" title={tr("No groups yet")}>{tr("Add a group, then put students in it, or let students choose.")}</EmptyState></div>
		{:else}
			<div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
				{#each shown as g (g.id)}
					{@const out = view.students.filter((s) => !g.people.some((p) => p.user_id === s.user_id))}
					<section class="card card-border bg-base-100 p-4 vstack gap-2 m-0 self-start" aria-label={g.name}>
						<div class="flex items-start gap-2">
							<span class="dot mt-1.5" style:background="var(--cat-{g.color})" aria-hidden="true"></span>
							<div class="grow min-w-0">
								<h3 class="m-0 text-base break-words">{g.public_name ?? g.name}</h3>
								{#if g.public_name}<p class="m-0 text-xs muted">{tr("Named by its leader · yours: {0}", [g.name])} <button class="link text-xs" onclick={() => clearName(g)}>{tr("Use yours")}</button></p>{/if}
							</div>
							<span class="badge badge-ghost tabular" title={tr("Students")}>{g.members}{#if view.policy.max_size}/{view.policy.max_size}{/if}</span>
							<RowMenu hint={tr("More actions for {0}", [g.name])} actions={[
								{ label: 'Rename', icon: 'pencil', onclick: () => rename(g) },
								{ label: 'Delete group', icon: 'trash', danger: true, onclick: () => remove(g) }
							]} />
						</div>
						<div class="flex gap-1" role="radiogroup" aria-label={tr("Colour of {0}", [g.name])}>
							{#each [1, 2, 3, 4, 5, 6, 7, 8] as c (c)}
								<button class="swatch" class:on={c === g.color} style:background="var(--cat-{c})" role="radio" aria-checked={c === g.color} aria-label={tr("Colour {0}", [c])} disabled={busy} onclick={() => c !== g.color && recolor(g, c)}></button>
							{/each}
						</div>
						{#if g.people.length}
							<ul class="m-0 p-0 list-none vstack gap-1">
								{#each byRole(g.people) as p (p.user_id)}
									<li class="flex items-center gap-2">
										<span class="grow min-w-0 truncate small">{p.name}{#if p.self_joined}<span class="text-xs muted">{' · '}{tr("chose it")}</span>{/if}</span>
										<select class="select select-xs w-auto" value={p.role} aria-label={tr("Role of {0}", [p.name])} disabled={busy} onchange={(e) => setRole(g, p, e.currentTarget.value as Role)}>
											{#each ROLES as r (r.v)}<option value={r.v}>{roleLabel(r.v)}</option>{/each}
										</select>
										<button class="btn btn-ghost btn-xs" aria-label={tr("Take {0} out of {1}", [p.name, g.name])} disabled={busy} onclick={() => take(g, p)}><Icon name="x" size={13} /></button>
									</li>
								{/each}
							</ul>
						{:else}
							<p class="m-0 small muted">{tr("No students yet.")}</p>
						{/if}
						{#if out.length}
							<div class="flex items-center gap-1">
								<select class="select select-sm grow" bind:value={adding[g.id]} aria-label={tr("Add a student to {0}", [g.name])}>
									<option value="">{tr("Add a student…")}</option>
									{#each out as s (s.user_id)}<option value={s.enrolment_id}>{s.name}{s.student_number ? ` (${s.student_number})` : ''}</option>{/each}
								</select>
								<button class="btn btn-sm" disabled={busy || !adding[g.id]} onclick={() => add(g)}>{tr("Add")}</button>
							</div>
						{/if}
					</section>
				{/each}
			</div>
			{#if !shown.length}<p class="muted small m-0">{tr("No group matches.")}</p>{/if}
		{/if}
	</div>
{/if}

<style>
	.dot {
		width: 0.75rem;
		height: 0.75rem;
		border-radius: 999px;
		flex: none;
	}
	.swatch {
		width: 1.25rem;
		height: 1.25rem;
		border-radius: 999px;
		border: 2px solid transparent;
		cursor: pointer;
	}
	.swatch.on {
		border-color: var(--color-base-content);
	}
</style>
