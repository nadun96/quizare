<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// One item's own groups for mark sheets (D-141). Enter a number and the
	// students are shared out; rename and move them, then save. The class's
	// groups are the classroom's, made on its Groups tab (D-158).
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { groupsProblem, itemKey, KIND_LABEL, splitIntoGroups, type ClassReport } from './classReport';

	let { classroomId, report, onsaved }: { classroomId: string; report: ClassReport; onsaved: (r: ClassReport) => void } = $props();
	type Editing = { name: string; members: string[] };
	const firstItem = () => (report.items[0] ? itemKey(report.items[0]) : '');
	let scope = $state(firstItem());
	let groups = $state<Editing[]>([]);
	let count = $state<number | null>(4);
	let saving = $state(false);
	const problem = $derived(groupsProblem(groups));
	const unplaced = $derived(report.students.filter((s) => !groups.some((g) => g.members.includes(s.user_id))));
	function current() {
		return report.item_groups.find((x) => itemKey(x) === scope)?.groups ?? [];
	}
	// Starts from what's saved whenever the scope changes.
	$effect(() => {
		void scope;
		groups = current().map((g) => ({ name: g.name, members: [...g.members] }));
	});

	function split() {
		if (!count || count < 1) return;
		groups = splitIntoGroups(report.students.map((s) => s.user_id), Math.min(count, 200));
	}
	function groupOf(user: string) {
		return groups.findIndex((g) => g.members.includes(user));
	}
	function move(user: string, to: number) {
		for (const g of groups) g.members = g.members.filter((u) => u !== user);
		if (to >= 0) groups[to].members.push(user);
	}
	function remove(i: number) {
		groups.splice(i, 1);
	}
	async function save() {
		if (problem) return;
		saving = true;
		try {
			const i = scope.indexOf(':');
			const body = { kind: scope.slice(0, i), id: scope.slice(i + 1), groups };
			onsaved(await api.put<ClassReport>(`/api/teacher/classrooms/${classroomId}/report-groups`, body));
			toast(tr("Groups saved"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("The groups didn’t save"), 'error');
		} finally {
			saving = false;
		}
	}
</script>

<section class="card card-border bg-base-100 p-4 vstack" aria-label={tr("Manage groups")}>
	<p class="m-0 small">{tr("The class’s groups are the classroom’s groups.")} <a href="?tab=groups" data-sveltekit-reload>{tr("Change them on the Groups tab")}</a></p>
	{#if scope}
	<div class="flex flex-wrap items-end gap-2">
		<label class="vstack gap-1 small">{tr("An item’s own groups")}
			<select class="select select-sm w-64" bind:value={scope}>
				{#each report.items as it (itemKey(it))}<option value={itemKey(it)}>{KIND_LABEL[it.kind]}: {it.title}</option>{/each}
			</select>
		</label>
		<label class="vstack gap-1 small">{tr("Number of groups")}
			<input class="input input-sm w-24 tabular" type="number" min="1" max="200" bind:value={count} />
		</label>
		<button class="btn btn-sm" disabled={!count || count < 1 || !report.students.length} onclick={split}><Icon name="shuffle" size={15} />{tr("Split at random")}</button>
		<button class="btn btn-sm" onclick={() => groups.push({ name: `Group ${groups.length + 1}`, members: [] })}>{tr("Add a group")}</button>
	</div>
	<p class="m-0 small muted">{tr("An item’s own groups give it a sheet of its own. It still appears on the class groups’ sheet.")}</p>
	{#if groups.length}
		<div class="flex flex-wrap gap-2">
			{#each groups as g, i (i)}
				<div class="flex items-center gap-1">
					<input class="input input-sm w-36" bind:value={g.name} maxlength="100" aria-label={tr("Name of group {0}", [i + 1])} />
					<span class="small muted tabular">{g.members.length}</span>
					<button class="btn btn-ghost btn-xs" aria-label={tr("Remove {0}", [g.name])} onclick={() => remove(i)}><Icon name="x" size={14} /></button>
				</div>
			{/each}
		</div>
		<div class="overflow-x-auto max-h-96">
			<table class="table table-sm">
				<thead><tr><th>{tr("Student")}</th><th>{tr("Group")}</th></tr></thead>
				<tbody>
					{#each report.students as s (s.user_id)}
						<tr>
							<td>{s.name}{#if s.student_number}<span class="text-xs muted tabular"> {s.student_number}</span>{/if}</td>
							<td>
								<select class="select select-sm w-48" value={groupOf(s.user_id)} aria-label={tr("Group of {0}", [s.name])} onchange={(e) => move(s.user_id, Number(e.currentTarget.value))}>
									<option value={-1}>{tr("No group")}</option>
									{#each groups as g, i (i)}<option value={i}>{g.name}</option>{/each}
								</select>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if unplaced.length}<p class="m-0 small text-warning">{tr("{0} {1} in a group.", [unplaced.length, unplaced.length === 1 ? tr("student isn’t") : tr("students aren’t")])}</p>{/if}
	{:else}
		<p class="m-0 small muted">{tr("No groups here{0}. Enter a number and split, or add them one by one.", [tr("; this item uses only the class’s")])}</p>
	{/if}
	{#if problem}<p class="m-0 small text-error" role="alert">{problem}</p>{/if}
	<div><button class="btn btn-primary btn-sm" disabled={saving || !!problem} onclick={save}>{saving ? tr("Saving…") : tr("Save groups")}</button></div>
	{/if}
</section>
