<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// A poll's presentations, for its teacher (D-165): asking for them, the
	// submissions, letting one present (and taking over), marks and ratings.
	import { api, ApiError } from '$lib/api';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { MODES, ratingText, step, type PresSettings, type Stage, type Submission } from './present';

	let { pollId, hasClassroom, version = 0 }: { pollId: string; hasClassroom: boolean; version?: number } = $props();
	type View = { settings: PresSettings; submissions: Submission[]; stage: Stage | null; presenter_id: string | null };
	let v = $state<View | null>(null);
	let draft = $state<PresSettings>({ mode: 'off', upload_by: 'any', out_of: 0, rating: false, instructions: '' });
	let marks = $state<Record<string, { mark: string; comment: string }>>({});
	let busy = $state(false);
	let preview = $state<{ id: string; n: number; slides: number; title: string } | null>(null);
	const base = $derived(`/api/teacher/polls/${pollId}`);

	async function load(fresh = false) {
		const r = await api.get<View>(`${base}/presentations`);
		v = r;
		if (fresh) draft = { ...r.settings };
		for (const x of r.submissions) if (!marks[x.id] || fresh) marks[x.id] = { mark: x.mark == null ? '' : String(x.mark), comment: x.comment };
	}
	$effect(() => {
		if (pollId) load(true);
	});
	// Live: reload when the poll changes (ratings, new submissions).
	$effect(() => {
		void version;
		if (v) load();
	});

	async function run(f: () => Promise<unknown>, done?: string) {
		busy = true;
		try {
			await f();
			if (done) toast(done);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields ?? {})[0] ?? e.message) : tr("That didn’t work. Try again."), 'error');
		} finally {
			busy = false;
		}
	}
	const saveSettings = (e: SubmitEvent) => {
		e.preventDefault();
		run(async () => (draft = await api.put<PresSettings>(`${base}/presentations`, { ...draft, out_of: Number(draft.out_of) || 0 })), tr("Saved"));
	};
	const present = (x: Submission, presenter = '') => run(() => api.put(`${base}/stage`, { submission_id: x.id, presenter }), tr("{0} is on the screen", [x.owner.name || x.title]));
	const stop = () => run(() => api.del(`${base}/stage`), tr("Off the screen"));
	const turn = (n: number | null) => n != null && run(() => api.put(`${base}/stage/slide`, { n }));
	function saveMark(x: Submission) {
		const m = marks[x.id];
		const text = m.mark.trim().replace(',', '.');
		run(() => api.put(`/api/teacher/poll-submissions/${x.id}/mark`, { mark: text === '' ? null : Number(text), comment: m.comment }), tr("Mark saved"));
	}
	async function remove(x: Submission) {
		if (await confirmDialog({ title: tr("Delete “{0}”?", [x.title]), body: tr("Its slides, mark and ratings go too. They can upload again."), confirm: tr("Delete"), danger: true }))
			run(() => api.del(`/api/teacher/poll-submissions/${x.id}`), tr("Deleted"));
	}
	const slideSrc = (id: string, n: number) => `/api/teacher/poll-submissions/${id}/slides/${n}`;
	const onStage = (x: Submission) => v?.stage?.submission_id === x.id;
</script>

