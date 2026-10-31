<script lang="ts">
	// Email to groups of accounts (D-102), for admins and managers given
	// "Email": write one (or start from a template) with placeholders filled
	// in for each recipient, choose who gets it, preview, test, send; keep
	// templates; see everything sent.
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '../api';
	import { auth } from '../session.svelte';
	import { PLACEHOLDERS, insertAt, parseEmails, render, unknownPlaceholders } from '../mailing';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import { confirmDialog, promptDialog } from '../ui/dialog.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import SortHeader from '../ui/SortHeader.svelte';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { isAdmin } from '../staff';
	import EmailQueue from './EmailQueue.svelte';
	import MailServer from './MailServer.svelte';
	import { inFlight, progressText, type Progress } from './emailqueue';

	type Template = { id: string; name: string; subject: string; body: string; updated_at: string };
	type Sent = { id: string; template_id: string | null; subject: string; body: string; audience_label: string; recipients: number; sent_by: string; sent_at: string; progress: Progress };
	type Room = { id: string; name: string; teacher: string; students: number };
	type Preview = { recipients: number; label: string; unknown: string[]; sample: { to: string; subject: string; body: string } | null };
	type Kind = 'everyone' | 'students' | 'teachers' | 'staff' | 'classroom' | 'emails';

	const KINDS: { id: Kind; label: string }[] = [
		{ id: 'students', label: 'All students' },
		{ id: 'teachers', label: 'All teachers' },
		{ id: 'staff', label: 'Admins and managers' },
		{ id: 'everyone', label: 'Everyone' },
		{ id: 'classroom', label: "One classroom's students" },
		{ id: 'emails', label: 'A list of addresses' }
	];

	let view = $state<'write' | 'templates' | 'sent' | 'queue' | 'server'>('write');

	// Writing.
	let subject = $state('');
	let body = $state('');
	let templateId = $state('');
	let kind = $state<Kind>('students');
	let room = $state<Room | null>(null);
	let roomQ = $state('');
	let rooms = $state<Room[]>([]);
	let emailText = $state('');
	let preview = $state<Preview | null>(null);
	let busy = $state(false);
	let errors = $state<Record<string, string>>({});
	let bodyEl = $state<HTMLTextAreaElement>();
	let subjectEl = $state<HTMLInputElement>();
	let lastField = $state<'subject' | 'body'>('body');

	let choices = $state<Template[]>([]);
	async function loadChoices() {
		try {
			choices = (await api.get<{ templates: Template[] }>('/api/admin/email/templates?size=100&sort=name')).templates;
		} catch {
			choices = [];
		}
	}
	loadChoices();

	const audience = $derived({ kind, classroom_id: kind === 'classroom' ? room?.id : undefined, emails: kind === 'emails' ? parseEmails(emailText) : undefined });
	const ready = $derived(subject.trim() !== '' && body.trim() !== '' && (kind !== 'classroom' || !!room) && (kind !== 'emails' || parseEmails(emailText).length > 0));
	const unknown = $derived(unknownPlaceholders(subject + '\n' + body));
	const me = $derived({ name: auth.user?.name ?? 'Your name', email: auth.user?.email ?? '', role: auth.user?.role ?? 'admin', classroom: room?.name ?? 'Grade 8' });
	const site = typeof location === 'undefined' ? '' : location.origin;
	const sample = $derived(preview?.sample ?? { to: me.email, subject: render(subject, me, site), body: render(body, me, site) });
	// Any change to what's written or who gets it makes the server's preview stale.
	$effect(() => {
		void subject;
		void body;
		void JSON.stringify(audience);
		preview = null;
	});

	let roomTimer: ReturnType<typeof setTimeout>;
	function findRooms() {
		clearTimeout(roomTimer);
		roomTimer = setTimeout(async () => {
			try {
				rooms = (await api.get<{ classrooms: Room[] }>('/api/admin/email/classrooms?q=' + encodeURIComponent(roomQ))).classrooms;
			} catch {
				rooms = [];
			}
		}, 250);
	}

	function useTemplate(t: Template) {
		templateId = t.id;
		subject = t.subject;
		body = t.body;
		view = 'write';
		toast(`Started from "${t.name}"`, 'info');
	}
	function pickTemplate(id: string) {
		const t = choices.find((c) => c.id === id);
		if (t) useTemplate(t);
		else templateId = '';
	}
	function place(key: string) {
		const el = lastField === 'subject' ? subjectEl : bodyEl;
		if (!el) return;
		const value = lastField === 'subject' ? subject : body;
		const r = insertAt(value, el.selectionStart ?? value.length, el.selectionEnd ?? value.length, key);
		if (lastField === 'subject') subject = r.text;
		else body = r.text;
		requestAnimationFrame(() => {
			el.focus();
			el.setSelectionRange(r.caret, r.caret);
		});
	}

	function failed(err: unknown, what: string) {
		if (err instanceof ApiError && Object.keys(err.fields).length) {
			errors = err.fields;
			toast(Object.values(err.fields)[0], 'error');
		} else toast(err instanceof ApiError ? err.message : what, 'error');
	}
	const draft = () => ({ subject, body, audience, template_id: templateId || undefined });

	async function check() {
		busy = true;
		errors = {};
		try {
			preview = await api.post<Preview>('/api/admin/email/preview', draft());
		} catch (err) {
			failed(err, 'Could not preview it');
		} finally {
			busy = false;
		}
	}
	async function test() {
		busy = true;
		errors = {};
		try {
			await api.post('/api/admin/email/test', draft());
			toast(`Test sent to ${auth.user?.email}`);
		} catch (err) {
			failed(err, 'Could not send the test');
		} finally {
			busy = false;
		}
	}
	async function send() {
		if (!preview) await check();
		if (!preview) return;
		if (!preview.recipients) return void toast('Nobody to send to: no active account matches', 'error');
		const ok = await confirmDialog({
			title: `Send to ${preview.recipients} ${preview.recipients === 1 ? 'person' : 'people'}?`,
			body: `"${subject.trim()}" goes to ${preview.label.toLowerCase()}, each filled in for its reader.${preview.unknown.length ? ` ${preview.unknown.length} listed ${preview.unknown.length === 1 ? 'address is' : 'addresses are'} not an account and won't get it.` : ''} Email can't be called back.`,
			confirm: 'Send'
		});
		if (!ok) return;
		busy = true;
		try {
			const s = await api.post<Sent>('/api/admin/email/sends', draft());
			toast(`Sending to ${s.recipients} ${s.recipients === 1 ? 'person' : 'people'}`);
			subject = body = templateId = emailText = '';
			preview = null;
			sent.load();
		} catch (err) {
			failed(err, 'Could not send it');
		} finally {
			busy = false;
		}
	}
	async function saveTemplate() {
		const current = choices.find((c) => c.id === templateId);
		const name = await promptDialog({ title: current ? 'Save the template' : 'Save as a template', body: current ? `Change "${current.name}", or give a new name to keep both.` : 'Start from it next time.', label: 'Name', value: current?.name ?? '', maxlength: 120 });
		if (!name) return;
		errors = {};
		try {
			const same = current && current.name.trim().toLowerCase() === name.trim().toLowerCase();
			const t = same ? await api.put<Template>('/api/admin/email/templates/' + current!.id, { name, subject, body }) : await api.post<Template>('/api/admin/email/templates', { name, subject, body });
			templateId = t.id;
			toast(`Saved "${t.name}"`);
			await loadChoices();
			templates.load();
		} catch (err) {
			failed(err, 'Could not save it');
		}
	}

	// Templates and history.
	const templates = new Paged<Template>(() => '/api/admin/email/templates', 'templates', { url: urlState, prefix: 'et', sort: 'name' });
	const sent = new Paged<Sent>(() => '/api/admin/email/sends', 'sends', { url: urlState, prefix: 'es', sort: 'sent', desc: true });
	templates.load();
	sent.load();
	// Sent follows its emails while they're on their way (D-104).
	const following = setInterval(() => {
		if (view === 'sent' && document.visibilityState === 'visible' && sent.rows.some((s) => inFlight(s.progress))) sent.load();
	}, 10_000);
	onDestroy(() => clearInterval(following));
	let shown = $state<string | null>(null);

	async function removeTemplate(t: Template) {
		if (!(await confirmDialog({ title: `Delete "${t.name}"?`, body: 'Emails already sent with it stay in Sent.', confirm: 'Delete', danger: true }))) return;
		try {
			await api.del('/api/admin/email/templates/' + t.id);
			if (templateId === t.id) templateId = '';
			toast('Template deleted');
			templates.load();
			loadChoices();
		} catch (err) {
			failed(err, 'Could not delete it');
		}
	}
	function again(s: Sent) {
		subject = s.subject;
		body = s.body;
		templateId = s.template_id && choices.some((c) => c.id === s.template_id) ? s.template_id : '';
		view = 'write';
		toast('Copied into a new email: choose who gets it', 'info');
	}
	const date = (iso: string) => new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
</script>

<div class="vstack">
	<div role="tablist" class="tabs tabs-box w-fit" aria-label="Email">
		<button class="tab" role="tab" aria-selected={view === 'write'} class:tab-active={view === 'write'} onclick={() => (view = 'write')}>Write</button>
		<button class="tab" role="tab" aria-selected={view === 'templates'} class:tab-active={view === 'templates'} onclick={() => (view = 'templates')}>Templates</button>
		<button class="tab" role="tab" aria-selected={view === 'sent'} class:tab-active={view === 'sent'} onclick={() => (view = 'sent')}>Sent</button>
		<button class="tab" role="tab" aria-selected={view === 'queue'} class:tab-active={view === 'queue'} onclick={() => (view = 'queue')}>Queue</button>
		{#if isAdmin(auth.user)}<button class="tab" role="tab" aria-selected={view === 'server'} class:tab-active={view === 'server'} onclick={() => (view = 'server')}>Mail server</button>{/if}
	</div>

	{#if view === 'write'}
		<div class="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
			<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="em-h">
				<div class="flex flex-wrap items-end gap-2">
					<h2 id="em-h" class="m-0 grow text-lg">New email</h2>
					<label class="small" for="em-tpl">Start from
						<select id="em-tpl" class="select select-sm ml-1 w-56 max-w-full" value={templateId} onchange={(e) => pickTemplate(e.currentTarget.value)}>
							<option value="">A blank email</option>
							{#each choices as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
						</select>
					</label>
				</div>

				<fieldset class="vstack gap-2">
					<legend class="small font-semibold">Who gets it</legend>
					<div class="flex flex-wrap gap-2">
						{#each KINDS as k (k.id)}
							<label class="badge badge-lg cursor-pointer gap-1 {kind === k.id ? 'badge-primary' : 'badge-ghost'}">
								<input class="sr-only" type="radio" name="em-kind" value={k.id} bind:group={kind} />{k.label}
							</label>
						{/each}
					</div>
					{#if kind === 'classroom'}
						{#if room}
							<p class="m-0 small">
								<b>{room.name}</b> ({room.teacher}, {room.students} {room.students === 1 ? 'student' : 'students'})
								<button class="btn btn-xs btn-ghost" onclick={() => (room = null)}>Change</button>
							</p>
						{:else}
							<input class="input input-sm w-80 max-w-full" type="search" placeholder="Find a classroom or its teacher" aria-label="Find a classroom" bind:value={roomQ} oninput={findRooms} onfocus={findRooms} />
							{#if rooms.length}
								<ul class="menu rounded-box border border-base-300 bg-base-100 p-1 w-96 max-w-full">
									{#each rooms as r (r.id)}<li><button onclick={() => (room = r)}>{r.name}<span class="small muted">{r.teacher} · {r.students}</span></button></li>{/each}
								</ul>
							{/if}
						{/if}
					{:else if kind === 'emails'}
						<textarea class="textarea textarea-sm w-full" rows="3" placeholder="Paste addresses: one a line, or separated by commas" aria-label="Addresses" bind:value={emailText}></textarea>
						<p class="m-0 small muted">{parseEmails(emailText).length} addresses. Only those with an account here get it.</p>
					{/if}
				</fieldset>

				<div>
					<label for="em-subject" class="small font-semibold">Subject</label>
					<input id="em-subject" class="input w-full" class:input-error={errors.subject} maxlength="200" bind:value={subject} bind:this={subjectEl} onfocus={() => (lastField = 'subject')} />
					{#if errors.subject}<p class="m-0 small text-error">{errors.subject}</p>{/if}
				</div>
				<div>
					<label for="em-body" class="small font-semibold">Message</label>
					<textarea id="em-body" class="textarea w-full font-mono text-sm" class:textarea-error={errors.body} rows="10" maxlength="20000" bind:value={body} bind:this={bodyEl} onfocus={() => (lastField = 'body')}></textarea>
					{#if errors.body}<p class="m-0 small text-error">{errors.body}</p>{/if}
				</div>
				<div class="flex flex-wrap items-center gap-1" aria-label="Placeholders">
					<span class="small muted mr-1">Insert:</span>
					{#each PLACEHOLDERS as p (p.key)}
						<button class="btn btn-xs btn-soft" title={p.hint} onclick={() => place(p.key)} disabled={p.key === 'classroom' && kind !== 'classroom'}>{p.label}</button>
					{/each}
				</div>
				{#if unknown.length}
					<p class="alert alert-warning m-0 small" role="status"><Icon name="alert" size={16} />Not a placeholder: {unknown.map((u) => `{{${u}}}`).join(', ')}. The server refuses it.</p>
				{/if}

				<div class="flex flex-wrap gap-2">
					<button class="btn btn-primary" onclick={send} disabled={!ready || busy || unknown.length > 0}><Icon name="send" size={16} />{preview ? `Send to ${preview.recipients}` : 'Send'}</button>
					<button class="btn" onclick={check} disabled={!ready || busy}>Check who gets it</button>
					<button class="btn btn-ghost" onclick={test} disabled={!subject.trim() || !body.trim() || busy}>Send me a test</button>
					<span class="spacer"></span>
					<button class="btn btn-ghost" onclick={saveTemplate} disabled={!subject.trim() || !body.trim()}>{templateId ? 'Save the template' : 'Save as a template'}</button>
				</div>
			</section>

			<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="em-p">
				<h2 id="em-p" class="m-0 text-lg">How it reads</h2>
				{#if preview}
					<p class="m-0" role="status"><b>{preview.recipients}</b> {preview.recipients === 1 ? 'person gets' : 'people get'} it: {preview.label.toLowerCase()}.</p>
					{#if preview.unknown.length}
						<p class="m-0 small text-warning">Not an account here, so left out: {preview.unknown.join(', ')}</p>
					{/if}
				{:else}
					<p class="m-0 small muted">Filled in for you below. "Check who gets it" counts the recipients and shows the first one's copy.</p>
				{/if}
				<div class="mail-sample rounded-box border border-base-300 p-3">
					<p class="m-0 small muted">To: {sample.to}</p>
					<p class="m-0 font-semibold">{sample.subject || 'No subject yet'}</p>
					<pre class="m-0 mt-2 whitespace-pre-wrap font-sans text-sm">{sample.body || 'Nothing written yet.'}</pre>
				</div>
			</section>
		</div>
	{:else if view === 'queue'}
		<EmailQueue />
	{:else if view === 'server'}
		<MailServer />
	{:else if view === 'templates'}
		<ListSearch list={templates} placeholder="Search templates" label="Search templates" />
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader list={templates} key="name" label="Template" /></th><th>Subject</th><th><SortHeader list={templates} key="updated" label="Changed" desc /></th><th><span class="sr-only">Actions</span></th></tr></thead>
				<tbody>
					{#each templates.rows as t (t.id)}
						<tr>
							<td class="font-semibold">{t.name}</td>
							<td>{t.subject}</td>
							<td class="small tabular">{date(t.updated_at)}</td>
							<td class="text-right whitespace-nowrap">
								<button class="btn btn-xs btn-primary" onclick={() => useTemplate(t)}>Use</button>
								<button class="btn btn-xs" onclick={() => removeTemplate(t)}>Delete</button>
							</td>
						</tr>
					{:else}
						<tr><td colspan="4" class="muted">{templates.loaded ? (templates.q ? `No templates match “${templates.q}”.` : 'No templates yet: write an email and save it as one.') : 'Loading…'}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pager list={templates} label="Templates" />
	{:else}
		<ListSearch list={sent} placeholder="Search subject, audience or sender" label="Search sent email" />
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
			<table class="table">
				<thead><tr><th><SortHeader list={sent} key="subject" label="Subject" /></th><th>To</th><th><SortHeader list={sent} key="recipients" label="People" /></th><th>By</th><th><SortHeader list={sent} key="sent" label="Sent" desc /></th></tr></thead>
				<tbody>
					{#each sent.rows as s (s.id)}
						<tr>
							<td><button class="link link-hover text-left font-semibold" aria-expanded={shown === s.id} onclick={() => (shown = shown === s.id ? null : s.id)}>{s.subject}</button></td>
							<td>{s.audience_label}</td>
							<td class="tabular">{s.recipients}{#if progressText(s.progress)}<br /><span class="small {s.progress.failed ? 'text-error' : 'muted'}">{progressText(s.progress)}</span>{/if}</td>
							<td>{s.sent_by}</td>
							<td class="small tabular">{date(s.sent_at)}</td>
						</tr>
						{#if shown === s.id}
							<tr><td colspan="5"><pre class="m-0 whitespace-pre-wrap font-sans text-sm">{s.body}</pre><button class="btn btn-xs mt-2" onclick={() => again(s)}>Write it again</button>
								<div class="mt-3"><EmailQueue send={s.id} label="Its emails" /></div></td></tr>
						{/if}
					{:else}
						<tr><td colspan="5" class="muted">{sent.loaded ? (sent.q ? `Nothing sent matches “${sent.q}”.` : 'Nothing sent yet.') : 'Loading…'}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<Pager list={sent} label="Sent email" />
	{/if}
</div>

<style>
	.mail-sample { background: var(--color-base-200); }
</style>
