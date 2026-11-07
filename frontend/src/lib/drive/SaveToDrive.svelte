<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// "Save to Drive" for a session's or a poll's results (D-71), or a
	// presentation's viewing records (D-78). Shown only
	// to teachers who connected Drive; the last save is linked.
	import { api, ApiError } from '../api';
	import { driveStatus, type DriveExport } from '../drive';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';

	let { kind, id, size = '' }: { kind: 'session' | 'poll' | 'presentation'; id: string; size?: string } = $props();
	let connected = $state(false);
	let saving = $state(false);
	let last = $state<DriveExport | null>(null);

	$effect(() => {
		driveStatus().then((s) => (connected = s.connected && !s.reconnect));
		api.get<{ exports: DriveExport[] }>('/api/teacher/drive/exports?target_id=' + id).then(
			(r) => (last = r.exports.find((e) => e.status === 'done') ?? null),
			() => {}
		);
	});

	async function save() {
		saving = true;
		try {
			last = await api.post<DriveExport>(`/api/teacher/${kind}s/${id}/drive-export`);
			toast(tr("Saved to Google Drive"));
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("Could not save to Drive"), 'error');
		} finally {
			saving = false;
		}
	}
</script>

{#if connected}
	<span class="inline-flex flex-wrap items-center gap-2">
		<button class="btn {size}" disabled={saving} onclick={save}>{#if saving}<span class="loading loading-spinner loading-xs"></span>{:else}<Icon name="cloud" size={16} />{/if}{tr("Save to Drive")}</button>
		{#if last?.web_link}<a class="small" href={last.web_link} target="_blank" rel="noopener noreferrer">{tr("Open the last copy")}</a>{/if}
	</span>
{/if}
