<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A classroom's name, description and details (D-128), for its owner.
	import { api, ApiError } from '../api';
	import type { Classroom } from '../types';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import PropsFields from './PropsFields.svelte';
	import { emptyProps, noFacets, type Facets, type Props } from './props';

	let { classroom, onsaved }: { classroom: Classroom; onsaved: (c: Classroom) => void } = $props();
	let name = $state('');
	let description = $state('');
	let details = $state<Props>(emptyProps());
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);
	let facets = $state<Facets>(noFacets());
	$effect(() => {
		name = classroom.name;
		description = classroom.description;
		details = { ...emptyProps(), ...classroom.props, tags: [...(classroom.props?.tags ?? [])] };
	});
	api.get<Facets>('/api/teacher/classroom-facets').then((f) => (facets = f), () => {});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		errors = {};
		try {
			const c = await api.patch<Classroom>('/api/teacher/classrooms/' + classroom.id, { name, description, props: details });
			toast(tr("Details saved"));
			onsaved(c);
		} catch (err) {
			if (err instanceof ApiError) errors = err.fields;
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : tr("Could not save"), 'error');
		} finally {
			busy = false;
		}
	}
</script>

<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3" onsubmit={save}>
	<h3 class="m-0 flex items-center gap-2 text-base"><Icon name="book" size={18} />{tr("Details")}</h3>
	<div class="grid gap-3 sm:grid-cols-2">
		<label class="flex flex-col gap-1"><span class="small">{tr("Name")}</span><input class="input w-full" class:input-error={errors.name} bind:value={name} maxlength="200" required /></label>
		<label class="flex flex-col gap-1"><span class="small">{tr("Description")}</span><input class="input w-full" bind:value={description} placeholder={tr("Optional")} /></label>
	</div>
	<PropsFields bind:value={details} {facets} dates {errors} id="classroom-details" />
	<div><button class="btn btn-primary" disabled={busy}>{tr("Save details")}</button></div>
</form>
