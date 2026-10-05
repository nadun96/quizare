<script lang="ts">
	// Classroom enrolment by join code, link or QR (FR-CLS-03, FR-CLS-05).
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';

	type Preview = { classroom_id: string; name: string; student_id_required: boolean; enrolment: { status: string } | null };
	const code = $derived(page.params.code ?? '');
	let p = $state<Preview | null>(null);
	let studentNumber = $state('');
	let error = $state('');
	let done = $state('');

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) {
			goto(loginUrl('/c/' + code), { replaceState: true });
			return;
		}
		api.get<Preview>('/api/join/classrooms/' + encodeURIComponent(code))
			.then((x) => (p = x))
			.catch((e) => (error = e instanceof ApiError && e.status === 404 ? 'No classroom has this code.' : String(e.message ?? e)));
	});

	async function enrol(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			const en = await api.post<{ status: string }>('/api/enrolments', { join_code: code, student_number: studentNumber });
			done = en.status === 'pending' ? 'Request sent. Your teacher will approve your enrolment.' : 'You are enrolled.';
		} catch (err) {
			error = err instanceof ApiError ? (err.fields.student_number ?? err.message) : 'Could not enrol';
		}
	}
</script>

<div class="narrow">
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
		{#if p}
			<h1>{p.name}</h1>
			{#if done}
				<p class="alert alert-soft alert-success">{done}</p>
				<a href="/my">Go to my quizzes</a>
			{:else if p.enrolment && p.enrolment.status !== 'removed'}
				<p class="alert alert-soft alert-success">You are {p.enrolment.status === 'pending' ? 'waiting for approval in' : 'enrolled in'} this classroom.</p>
			{:else}
				<form class="vstack" onsubmit={enrol}>
					{#if p.student_id_required}
						<div><label for="sid">Your student ID for this class</label><input class="input w-full" id="sid" bind:value={studentNumber} required /></div>
					{/if}
					<button class="btn btn-primary">Enrol</button>
				</form>
			{/if}
		{/if}
		{#if error}<p class="alert alert-soft alert-error">{error}</p>{/if}
	</div>
</div>
