<script lang="ts">
	// Chooses one of a classroom's topics, by module (D-147). With no topics
	// there's nothing to choose, so it shows nothing.
	import { api } from '../api';
	import type { Module, Topic } from '../types';

	let { classroomId, value = $bindable(null), compact = false }: { classroomId: string; value?: string | null; compact?: boolean } = $props();
	let groups = $state<{ module: Module; topics: Topic[] }[]>([]);

	async function load(id: string) {
		const { modules } = await api.get<{ modules: Module[] }>(`/api/teacher/classrooms/${id}/modules?size=100&sort=position`);
		const found = await Promise.all(
			modules.map(async (module) => ({ module, topics: (await api.get<{ topics: Topic[] }>(`/api/teacher/modules/${module.id}/topics?size=100&sort=position`)).topics }))
		);
		if (id !== classroomId) return; // another classroom was chosen meanwhile
		groups = found.filter((g) => g.topics.length);
		// A topic chosen under another classroom doesn't carry over.
		if (value && !groups.some((g) => g.topics.some((t) => t.id === value))) value = null;
	}
	$effect(() => {
		groups = [];
		if (classroomId) load(classroomId).catch(() => {}); // the rest of the form still works
	});
</script>

{#if groups.length}
	<select class="select w-full max-w-xs" class:select-sm={compact} aria-label="Topic" value={value ?? ''} onchange={(e) => (value = e.currentTarget.value || null)}>
		<option value="">No topic</option>
		{#each groups as g (g.module.id)}
			<optgroup label={g.module.name}>
				{#each g.topics as t (t.id)}<option value={t.id} selected={t.id === value}>{t.name}</option>{/each}
			</optgroup>
		{/each}
	</select>
{/if}