<div class="vstack">
	<form class="card card-border bg-base-100 p-4 vstack gap-3" onsubmit={saveSettings} aria-label={tr("Ask for presentations")}>
		<h2 class="m-0 text-base">{tr("Presentations")}</h2>
		<p class="m-0 small muted">{tr("Students upload slides (PDF or PowerPoint) here in the poll. You choose who presents and when; they turn the slides on their own device, and everyone in the poll and on your Present screen follows.")}</p>
		<div class="flex flex-wrap items-end gap-3">
			<label class="vstack gap-1 small">{tr("Ask for")}
				<select class="select select-sm w-auto" bind:value={draft.mode}>
					{#each MODES.filter((m) => m.v !== 'class_group' || hasClassroom) as m (m.v)}<option value={m.v}>{tr(m.label)}</option>{/each}
				</select>
			</label>
			{#if draft.mode === 'poll_group' || draft.mode === 'class_group'}
				<label class="vstack gap-1 small">{tr("Uploaded by")}
					<select class="select select-sm w-auto" bind:value={draft.upload_by}><option value="any">{tr("Any member")}</option><option value="leader">{tr("The leader (captain)")}</option></select>
				</label>
			{/if}
			<label class="vstack gap-1 small">{tr("Marked out of")}<input class="input input-sm w-24 tabular" type="number" min="0" max="10000" step="any" bind:value={draft.out_of} placeholder={tr("Not marked")} /></label>
			<label class="flex items-center gap-2 small pb-1"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={draft.rating} />{tr("The audience rates each (1–5)")}</label>
		</div>
		{#if draft.mode !== 'off'}<label class="vstack gap-1 small">{tr("Instructions")}<textarea class="textarea textarea-sm w-full" rows="2" maxlength="2000" bind:value={draft.instructions} placeholder={tr("e.g. 5 slides, 5 minutes, about one cause of the war")}></textarea></label>{/if}
		<div><button class="btn btn-primary btn-sm" disabled={busy}>{tr("Save")}</button></div>
	</form>

	{#if v?.stage}
		{@const st = v.stage}
		<section class="card card-border border-primary bg-base-100 p-4 vstack gap-2" aria-label={tr("On the screen")}>
			<div class="flex flex-wrap items-center gap-2">
				<span class="badge badge-primary">{tr("On the screen")}</span>
				<strong class="grow min-w-0 truncate">{st.title} · {st.owner}</strong>
				<span class="small muted">{st.presenter_name ? tr("{0} controls it", [st.presenter_name]) : tr("You control it")}</span>
			</div>
			<img class="slide" src={slideSrc(st.submission_id, st.slide)} alt={tr("Slide {0} of {1}", [st.slide, st.slides])} />
			<div class="flex flex-wrap items-center gap-2">
				<button class="btn btn-sm" disabled={busy || step(st, -1) == null} onclick={() => turn(step(st, -1))}><Icon name="chevron-left" size={16} />{tr("Back")}</button>
				<span class="tabular small">{st.slide} / {st.slides}</span>
				<button class="btn btn-sm" disabled={busy || step(st, 1) == null} onclick={() => turn(step(st, 1))}>{tr("Next")}<Icon name="chevron-right" size={16} /></button>
				<span class="grow"></span>
				{#if st.presenter_name}
					{@const x = v.submissions.find((s) => s.id === st.submission_id)}
					{#if x}<button class="btn btn-sm" disabled={busy} onclick={() => present(x, 'teacher')}>{tr("Take over")}</button>{/if}
				{/if}
				<button class="btn btn-sm btn-error btn-outline" disabled={busy} onclick={stop}>{tr("Stop presenting")}</button>
			</div>
		</section>
	{/if}

	{#if v && v.settings.mode !== 'off'}
		{#if !v.submissions.length}
			<p class="m-0 small muted">{tr("Nothing uploaded yet. Participants see an upload box on their poll page.")}</p>
		{:else}
			<div class="card card-border bg-base-100 overflow-x-auto">
				<table class="table table-sm">
					<thead><tr><th>{tr("From")}</th><th>{tr("Presentation")}</th><th>{tr("Rating")}</th>{#if v.settings.out_of}<th>{tr("Mark (out of {0})", [v.settings.out_of])}</th>{/if}<th><span class="sr-only">{tr("Actions")}</span></th></tr></thead>
					<tbody>
						{#each v.submissions as x (x.id)}
							<tr class:bg-primary-50={onStage(x)}>
								<td class="whitespace-nowrap"><strong>{x.owner.name || '—'}</strong><br /><span class="text-xs muted">{tr("by {0}", [x.uploaded_by || '—'])}</span></td>
								<td class="min-w-48">
									<button class="link text-left" onclick={() => (preview = { id: x.id, n: 1, slides: x.slides, title: x.title })}>{x.title}</button><br />
									<span class="text-xs muted">{x.slides} {x.slides === 1 ? tr("slide") : tr("slides")}{#if !x.ready} · <span class="text-warning">{tr("uploading")}</span>{/if}{#if x.presented_at} · {tr("presented")}{/if}</span>
								</td>
								<td class="small whitespace-nowrap">{ratingText(x) || '—'}</td>
								{#if v.settings.out_of}
									<td class="whitespace-nowrap">
										<input class="input input-xs w-16 tabular" inputmode="decimal" bind:value={marks[x.id].mark} aria-label={tr("Mark for {0}", [x.title])} />
										<input class="input input-xs w-36" bind:value={marks[x.id].comment} maxlength="1000" placeholder={tr("Comment")} aria-label={tr("Comment for {0}", [x.title])} />
										<button class="btn btn-xs" disabled={busy} onclick={() => saveMark(x)}>{tr("Save")}</button>
									</td>
								{/if}
								<td class="whitespace-nowrap text-right">
									{#if onStage(x)}<span class="badge badge-primary badge-sm">{tr("on screen")}</span>
									{:else}
										<button class="btn btn-xs btn-primary" disabled={busy || !x.ready} onclick={() => present(x)} title={tr("They control the slides on their device")}>{tr("Let present")}</button>
										<button class="btn btn-xs" disabled={busy || !x.ready} onclick={() => present(x, 'teacher')}>{tr("Show it myself")}</button>
									{/if}
									<button class="btn btn-ghost btn-xs" aria-label={tr("Delete {0}", [x.title])} disabled={busy} onclick={() => remove(x)}><Icon name="trash" size={13} /></button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}

	{#if preview}
		{@const pv = preview}
		<section class="card card-border bg-base-100 p-3 vstack gap-2" aria-label={tr("Preview")}>
			<div class="flex items-center gap-2"><strong class="grow truncate">{pv.title}</strong><span class="tabular small">{pv.n} / {pv.slides}</span><button class="btn btn-ghost btn-xs" onclick={() => (preview = null)} aria-label={tr("Close the preview")}><Icon name="x" size={14} /></button></div>
			<img class="slide" src={slideSrc(pv.id, pv.n)} alt={tr("Slide {0} of {1}", [pv.n, pv.slides])} />
			<div class="flex gap-2">
				<button class="btn btn-sm" disabled={pv.n <= 1} onclick={() => preview && (preview.n -= 1)}><Icon name="chevron-left" size={16} /></button>
				<button class="btn btn-sm" disabled={pv.n >= pv.slides} onclick={() => preview && (preview.n += 1)}><Icon name="chevron-right" size={16} /></button>
			</div>
		</section>
	{/if}
</div>

<style>
	.slide { width: 100%; max-height: 60vh; object-fit: contain; background: #fff; border-radius: 0.5rem; border: 1px solid var(--color-base-300); }
	.bg-primary-50 { background: color-mix(in oklab, var(--color-primary) 8%, transparent); }
</style>
