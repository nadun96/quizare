<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A student's groups in a classroom (D-158): the ones they're in, with
	// who else is; choosing groups while the teacher allows it; and naming the
	// group they lead.
	import { api, ApiError } from '$lib/api';
	import { promptDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { byRole, joinBlock, policyState, type MyGroup, type MyGroupsView, type Role } from './groups';

	let { classroomId }: { classroomId: string } = $props();
	let view = $state<MyGroupsView | null>(null);
	let busy = $state('');

	async function load() {
		try {
			view = await api.get<MyGroupsView>(`/api/my/classrooms/${classroomId}/groups`);
		} catch {
			view = null;
		}
	}
	$effect(() => {
		if (classroomId) load();
	});

	const mine = $derived((view?.groups ?? []).filter((g) => g.mine));
	const others = $derived((view?.groups ?? []).filter((g) => !g.mine));
	const phase = $derived(view ? policyState(view.policy) : 'off');
	const when = (s: string | null) => (s ? new Date(s).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '');
	const roleText = (r?: Role) => (r === 'leader' ? tr("Leader") : r === 'assistant' ? tr("Assistant leader") : '');

	async function run(id: string, act: () => Promise<unknown>, done: string) {
		busy = id;
		try {
			await act();
			toast(done);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields ?? {})[0] ?? e.message) : tr("That didn’t work. Try again."), 'error');
		} finally {
			busy = '';
		}
	}
	const join = (g: MyGroup) => run(g.id, () => api.post(`/api/my/groups/${g.id}`), tr("You joined {0}", [g.name]));
	const leave = (g: MyGroup) => run(g.id, () => api.del(`/api/my/groups/${g.id}`), tr("You left {0}", [g.name]));
	async function name(g: MyGroup) {
		const v = await promptDialog({
			title: tr("Name your group"),
			body: tr("Everyone sees this name in quizzes, polls and tutoring. Your teacher calls it “{0}”. Leave it empty to use that.", [g.teacher_name]),
			label: tr("Group name"),
			value: g.public_name ?? '',
			maxlength: 40,
			allowEmpty: true
		});
		if (v !== null) run(g.id, () => api.put(`/api/my/groups/${g.id}/name`, { name: v }), tr("Saved"));
	}
</script>

{#if view && (view.groups.length || phase !== 'off')}
	<section class="vstack gap-2" aria-label={tr("Your groups")}>
		<div class="flex flex-wrap items-center gap-2">
			<h2 class="m-0 text-base grow">{tr("Your groups")}</h2>
			{#if phase === 'open'}<span class="badge badge-success badge-soft">{tr("You can choose groups")}{#if view.policy.closes_at}{' · '}{tr("until {0}", [when(view.policy.closes_at)])}{/if}</span>
			{:else if phase === 'soon'}<span class="badge badge-info badge-soft">{tr("Choosing opens {0}", [when(view.policy.opens_at)])}</span>{/if}
		</div>
		{#if mine.length}
			<div class="grid gap-2 sm:grid-cols-2">
				{#each mine as g (g.id)}
					<div class="card card-border bg-base-100 p-3 vstack gap-1">
						<div class="flex items-center gap-2">
							<span class="dot" style:background="var(--cat-{g.color})" aria-hidden="true"></span>
							<span class="font-semibold grow min-w-0 break-words">{g.name}</span>
							{#if g.role && g.role !== 'member'}<span class="badge badge-primary badge-soft badge-sm">{roleText(g.role)}</span>{/if}
						</div>
						{#if g.members?.length}<p class="m-0 small muted">{byRole(g.members).map((m) => (m.role === 'leader' ? `${m.name} ★` : m.name)).join(', ')}</p>{/if}
						<div class="flex flex-wrap gap-1">
							{#if g.role === 'leader'}<button class="btn btn-xs" disabled={busy === g.id} onclick={() => name(g)}><Icon name="pencil" size={13} />{tr("Name the group")}</button>{/if}
							{#if g.self_joined && view.policy.open}<button class="btn btn-xs btn-ghost" disabled={busy === g.id} onclick={() => leave(g)}>{tr("Leave")}</button>{/if}
						</div>
					</div>
				{/each}
			</div>
		{:else}
			<p class="m-0 small muted">{tr("You’re not in a group yet.")}</p>
		{/if}
		{#if phase === 'open' && others.length}
			<p class="m-0 small muted">{tr("You can choose {0} {1} yourself; you’ve chosen {2}.", [view.policy.max_per_student, view.policy.max_per_student === 1 ? tr("group") : tr("groups"), view.chosen])}</p>
			<ul class="list card card-border bg-base-100">
				{#each others as g (g.id)}
					{@const why = joinBlock(g, view)}
					<li class="item">
						<span class="dot" style:background="var(--cat-{g.color})" aria-hidden="true"></span>
						<span class="grow min-w-0">{g.name} <span class="text-xs muted tabular">· {g.size}{#if view.policy.max_size}/{view.policy.max_size}{/if}</span></span>
						{#if why === 'full'}<span class="small muted">{tr("Full")}</span>
						{:else}<button class="btn btn-sm" disabled={!!why || busy === g.id} title={why === 'limit' ? tr("Leave a group you chose first") : undefined} onclick={() => join(g)}>{tr("Join")}</button>{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</section>
{/if}

<style>
	.dot { width: 0.7rem; height: 0.7rem; border-radius: 999px; flex: none; }
	.list { list-style: none; margin: 0; padding: 0; overflow: hidden; }
	.item { display: flex; align-items: center; gap: 0.6rem; padding: 0.55rem 1rem; }
	.item + .item { border-top: 1px solid var(--color-base-200); }
</style>
