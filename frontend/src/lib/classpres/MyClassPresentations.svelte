<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// A student's group presentations in their classroom (D-166): what each
	// task asks, their group's upload, Present once the teacher allows it, and
	// following (and rating) whatever is on the classroom's screen.
	import { api, ApiError } from '$lib/api';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import StageCard from './StageCard.svelte';
	import UploadBox from './UploadBox.svelte';
	import { mySlide, ratingText, type ClassStage, type GroupPres, type MyClassPresentations } from './classpres';

	let { classroomId }: { classroomId: string } = $props();
	let v = $state<MyClassPresentations | null>(null);
	let busy = $state(false);
	const stage = $derived(v?.stage ?? null);

	async function load() {
		try {
			v = await api.get<MyClassPresentations>(`/api/my/classrooms/${classroomId}/presentations`);
		} catch {
			v = null;
		}
	}
	$effect(() => {
		if (classroomId) load();
	});

	// Follow the screen: quickly while something's on, now and then otherwise.
	let tick = 0;
	$effect(() => {
		const timer = setInterval(async () => {
			if (document.hidden || !v) return;
			tick++;
			if (!v.stage && tick % 4 !== 0) return;
			try {
				const before = v.stage?.submission_id ?? '';
				const r = await api.get<{ stage: ClassStage | null }>(`/api/my/classrooms/${classroomId}/stage`);
				if (v) v.stage = r.stage;
				// Sooner while an upload waits for the teacher's permission.
				const waiting = !!v?.tasks.some((t) => t.my_groups.some((g) => g.presentation?.ready && !g.presentation.allowed));
				if ((r.stage?.submission_id ?? '') !== before || tick % 15 === 0 || (waiting && tick % 4 === 0)) await load();
			} catch {
				/* next time */
			}
		}, 1500);
		return () => clearInterval(timer);
	});

	async function run(f: () => Promise<unknown>, done?: string) {
		busy = true;
		try {
			await f();
			if (done) toast(done);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : tr("That didn’t work. Try again."), 'error');
		} finally {
			busy = false;
		}
	}
	const present = (p: GroupPres) => run(() => api.post(`/api/my/group-presentations/${p.id}/present`), tr("You’re presenting. Use Next and Back."));
	const turn = (n: number) => {
		if (v?.stage) v.stage.slide = n; // straight away; the server agrees or we reload
		run(() => api.put(`/api/my/classrooms/${classroomId}/stage/slide`, { n }));
	};
	const end = () => run(() => api.del(`/api/my/classrooms/${classroomId}/stage`), tr("You’ve stopped presenting"));
	const rate = (stars: number) => stage && run(() => api.put(`/api/my/group-presentations/${stage.submission_id}/rating`, { stars }), tr("Thanks for rating it"));
	const when = (iso: string) => new Date(iso).toLocaleString(localeTag(), { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
	const canPresent = (p: GroupPres | null) => !!p && p.allowed && p.ready && !p.files_removed && (!stage || stage.submission_id === p.id) && !stage?.presenting;
</script>

{#if v && (v.tasks.length || v.stage)}
	<section class="vstack gap-2" aria-labelledby="pres-h">
		<h2 id="pres-h" class="m-0 text-base flex items-center gap-2"><Icon name="monitor" size={16} />{tr("Group presentations")}</h2>
		{#if stage}
			<StageCard {stage} src={mySlide(stage.submission_id, stage.slide)} canTurn={stage.presenting} {busy} onturn={turn} onrate={rate}>
				{#snippet actions()}
					{#if stage.presenting}<button class="btn btn-sm btn-error btn-outline" disabled={busy} onclick={end}>{tr("End presenting")}</button>{/if}
				{/snippet}
			</StageCard>
		{/if}
		{#each v.tasks as t (t.id)}
			<div class="card card-border bg-base-100 p-4 vstack gap-2">
				<div class="flex flex-wrap items-center gap-2">
					<strong class="grow min-w-0">{t.title}</strong>
					{#if t.due_at}<span class="small muted">{tr("Due {0}", [when(t.due_at)])}</span>{/if}
				</div>
				{#if t.instructions}<p class="m-0 small whitespace-pre-line">{t.instructions}</p>{/if}
				{#if !t.my_groups.length}
					<p class="m-0 small muted">{tr("You’re not in a group yet, so there’s nothing to upload. Ask your teacher, or join one under Your groups.")}</p>
				{/if}
				{#each t.my_groups as g (g.id)}
					{@const p = g.presentation}
					<div class="rounded-box border border-base-300 p-3 vstack gap-2">
						{#if t.my_groups.length > 1 || p}<p class="m-0 small"><span class="muted">{tr("For {0}", [g.name])}</span></p>{/if}
						{#if p}
							<div class="small vstack gap-1">
								<p class="m-0"><strong>{p.title}</strong> · {p.slides} {p.slides === 1 ? tr("slide") : tr("slides")}{#if !p.ready && !p.files_removed} · <span class="text-warning">{tr("not finished uploading")}</span>{/if}</p>
								<p class="m-0 muted">{tr("Uploaded by {0}", [p.uploaded_by || '—'])}{#if p.presented_at} · {tr("presented")}{/if}{#if ratingText(p)} · {ratingText(p)}{/if}</p>
								{#if p.files_removed}<p class="m-0 muted">{tr("Your teacher has deleted the slides.")}</p>{/if}
								{#if p.mark != null}<p class="m-0">{tr("Mark: {0} / {1}", [p.mark, t.out_of])}{#if p.comment} · {p.comment}{/if}</p>{/if}
							</div>
							{#if p.ready && !p.files_removed}
								<div class="flex flex-wrap items-center gap-2">
									{#if stage?.submission_id === p.id && stage.presenting}
										<span class="badge badge-primary">{tr("You’re presenting")}</span>
									{:else if canPresent(p)}
										<button class="btn btn-primary btn-sm" disabled={busy} onclick={() => present(p)}><Icon name="play" size={14} />{tr("Present")}</button>
										<span class="small muted">{tr("Your teacher has allowed it. Your slides go on the classroom’s screen.")}</span>
									{:else if p.allowed && stage}
										<span class="small muted">{tr("Something else is on the screen; Present when it ends.")}</span>
									{:else}
										<span class="small muted">{tr("Waiting for your teacher to let your group present.")}</span>
									{/if}
								</div>
							{/if}
						{/if}
						{#if g.can_upload}
							<UploadBox {classroomId} taskId={t.id} groupId={g.id} replacing={!!p} canConvert={v.can_convert} disabled={stage?.submission_id === p?.id && !!p} onuploaded={load} />
						{:else if !p && t.upload_by === 'leader'}
							<p class="m-0 small muted">{tr("Your group’s leader uploads it.")}</p>
						{/if}
					</div>
				{/each}
			</div>
		{/each}
	</section>
{/if}
