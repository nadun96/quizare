<script lang="ts">
	// A classroom's Drive files on a student's page (D-71); nothing when there are none.
	import { api } from '../api';
	import type { ClassroomFile } from '../drive';
	import Icon from '../ui/Icon.svelte';

	let { classroomId }: { classroomId: string } = $props();
	let files = $state<ClassroomFile[]>([]);
	$effect(() => {
		api.get<{ files: ClassroomFile[] }>(`/api/my/classrooms/${classroomId}/drive-files`).then((r) => (files = r.files), () => {});
	});
</script>

{#if files.length}
	<ul class="files" aria-label="Files from your teacher">
		{#each files as f (f.id)}
			<li>
				{#if f.icon_link}<img src={f.icon_link} alt="" width="16" height="16" />{:else}<Icon name="cloud" size={16} />{/if}
				<a class="truncate" href={f.web_link} target="_blank" rel="noopener noreferrer">{f.name}</a>
			</li>
		{/each}
	</ul>
{/if}

<style>
	.files { list-style: none; margin: 0.4rem 0 0; padding: 0; display: grid; gap: 0.3rem; }
	.files li { display: flex; align-items: center; gap: 0.45rem; min-width: 0; font-size: 0.9rem; }
	.files img { flex: none; }
</style>
