<script lang="ts">
	// Messages (D-103) between a teacher and a student of theirs: the
	// conversations, latest first, beside the open one. Either side starts
	// one from the people they can write to. An open conversation checks for
	// new messages every 15 seconds while the page is visible.
	import { onDestroy, untrack } from 'svelte';
	import { api, ApiError } from '../api';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import Avatar from '../ui/Avatar.svelte';
	import Icon from '../ui/Icon.svelte';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import { toast } from '../ui/toast.svelte';
	import { inbox, when, type Conversation, type Message, type Person } from './inbox.svelte';
	import { t } from '../i18n/i18n.svelte';

	let { teacher, initial = '', onopen }: { teacher: boolean; initial?: string; onopen?: (id: string) => void } = $props();

	const PAGE = 30;
	const list = new Paged<Conversation>(() => '/api/my/conversations', 'conversations', { url: urlState, prefix: 'cv', sort: 'last', desc: true });
	list.load();

	let current = $state<Conversation | null>(null);
	let to = $state<Person | null>(null); // writing to someone with no conversation open yet
	let messages = $state<Message[]>([]);
	let total = $state(0);
	let draft = $state('');
	let sending = $state(false);
	let thread = $state<HTMLElement>();

	function scrollDown() {
		requestAnimationFrame(() => thread && (thread.scrollTop = thread.scrollHeight));
	}
	async function fetchPage(id: string, page: number) {
		return api.get<{ messages: Message[]; total: number }>(`/api/my/conversations/${id}/messages?page=${page}&size=${PAGE}`);
	}
	async function read(id: string) {
		try {
			await api.post(`/api/my/conversations/${id}/read`);
			inbox.load(true);
			list.rows.find((c) => c.id === id) && list.load();
		} catch {
			// Next time.
		}
	}
	async function openConversation(id: string) {
		to = null;
		picking = false;
		try {
			current = await api.get<Conversation>('/api/my/conversations/' + id);
			const r = await fetchPage(id, 1);
			messages = r.messages.reverse();
			total = r.total;
			onopen?.(id);
			scrollDown();
			if (current.unread) read(id);
		} catch {
			toast('That conversation is not yours, or no longer there', 'error');
			current = null;
		}
	}
	if (untrack(() => initial)) openConversation(untrack(() => initial));

	async function earlier() {
		if (!current) return;
		const before = thread?.scrollHeight ?? 0;
		const r = await fetchPage(current.id, Math.floor(messages.length / PAGE) + 1);
		const seen = new Set(messages.map((m) => m.id));
		messages = [...r.messages.reverse().filter((m) => !seen.has(m.id)), ...messages];
		total = r.total;
		requestAnimationFrame(() => thread && (thread.scrollTop = thread.scrollHeight - before));
	}

	// New messages in the open conversation.
	const poll = setInterval(async () => {
		if (!current || document.visibilityState !== 'visible') return;
		try {
			const r = await fetchPage(current.id, 1);
			const seen = new Set(messages.map((m) => m.id));
			const fresh = r.messages.filter((m) => !seen.has(m.id)).reverse();
			if (!fresh.length) return;
			messages = [...messages, ...fresh];
			total = r.total;
			scrollDown();
			if (fresh.some((m) => !m.mine)) read(current.id);
		} catch {
			// The next tick tries again.
		}
	}, 15_000);
	onDestroy(() => clearInterval(poll));

	async function send(e?: SubmitEvent) {
		e?.preventDefault();
		const body = draft.trim();
		if (!body || sending) return;
		sending = true;
		try {
			if (current) {
				const m = await api.post<Message>(`/api/my/conversations/${current.id}/messages`, { body });
				messages = [...messages, m];
				total++;
			} else if (to) {
				const r = await api.post<{ conversation: Conversation; message: Message }>('/api/my/conversations', { user_id: to.id, body });
				await openConversation(r.conversation.id);
			}
			draft = '';
			scrollDown();
			list.load();
		} catch (err) {
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not send it', 'error');
		} finally {
			sending = false;
		}
	}
	function keys(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
			e.preventDefault();
			send();
		}
	}

	// Starting a conversation.
	let picking = $state(false);
	let contacts = $state<Paged<Person> | null>(null);
	function newMessage() {
		picking = true;
		current = null;
		to = null;
		const c = new Paged<Person>(() => '/api/my/contacts', 'contacts', { sort: 'name' });
		c.load();
		contacts = c;
	}
	async function choose(p: Person) {
		// An existing conversation with them opens instead.
		const r = await api.get<{ conversations: Conversation[] }>('/api/my/conversations?size=100&q=' + encodeURIComponent(p.name)).catch(() => ({ conversations: [] }));
		const found = r.conversations.find((c) => c.with.id === p.id);
		if (found) return openConversation(found.id);
		picking = false;
		to = p;
		messages = [];
		total = 0;
		requestAnimationFrame(() => document.getElementById('msg-draft')?.focus());
	}
	const other = $derived(current?.with ?? to);
	const sameDay = (a: string, b: string) => new Date(a).toDateString() === new Date(b).toDateString();
