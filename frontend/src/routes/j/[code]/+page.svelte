<script lang="ts">
	// QR landing page (FR-SS-03, FR-ACC-06): log in or register if needed,
	// give the classroom student ID if required, then go to the waiting room.
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';

	type Preview = { session_id: string; title: string; quiz_title: string; status: string; student_id_required: boolean; attempt: { id: string } | null };

	const code = $derived(page.params.code ?? '');
	let preview = $state<Preview | null>(null);
	let error = $state('');
	let fieldError = $state('');
	let studentNumber = $state('');
	let busy = $state(false);

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
			else if (!preview.student_id_required) join();
		} catch (e) {
			error = e instanceof ApiError ? (e.status === 404 ? 'No quiz session has this code.' : e.message) : 'Could not load the session';
		}
	}

	async function join(e?: SubmitEvent) {
		e?.preventDefault();
		busy = true;
		fieldError = '';
		try {
			const st = await api.post<{ attempt_id: string }>('/api/join/sessions/' + encodeURIComponent(code), { student_number: studentNumber });
			goto('/attempt/' + st.attempt_id, { replaceState: true });
		} catch (err) {
			if (err instanceof ApiError && err.fields.student_number) fieldError = err.fields.student_number;
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
		{:else if preview?.student_id_required}
			<h1>{preview.title}</h1>
			<form class="vstack" onsubmit={join}>
				<div>
					<label for="sid">Your student ID for this class</label>
					<input class="input w-full" id="sid" bind:value={studentNumber} required autocomplete="off" />
					{#if fieldError}<p class="field-error">{fieldError}</p>{/if}
					<p class="small muted">Your teacher attaches this ID to your answers. You only enter it once per class.</p>
				</div>
				<button class="btn btn-primary" disabled={busy}>Join the quiz</button>
			</form>
		{:else}
			<p>Joining…</p>
		{/if}
	</div>
</div>
