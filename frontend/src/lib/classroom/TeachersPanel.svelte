<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A classroom's teachers (D-121): the owner invites co-teachers by email
	// and removes them; a co-teacher sees who teaches with them and can leave.
	import { goto } from '$app/navigation';
	import { api, ApiError } from '../api';
	import { auth } from '../session.svelte';
	import Avatar from '../ui/Avatar.svelte';
	import Icon from '../ui/Icon.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';

	type Teacher = { user_id: string; name: string; email: string; avatar?: string; role: 'owner' | 'coteacher'; status: 'active' | 'invited'; invited_at?: string };
	let { classroomId, classroomName, owner }: { classroomId: string; classroomName: string; owner: boolean } = $props();
	let teachers = $state<Teacher[] | null>(null);
	let email = $state('');
	let error = $state('');
	let busy = $state(false);
	const base = $derived('/api/teacher/classrooms/' + classroomId + '/teachers');

	async function load() {
		teachers = (await api.get<{ teachers: Teacher[] }>(base)).teachers;
	}
	$effect(() => {
		void classroomId;
		load();
	});

	async function invite(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		busy = true;
		try {
			const t = await api.post<Teacher>(base, { email });
			toast(tr("Invitation sent to {0}", [t.name]));
			email = '';
			await load();
		} catch (err) {
			error = err instanceof ApiError ? (err.fields.email ?? err.message) : tr("Could not invite");
		} finally {
			busy = false;
		}
	}
	async function remove(t: Teacher) {
		const me = t.user_id === auth.user?.id;
		const ok = await confirmDialog(
			me
				? { title: `Leave “${classroomName}”?`, body: 'You stop teaching it: its students, quizzes and results go from your account. What you made stays in the classroom.', confirm: 'Leave classroom', danger: true }
				: { title: t.status === 'invited' ? `Withdraw the invitation to ${t.name}?` : `Remove ${t.name}?`, body: t.status === 'invited' ? 'They won’t be able to accept it.' : 'They stop teaching this classroom. What they made stays in it.', confirm: t.status === 'invited' ? 'Withdraw' : 'Remove', danger: true }
		);
		if (!ok) return;
		try {
			await api.del(base + '/' + t.user_id);
			if (me) {
				toast(tr("You left “{0}”", [classroomName]));
				goto('/t');
			} else {
				toast(t.status === 'invited' ? tr("Invitation withdrawn") : tr("{0} removed", [t.name]));
				load();
			}
		} catch (err) {
			toast(err instanceof ApiError ? err.message : tr("That didn’t work"), 'error');
		}
	}
</script>

<div class="vstack">
	<div class="card card-border bg-base-100 p-4 sm:p-6 vstack">
		<div>
			<h2 class="m-0 text-lg">{tr("Teachers")}</h2>
			<p class="m-0 small muted">{tr("Co-teachers teach this classroom with {0}: its students, modules and topics, quizzes and their sessions and marking, tutorials, presentations, results and trends. The classroom itself (its name, settings, join code, archiving and deleting, its teachers, and removing students) stays the owner’s.", [owner ? 'you' : tr("its owner")])}</p>
		</div>
		{#if !teachers}
			<p class="m-0 muted">{tr("Loading…")}</p>
		{:else}
			<ul class="list">
				{#each teachers as t (t.user_id)}
					<li>
						<Avatar id={t.user_id} name={t.name} avatar={t.avatar ?? ""} size={36} />
						<div class="min-w-0 grow">
							<p class="m-0 font-semibold truncate">{t.name}{#if t.user_id === auth.user?.id}{' '}<span class="muted font-normal">{tr("(you)")}</span>{/if}</p>
							<p class="m-0 small muted truncate">{t.email}</p>
						</div>
						{#if t.role === 'owner'}<span class="badge badge-soft">{tr("Owner")}</span>
						{:else if t.status === 'invited'}<span class="badge badge-soft warn">{tr("Invited")}</span>
						{:else}<span class="badge badge-soft ok">{tr("Co-teacher")}</span>{/if}
						{#if t.role === 'coteacher' && (owner || t.user_id === auth.user?.id)}
							<button class="btn btn-sm btn-ghost" onclick={() => remove(t)}>{t.user_id === auth.user?.id ? tr("Leave") : t.status === 'invited' ? tr("Withdraw") : tr("Remove")}</button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</div>

	{#if owner}
		<form class="card card-border bg-base-100 p-4 sm:p-6 vstack" onsubmit={invite}>
			<h3 class="m-0 text-base">{tr("Invite a co-teacher")}</h3>
			<div class="flex flex-wrap items-end gap-2">
				<div class="grow" style="min-width: 14rem">
					<label for="co-email" class="small font-semibold">{tr("Their email")}</label>
					<input id="co-email" class="input w-full" type="email" placeholder={tr("teacher@school.lk")} bind:value={email} required aria-invalid={!!error} />
				</div>
				<button class="btn btn-primary" disabled={busy}><Icon name="send" size={16} />{tr("Send invitation")}</button>
			</div>
			{#if error}<p class="field-error m-0">{error}</p>{/if}
			<p class="m-0 small muted">{tr("They need a teacher account here. They get an email, and the invitation waits on their Classrooms page until they accept it.")}</p>
		</form>
	{/if}
</div>

<style>
	.list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.5rem; }
	.list li { display: flex; align-items: center; gap: 0.75rem; padding: 0.4rem 0; border-bottom: 1px solid var(--color-base-200); }
	.list li:last-child { border-bottom: 0; }
</style>