</script>

<div class="msgs" class:open={!!(current || to || picking)}>
	<aside class="msgs-list card card-border bg-base-100 shadow-sm" aria-label="Conversations">
		<div class="flex items-center gap-2 p-3 pb-0">
			<button class="btn btn-primary btn-sm" onclick={newMessage}><Icon name="plus" size={16} />{t('inbox.newMessage')}</button>
		</div>
		<div class="p-3"><ListSearch {list} placeholder={t('inbox.searchByName')} label={t('inbox.searchByName')} /></div>
		<ul class="conv">
			{#each list.rows as c (c.id)}
				<li>
					<button class="conv-row" class:on={current?.id === c.id} onclick={() => openConversation(c.id)}>
						<Avatar id={c.with.id} name={c.with.name} avatar={c.with.avatar} size={36} />
						<span class="grow min-w-0 text-left">
							<span class="flex items-baseline gap-2"><span class="grow truncate" class:font-bold={c.unread > 0}>{c.with.name}</span><span class="small muted shrink-0">{when(c.last_at)}</span></span>
							<span class="flex items-center gap-2"><span class="small muted grow truncate">{c.last_mine ? t('inbox.you') : ''}{c.last}</span>{#if c.unread}<span class="badge badge-primary badge-sm" aria-label="{c.unread} unread">{c.unread}</span>{/if}</span>
						</span>
					</button>
				</li>
			{:else}
				<li class="p-4 small muted">{list.loaded ? (list.q ? `Nobody matches “${list.q}”.` : teacher ? 'No messages yet. Write to a student of your classrooms with New message.' : 'No messages yet. You can write to your teachers with New message.') : 'Loading…'}</li>
			{/each}
		</ul>
		<div class="px-3"><Pager {list} label="Conversations" /></div>
	</aside>

	<section class="msgs-thread card card-border bg-base-100 shadow-sm" aria-label={other ? `Messages with ${other.name}` : 'Messages'}>
		{#if picking && contacts}
			<div class="p-4 vstack">
				<div class="flex items-center gap-2">
					<button class="btn btn-ghost btn-sm btn-circle back" aria-label="Back to conversations" onclick={() => (picking = false)}><Icon name="x" size={18} /></button>
					<h2 class="m-0 text-lg">Write to {teacher ? 'a student' : 'a teacher'}</h2>
				</div>
				<ListSearch list={contacts} placeholder={teacher ? 'Search name, email or student ID' : 'Search your teachers'} label="Search people" />
				<ul class="conv">
					{#each contacts.rows as p (p.id)}
						<li><button class="conv-row" onclick={() => choose(p)}><Avatar id={p.id} name={p.name} avatar={p.avatar} size={32} /><span class="grow min-w-0 text-left"><span class="block truncate">{p.name}</span><span class="block small muted truncate">{p.classrooms}</span></span></button></li>
					{:else}
						<li class="p-3 small muted">{contacts.loaded ? (contacts.q ? `Nobody matches “${contacts.q}”.` : teacher ? 'No students in your classrooms yet.' : "You aren't in a classroom yet.") : 'Loading…'}</li>
					{/each}
				</ul>
				<Pager list={contacts} label="People" />
			</div>
		{:else if other}
			<header class="thread-head">
				<button class="btn btn-ghost btn-sm btn-circle back" aria-label="Back to conversations" onclick={() => ((current = null), (to = null))}><Icon name="x" size={18} /></button>
				<Avatar id={other.id} name={other.name} avatar={other.avatar} size={36} />
				<div class="min-w-0"><h2 class="m-0 text-base truncate">{other.name}</h2>{#if other.classrooms}<p class="m-0 small muted truncate">{other.classrooms}</p>{/if}</div>
			</header>
			<div class="thread" bind:this={thread} aria-live="polite">
				{#if messages.length < total}<div class="text-center"><button class="btn btn-xs btn-ghost" onclick={earlier}>Earlier messages</button></div>{/if}
				{#each messages as m, i (m.id)}
					{#if i === 0 || !sameDay(messages[i - 1].created_at, m.created_at)}
						<p class="day small muted">{new Date(m.created_at).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}</p>
					{/if}
					<div class="bubble-row" class:mine={m.mine}>
						<div class="bubble" class:mine={m.mine}>
							<span class="sr-only">{m.mine ? 'You' : m.sender}:</span>
							<p class="m-0 whitespace-pre-wrap">{m.body}</p>
							<time class="stamp" datetime={m.created_at}>{new Date(m.created_at).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}</time>
						</div>
					</div>
				{:else}
					<p class="m-auto small muted text-center">Say hello to {other.name}. They get an email too, unless they turned those off.</p>
				{/each}
			</div>
			{#if current && !current.can_reply}
				<p class="m-0 p-3 small muted border-t border-base-300">{teacher ? 'They are no longer in any of your classrooms' : 'You are no longer in any of their classrooms'}, so no more messages can be sent here.</p>
			{:else}
				<form class="composer" onsubmit={send}>
					<label for="msg-draft" class="sr-only">Message to {other.name}</label>
					<textarea id="msg-draft" class="textarea grow" rows="2" maxlength="5000" placeholder={t('inbox.writePlaceholder')} bind:value={draft} onkeydown={keys}></textarea>
					<button class="btn btn-primary btn-circle" aria-label={t('inbox.send')} disabled={!draft.trim() || sending}><Icon name="send" size={18} /></button>
				</form>
			{/if}
		{:else}
			<div class="m-auto p-8 text-center muted vstack items-center">
				<Icon name="send" size={32} />
				<p class="m-0">{t('inbox.choose')}</p>
			</div>
		{/if}
	</section>
</div>

<style>
	.msgs { display: grid; gap: 1rem; grid-template-columns: minmax(16rem, 22rem) minmax(0, 1fr); min-height: min(70dvh, 44rem); }
	.msgs-list, .msgs-thread { display: flex; flex-direction: column; min-height: 0; }
	.msgs-thread { max-height: min(78dvh, 48rem); }
	.conv { list-style: none; margin: 0; padding: 0 0.25rem; overflow-y: auto; flex: 1; }
	.conv-row { display: flex; width: 100%; align-items: center; gap: 0.75rem; padding: 0.6rem 0.6rem; border-radius: var(--radius-field); }
	.conv-row:hover { background: var(--color-base-200); }
	.conv-row.on { background: color-mix(in oklab, var(--color-primary) 14%, transparent); }
	.thread-head { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 1rem; border-bottom: 1px solid var(--color-base-300); }
	.thread { flex: 1; overflow-y: auto; padding: 1rem; display: flex; flex-direction: column; gap: 0.35rem; }
	.day { text-align: center; margin: 0.75rem 0 0.25rem; }
	.bubble-row { display: flex; }
	.bubble-row.mine { justify-content: flex-end; }
	.bubble { max-width: min(80%, 34rem); padding: 0.5rem 0.75rem; border-radius: 1rem 1rem 1rem 0.3rem; background: var(--color-base-200); }
	.bubble.mine { border-radius: 1rem 1rem 0.3rem 1rem; background: var(--color-primary); color: var(--color-primary-content); }
	.stamp { display: block; text-align: right; font-size: 0.7rem; opacity: 0.7; margin-top: 0.15rem; }
	.composer { display: flex; align-items: flex-end; gap: 0.5rem; padding: 0.75rem; border-top: 1px solid var(--color-base-300); }
	.back { display: none; }
	@media (max-width: 767px) {
		.msgs { grid-template-columns: minmax(0, 1fr); }
		.msgs.open .msgs-list { display: none; }
		.msgs:not(.open) .msgs-thread { display: none; }
		.back { display: inline-flex; }
		.msgs-thread { max-height: calc(100dvh - 12rem); min-height: 60dvh; }
	}
</style>
