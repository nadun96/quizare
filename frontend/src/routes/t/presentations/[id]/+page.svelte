<script lang="ts">
	// A presentation's page for its teacher (D-76): its slides and file, its
	// settings, and who saw which slides for how long.
	import { goto } from '$app/navigation';
	import { page as appPage } from '$app/state';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import { fmtBytes } from '$lib/staff';
	import { countPages, MAX_PDF_BYTES, slideAlt, uploadPdf, type Presentation, type Slide } from '$lib/presentation/presentation';
	import { duration, fromLocalInput, toLocalInput } from '$lib/tutorial/tutorial';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import IconBtn from '$lib/ui/IconBtn.svelte';
	import OptionGroup from '$lib/ui/OptionGroup.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { count } from '$lib/words';

	type Student = { user_id: string; viewer_id?: string; name: string; student_number?: string; started: boolean; seen: number; seconds: number; current: number; finished: boolean };
	type SlideViews = { n: number; text: string; viewers: number; median_seconds: number; total_seconds: number };
	type Report = { slides: SlideViews[]; students: Student[] };
	type Detail = { viewer_id: string; name: string; slides: { n: number; seconds: number; first_at?: string }[] };

	const ready = requireRole('teacher');
	const id = $derived(appPage.params.id ?? '');
	let p = $state<Presentation | null>(null);
	let slides = $state<Slide[]>([]);
	let tab = $state<'slides' | 'settings' | 'students'>('slides');
	let errors = $state<Record<string, string>>({});

	async function load() {
		p = await api.get<Presentation>('/api/teacher/presentations/' + id);
		slides = (await api.get<{ slides: Slide[] }>(`/api/teacher/presentations/${id}/slides`)).slides;
	}
	$effect(() => {
		if (ready() && id) load();
	});
	function fail(e: unknown, what: string) {
		if (e instanceof ApiError) {
			errors = e.fields;
			toast(Object.keys(e.fields).length ? Object.values(e.fields)[0] : e.message, 'error');
		} else toast(e instanceof Error ? e.message : what, 'error');
	}

	// ---- the file ----
	let uploading = $state<{ done: number; total: number } | null>(null);
	async function choose(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file || !p) return;
		if (file.size > MAX_PDF_BYTES) return toast('A PDF of at most 50 MB, please', 'error');
		try {
			if (p.slide_count) {
				const n = await countPages(file);
				const body = n === p.slide_count
					? 'Students see the new slides once the upload finishes. Their viewing records stay.'
					: `The new file has ${count(n, 'slide')}; this one has ${p.slide_count}. Viewing records stay with their slide numbers, so they may no longer match the same content.`;
				if (!(await confirmDialog({ title: `Replace “${p.file_name}”?`, body, confirm: 'Replace' }))) return;
			}
			uploading = { done: 0, total: 0 };
			p = await uploadPdf(id, file, (done, total) => (uploading = { done, total }));
			await load();
			toast(`${count(p.slide_count, 'slide')} ready`);
		} catch (err) {
			fail(err, 'Could not read that PDF');
		} finally {
			uploading = null;
		}
	}

	// ---- settings ----
	let form = $state<{ title: string; description: string; allow_jump: boolean; allow_download: boolean; opens: string; due: string } | null>(null);
	$effect(() => {
		if (p && !form) form = { title: p.title, description: p.description, allow_jump: p.allow_jump, allow_download: p.allow_download, opens: toLocalInput(p.opens_at), due: toLocalInput(p.due_at) };
	});
	async function saveSettings(e: SubmitEvent) {
		e.preventDefault();
		if (!form) return;
		errors = {};
		try {
			p = await api.put<Presentation>('/api/teacher/presentations/' + id, {
				title: form.title, description: form.description, allow_jump: form.allow_jump, allow_download: form.allow_download,
				clear_dates: true, opens_at: fromLocalInput(form.opens), due_at: fromLocalInput(form.due)
			});
			toast('Settings saved');
		} catch (err) {
			fail(err, 'Could not save');
		}
	}
	async function setStatus(status: 'draft' | 'published') {
		try {
			p = await api.post<Presentation>(`/api/teacher/presentations/${id}/status`, { status });
			toast(status === 'published' ? 'Published: the classroom’s students can open it' : 'Back to a draft: students can’t open it');
		} catch (e) {
			fail(e, 'Could not change it');
		}
	}
	async function remove() {
		if (!p) return;
		if (!(await confirmDialog({ title: 'Delete this presentation?', body: 'Its file, slides and the record of who saw them are deleted.', confirm: 'Delete', danger: true }))) return;
		await api.del('/api/teacher/presentations/' + id);
		goto('/t/topics/' + p.topic_id);
	}

	// ---- students ----
	let report = $state<Report | null>(null);
	let detail = $state<Detail | null>(null);
	$effect(() => {
		if (tab === 'students' && ready()) api.get<Report>(`/api/teacher/presentations/${id}/report`).then((r) => (report = r));
	});
	async function openDetail(s: Student) {
		if (s.viewer_id) detail = await api.get<Detail>(`/api/teacher/presentations/${id}/viewers/${s.viewer_id}`);
	}
	const progress = (s: Student) => (s.finished ? 'Seen to the end' : s.started ? `Slide ${s.current} of ${p?.slide_count}` : 'Not started');
	const thumb = (n: number) => `/api/teacher/presentations/${id}/slides/${n}?v=${p?.version}`;
