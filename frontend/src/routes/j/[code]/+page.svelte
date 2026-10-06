<script lang="ts">
	// QR landing page (FR-SS-03, FR-ACC-06): log in or register if needed,
	// give the classroom student ID if required, then go to the waiting room.
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';

	type Team = { id: string; name: string; color: number; members: number };
	type Preview = { session_id: string; title: string; quiz_title: string; status: string; student_id_required: boolean; attempt: { id: string } | null; team_mode: string; teams?: Team[] };

	const code = $derived(page.params.code ?? '');
	let preview = $state<Preview | null>(null);
	let error = $state('');
	let fieldError = $state('');
	let studentNumber = $state('');
	let busy = $state(false);
	let teamId = $state('');
	// Students choose their team (team_mode=self, D-44) before joining.
	const chooseTeam = $derived(preview?.team_mode === 'self' && !!preview.teams?.length);

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) {
			goto(loginUrl('/j/' + code), { replaceState: true });
			return;
		}
		if (auth.user.role !== 'student') {
			error = 'Only student accounts can join a quiz. Log in with your student account.';
			return;
		}
		load();
	});

	async function load() {
		try {
			preview = await api.get<Preview>('/api/join/sessions/' + encodeURIComponent(code));
			if (preview.attempt) goto('/attempt/' + preview.attempt.id, { replaceState: true });
			else if (!preview.student_id_required && !(preview.team_mode === 'self' && preview.teams?.length)) join();
		} catch (e) {
			error = e instanceof ApiError ? (e.status === 404 ? 'No quiz session has this code.' : e.message) : 'Could not load the session';
		}
	}

	async function join(e?: SubmitEvent) {
		e?.preventDefault();
		busy = true;
		fieldError = '';
		try {
			const st = await api.post<{ attempt_id: string }>('/api/join/sessions/' + encodeURIComponent(code), { student_number: studentNumber, team_id: teamId });
			goto('/attempt/' + st.attempt_id, { replaceState: true });
		} catch (err) {
			if (err instanceof ApiError && err.fields.student_number) fieldError = err.fields.student_number;
			else if (err instanceof ApiError && err.fields.team_id) fieldError = err.fields.team_id;
			else error = err instanceof ApiError ? err.message : 'Could not join';
		} finally {
			busy = false;
		}
	}
</script>

<div class="narrow">
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
		{#if error}
			<h1>Can't join</h1>
			<p class="alert alert-soft alert-error">{error}</p>
			<a href="/join">Enter a different code</a>
		{:else if preview && (preview.student_id_required || chooseTeam)}
			<h1>{preview.title}</h1>
			<form class="vstack" onsubmit={join}>
				{#if preview.student_id_required}
					<div>
						<label for="sid">Your student ID for this class</label>
						<input class="input w-full" id="sid" bind:value={studentNumber} required autocomplete="off" />
						<p class="small muted">Your teacher attaches this ID to your answers. You only enter it once per class.</p>
					</div>
				{/if}
				{#if chooseTeam}
					<fieldset class="m-0 border-0 p-0">
						<legend class="font-semibold mb-1">Choose your team</legend>
						<div class="team-pick">
							{#each preview.teams ?? [] as t (t.id)}
								<label class="topt" class:on={teamId === t.id}>
									<input type="radio" class="sr-only" name="team" value={t.id} bind:group={teamId} required />
									<span class="tdot" style:background="var(--cat-{t.color})" aria-hidden="true"></span>
									<span class="min-w-0"><span class="block truncate font-semibold">{t.name}</span><span class="small muted">{t.members} {t.members === 1 ? 'member' : 'members'}</span></span>
								</label>
							{/each}
						</div>
						<p class="small muted m-0 mt-1">You still answer the quiz on your own; your marks count towards your team.</p>
					</fieldset>
				{/if}
				{#if fieldError}<p class="field-error">{fieldError}</p>{/if}
				<button class="btn btn-primary" disabled={busy || (chooseTeam && !teamId)}>Join the quiz</button>
			</form>
		{:else}
			<p>Joining…</p>
		{/if}
	</div>
</div>

<style>
	.team-pick { display: grid; gap: 0.5rem; grid-template-columns: repeat(auto-fill, minmax(min(9rem, 100%), 1fr)); }
	.topt { display: flex; gap: 0.6rem; align-items: center; margin: 0; padding: 0.7rem 0.8rem; border-radius: var(--radius-box); border: 1.5px solid var(--color-base-300); cursor: pointer; font-weight: 400; min-height: 3.5rem; }
	.topt.on { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 7%, var(--color-base-100)); }
	.topt:has(input:focus-visible) { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.tdot { width: 0.9rem; height: 0.9rem; border-radius: 999px; flex: none; }
</style>
