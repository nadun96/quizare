<script lang="ts">
	// A classroom's Google Drive files (D-71): the teacher picks files and
	// chooses who can open them; students see them on their page.
	import { api, ApiError } from '../api';
	import { connectUrl, driveStatus, pickFiles, SHARING, type ClassroomFile, type DriveStatus } from '../drive';
	import Icon from '../ui/Icon.svelte';
	import OptionGroup from '../ui/OptionGroup.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';

	let { classroomId }: { classroomId: string } = $props();
	let st = $state<DriveStatus | null>(null);
	let files = $state<ClassroomFile[] | null>(null);
	let sharing = $state<'students' | 'link' | 'none'>('students');
	let busy = $state(false);

	async function load() {
		st = await driveStatus();
		files = (await api.get<{ files: ClassroomFile[] }>(`/api/teacher/classrooms/${classroomId}/drive-files`)).files;
	}
	$effect(() => {
		load();
	});

	function fail(e: unknown) {
		toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : e instanceof Error ? e.message : 'Something went wrong', 'error');
	}
	async function add() {
		busy = true;
		try {
			const picked = await pickFiles({ kind: 'any', multiple: true, title: 'Choose files for the class' });
			if (!picked.length) return;
			files = (await api.post<{ files: ClassroomFile[] }>(`/api/teacher/classrooms/${classroomId}/drive-files`, { file_ids: picked.map((p) => p.id), sharing })).files;
			toast(picked.length === 1 ? 'File added' : `${picked.length} files added`);
		} catch (e) {
			fail(e);
		} finally {
			busy = false;
		}
	}
	async function setSharing(f: ClassroomFile, v: string) {
		try {
			files = (await api.patch<{ files: ClassroomFile[] }>(`/api/teacher/classrooms/${classroomId}/drive-files/${f.id}`, { sharing: v })).files;
		} catch (e) {
			fail(e);
		}
	}
	async function remove(f: ClassroomFile) {
		if (!(await confirmDialog({ title: `Remove “${f.name}”?`, body: 'Students no longer see it here, and the sharing this platform added is taken back. The file stays in your Drive.', confirm: 'Remove' }))) return;
		try {
			await api.del(`/api/teacher/classrooms/${classroomId}/drive-files/${f.id}`);
			files = (files ?? []).filter((x) => x.id !== f.id);
		} catch (e) {
			fail(e);
		}
	}
	const sharedText = (f: ClassroomFile) =>
		f.sharing === 'students' ? `Shared with ${f.shared_with ?? 0} student${f.shared_with === 1 ? '' : 's'}` : f.sharing === 'link' ? 'Anyone with the link' : 'Sharing left to you, in Drive';
</script>

{#if !st || !files}
	<Skeleton lines={3} />
{:else if !st.available}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><p class="muted m-0">Google Drive isn’t set up on this platform. Ask your administrator.</p></div>
{:else}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
		<div class="flex flex-wrap items-center gap-3">
			<div class="min-w-0 flex-1">
				<h3 class="m-0">Files from Google Drive</h3>
				<p class="small muted m-0">Notes, worksheets and slides your students open from their page.</p>
			</div>
			{#if st.connected && !st.reconnect}
				<button class="btn btn-primary" disabled={busy} onclick={add}>{#if busy}<span class="loading loading-spinner loading-xs"></span>{:else}<Icon name="plus" size={16} />{/if}Add from Drive</button>
			{:else}
				<a class="btn btn-primary" href={connectUrl(location.pathname + '?tab=files')} data-sveltekit-reload><Icon name="link" size={16} />{st.reconnect ? 'Reconnect Google Drive' : 'Connect Google Drive'}</a>
			{/if}
		</div>
		{#if st.connected && !st.reconnect}
			<div class="vstack gap-1">
				<span class="small font-semibold">Share new files with</span>
				<OptionGroup label="Share new files with" size="sm" value={sharing} onchange={(v) => (sharing = v as typeof sharing)} options={[
					{ v: 'students', label: SHARING.students.label, icon: 'users', hint: SHARING.students.hint },
					{ v: 'link', label: SHARING.link.label, icon: 'link', hint: SHARING.link.hint },
					{ v: 'none', label: SHARING.none.label, icon: 'cloud', hint: SHARING.none.hint }
				]} />
			</div>
		{/if}
	</div>
	{#each files as f (f.id)}
		<div class="card card-border bg-base-100 shadow-sm p-4 vstack gap-2">
			<div class="flex flex-wrap items-center gap-3">
				{#if f.icon_link}<img src={f.icon_link} alt="" width="16" height="16" />{:else}<Icon name="cloud" size={16} />{/if}
				<a class="min-w-0 flex-1 truncate font-semibold" href={f.web_link} target="_blank" rel="noopener noreferrer">{f.name}</a>
				<span class="small muted">{sharedText(f)}</span>
				<button class="btn btn-sm btn-ghost" aria-label="Remove {f.name}" onclick={() => remove(f)}><Icon name="trash" size={14} /></button>
			</div>
			{#if st.connected && !st.reconnect}
				<OptionGroup label="Who can open {f.name}" size="sm" value={f.sharing} onchange={(v) => setSharing(f, v)} options={[
					{ v: 'students', label: 'Students', icon: 'users' },
					{ v: 'link', label: 'Anyone with the link', icon: 'link' },
					{ v: 'none', label: 'As in Drive', icon: 'cloud' }
				]} />
			{/if}
			{#if f.sync_error}<p class="small text-warning m-0 flex gap-1"><Icon name="alert" size={14} />{f.sync_error}</p>{/if}
		</div>
	{:else}
		<p class="small muted m-0">No files yet.</p>
	{/each}
{/if}