</script>

<svelte:head><title>{p?.title ?? 'Presentation'}</title></svelte:head>

<div class="page-container vstack">
	{#if !p}
		<Skeleton lines={6} />
	{:else}
		<p class="small m-0"><a href={'/t/topics/' + p.topic_id}>← Back to the topic</a></p>
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="m-0">{p.title}</h1>
			<span class="badge badge-soft" class:ok={p.status === 'published'}>{p.status === 'published' ? 'Published' : 'Draft'}</span>
			<span class="spacer"></span>
			{#if p.status === 'published'}<button class="btn" onclick={() => setStatus('draft')}>Unpublish</button>
			{:else}<button class="btn btn-primary" disabled={!p.slide_count} onclick={() => setStatus('published')}><Icon name="check" size={16} />Publish to the class</button>{/if}
		</div>
		<div class="tabs tabs-border" role="tablist">
			{#each [['slides', p.slide_count ? `Slides (${p.slide_count})` : 'Slides'], ['settings', 'Settings'], ['students', 'Students']] as [k, l] (k)}
				<button class="tab" role="tab" aria-selected={tab === k} class:tab-active={tab === k} onclick={() => (tab = k as typeof tab)}>{l}</button>
			{/each}
		</div>

		{#if tab === 'slides'}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3">
				{#if uploading}
					<p class="m-0 font-semibold">{uploading.total ? `Preparing slide ${Math.min(uploading.done + 1, uploading.total)} of ${uploading.total}…` : 'Reading the PDF…'}</p>
					<progress class="progress progress-primary w-full" value={uploading.done} max={uploading.total || 1} aria-label="Upload progress"></progress>
					<p class="small muted m-0">Keep this page open until it finishes. Students keep the current slides until then.</p>
				{:else}
					<div class="flex flex-wrap items-center gap-3">
						{#if p.slide_count}
							<span class="flex-1 min-w-0"><Icon name="book" size={16} /> <strong>{p.file_name}</strong> <span class="small muted">· {count(p.slide_count, 'slide')} · {fmtBytes(p.file_bytes)}</span></span>
							<a class="btn btn-sm btn-ghost" href={`/api/teacher/presentations/${id}/pdf`} download><Icon name="download" size={14} />PDF</a>
						{:else}
							<span class="flex-1">Upload a PDF. Each page becomes a slide. From PowerPoint or Google Slides, save or download it as a PDF first.</span>
						{/if}
						<label class="btn btn-sm" class:btn-primary={!p.slide_count}><Icon name="upload" size={14} />{p.slide_count ? 'Replace' : 'Upload a PDF'}
							<input class="sr-only" type="file" accept=".pdf,application/pdf" onchange={choose} aria-label="Choose a PDF" /></label>
					</div>
					<p class="small muted m-0">At most 50 MB and 300 pages. Your browser turns the pages into pictures, so this takes a moment for big files.</p>
				{/if}
			</div>
			{#if slides.length}
				<ol class="thumbs" aria-label="Slides">
					{#each slides as s (s.n)}
						<li>
							<img src={thumb(s.n)} alt={slideAlt(s, slides.length)} width={s.width} height={s.height} loading="lazy" />
							<span class="small"><strong>{s.n}</strong>{#if s.text}<span class="muted">{' · '}{s.text.slice(0, 60)}</span>{/if}</span>
						</li>
					{/each}
				</ol>
			{/if}
		{:else if tab === 'settings' && form}
			<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={saveSettings}>
				<div><label for="pt">Title</label><input id="pt" class="input w-full" class:input-error={errors.title} bind:value={form.title} maxlength="200" required /></div>
				<div><label for="pd">Description <span class="muted">(shown above the slides)</span></label><textarea id="pd" class="textarea w-full" rows="2" bind:value={form.description}></textarea></div>
				<div class="vstack gap-1">
					<span class="font-semibold">Moving through it</span>
					<OptionGroup label="Moving through it" value={String(form.allow_jump)} onchange={(v) => form && (form.allow_jump = v === 'true')} options={[
						{ v: 'true', label: 'Any slide', icon: 'arrow-up-down', hint: 'Students can go to any slide, for example to look something up again.' },
						{ v: 'false', label: 'In order', icon: 'arrow-right', hint: 'Students see each slide before the next; they can always go back.' }
					]} />
				</div>
				<div class="vstack gap-1">
					<span class="font-semibold">Downloading the PDF</span>
					<OptionGroup label="Downloading the PDF" value={String(form.allow_download)} onchange={(v) => form && (form.allow_download = v === 'true')} options={[
						{ v: 'false', label: 'Not allowed', icon: 'eye', hint: 'Students see the slides here only.' },
						{ v: 'true', label: 'Allowed', icon: 'download', hint: 'Students can download the PDF, for example to print it.' }
					]} />
				</div>
				<div class="flex flex-wrap gap-4">
					<div><label for="po">Opens <span class="muted">(optional)</span></label><input id="po" class="input" type="datetime-local" bind:value={form.opens} /></div>
					<div><label for="pdue">See by <span class="muted">(optional)</span></label><input id="pdue" class="input" class:input-error={errors.due_at} type="datetime-local" bind:value={form.due} />{#if errors.due_at}<p class="field-error m-0">{errors.due_at}</p>{/if}</div>
				</div>
				<p class="small muted m-0">Students are told that you see which slides they view and for how long.</p>
				<div class="flex flex-wrap gap-2"><button class="btn btn-primary">Save settings</button><span class="spacer"></span><button type="button" class="btn btn-error btn-outline" onclick={remove}>Delete presentation</button></div>
			</form>
		{:else if tab === 'students'}
			{#if !report}<Skeleton lines={4} />
			{:else}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table">
						<thead><tr><th>Student</th><th>Progress</th><th class="text-right">Slides seen</th><th class="text-right">Time</th><th><span class="sr-only">Actions</span></th></tr></thead>
						<tbody>
							{#each report.students as s (s.user_id)}
								<tr>
									<td>{s.name}{#if s.student_number}<span class="small muted">{' · '}{s.student_number}</span>{/if}</td>
									<td><span class="badge badge-soft" class:ok={s.finished}>{progress(s)}</span></td>
									<td class="text-right tabular">{s.started ? `${s.seen} of ${p.slide_count}` : '—'}</td>
									<td class="text-right tabular">{s.started ? duration(s.seconds) : '—'}</td>
									<td>{#if s.viewer_id}<button class="btn btn-sm" onclick={() => openDetail(s)}>By slide</button>{/if}</td>
								</tr>
							{:else}<tr><td colspan="5" class="muted">No students in this classroom yet.</td></tr>{/each}
						</tbody>
					</table>
				</div>
				{#if detail}
					<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack">
						<div class="flex items-center gap-2"><h3 class="m-0">{detail.name}, slide by slide</h3><span class="spacer"></span><IconBtn icon="x" label="Close" hint="Close the slide times" onclick={() => (detail = null)} /></div>
						<div class="table-wrap">
							<table class="table table-sm">
								<thead><tr><th>Slide</th><th class="text-right">Time</th><th class="text-right">First seen</th></tr></thead>
								<tbody>
									{#each detail.slides as st (st.n)}
										<tr class:muted={!st.first_at}><td>{st.n}</td><td class="text-right tabular">{st.first_at ? duration(st.seconds) : '—'}</td>
											<td class="text-right small tabular">{st.first_at ? new Date(st.first_at).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : 'Not seen'}</td></tr>
									{/each}
								</tbody>
							</table>
						</div>
					</div>
				{/if}
				<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
					<table class="table table-sm">
						<caption class="text-left font-semibold pb-2">Each slide</caption>
						<thead><tr><th>Slide</th><th class="text-right">Students</th><th class="text-right">Median time</th></tr></thead>
						<tbody>
							{#each report.slides as sv (sv.n)}
								<tr><td>{sv.n}{#if sv.text}<span class="small muted">{' · '}{sv.text.slice(0, 60)}</span>{/if}</td><td class="text-right tabular">{sv.viewers}</td><td class="text-right tabular">{sv.viewers ? duration(sv.median_seconds) : '—'}</td></tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		{/if}
	{/if}
</div>

<style>
	.thumbs { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr)); gap: 0.75rem; }
	.thumbs li { display: grid; gap: 0.35rem; min-width: 0; }
	.thumbs img { width: 100%; height: auto; border-radius: 0.5rem; border: 1px solid var(--color-base-300); background: #fff; }
	.thumbs span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
