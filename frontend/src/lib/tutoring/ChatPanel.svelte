<script lang="ts">
	// The session's chat (TS-FR-40 to TS-FR-45), and while breakout groups are
	// open, each group's own chat (TS-FR-46, D-79). Text is shown as text with
	// clickable links, never as HTML (TS-FR-43).
	import { ApiError } from '../api';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import type { TutorClient } from './client';
	import { audienceLabel, CHAT_MODES, linkParts, type ChatMessage, type TutoringView } from './types';

	let { client, view, messages, onreply }: { client: TutorClient; view: TutoringView; messages: ChatMessage[]; onreply?: (m: ChatMessage) => void } = $props();
	const teacher = $derived(view.me.role === 'teacher' || view.me.role === 'coteacher');
	// A student in an open group chooses between the class's chat and their group's.
	const inGroup = $derived(!teacher && view.session.groups_open && !!view.me.group);
	let room = $state<'class' | 'group'>('class');
	$effect(() => {
		if (!inGroup) room = 'class';
	});
	const groupName = (id?: number) => view.groups.find((g) => g.id === id)?.name ?? 'a group';
	const shown = $derived(teacher ? messages : messages.filter((m) => (room === 'group') === (m.audience === 'group')));
	const path = $derived('/api/sessions/' + view.session.id);
	const mode = $derived(CHAT_MODES.find((m) => m.id === view.session.chat_mode));
	const canWrite = $derived(
		teacher || (room === 'group' ? view.session.group_chat && !view.me.chat_muted : view.session.chat_mode !== 'off' && view.session.chat_mode !== 'announcements' && !view.me.chat_muted)
	);
	let text = $state('');
	let to = $state('');
	let list = $state<HTMLOListElement>();
	let sending = $state(false);
	const students = $derived((view.participants ?? []).filter((p) => p.role === 'student' && p.state === 'admitted'));

	$effect(() => {
		shown.length;
		queueMicrotask(() => list?.lastElementChild?.scrollIntoView({ block: 'nearest' }));
	});

	export function replyTo(userId: string) {
		to = userId;
	}

	async function send(e: SubmitEvent) {
		e.preventDefault();
		if (!text.trim()) return;
		sending = true;
		try {
			const target = teacher && to.startsWith('g:') ? { group_id: Number(to.slice(2)) } : teacher && to ? { to } : room === 'group' ? { group: true } : {};
			await client.post(path + '/chat', { text, ...target });
			text = '';
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'Not sent', 'error');
		} finally {
			sending = false;
		}
	}
	async function remove(m: ChatMessage) {
		if (!(await confirmDialog({ title: 'Delete this message for everyone?', confirm: 'Delete', danger: true }))) return;
		await client.del(path + '/chat/' + m.id);
	}
	const pin = (m: ChatMessage | null) => client.put(path + '/pin', { id: m?.id ?? 0 });
	const time = (s: string) => new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	const nameOf = (id?: string) => students.find((s) => s.user_id === id)?.name ?? 'a student';
</script>

<section class="chat vstack" aria-label="Chat">
	{#if inGroup}
		<div class="join" role="tablist" aria-label="Which chat">
			<button class="btn btn-xs join-item" role="tab" aria-selected={room === 'class'} class:btn-primary={room === 'class'} onclick={() => (room = 'class')}>Class</button>
			<button class="btn btn-xs join-item" role="tab" aria-selected={room === 'group'} class:btn-primary={room === 'group'} onclick={() => (room = 'group')}>{groupName(view.me.group)}</button>
		</div>
	{/if}
	{#if room === 'group'}
		<p class="small muted m-0">{view.session.group_chat ? 'Your group sees these messages, and so do your teachers.' : 'Chat in groups is off.'}</p>
	{:else}
	<p class="small muted m-0">{teacher ? `Chat: ${mode?.label}` : mode?.student}{#if view.session.slow_seconds}{' · '}slow mode, one message every {view.session.slow_seconds} s{/if}{#if view.me.chat_muted}{' · '}the teacher has muted you in the chat{/if}</p>
	{/if}
	{#if view.pinned}
		<div class="alert alert-soft alert-info small py-2" role="note">
			<span><strong>Pinned:</strong> {#each linkParts(view.pinned.text) as p, i (i)}{#if p.href}<a href={p.href} target="_blank" rel="noopener noreferrer nofollow">{p.text}</a>{:else}{p.text}{/if}{/each}</span>
			{#if teacher}<button class="btn btn-xs btn-ghost" onclick={() => pin(null)}>Unpin</button>{/if}
		</div>
	{/if}
	<ol class="msgs" bind:this={list} aria-live="polite">
		{#each shown as m (m.id)}
			<li class:mine={m.user_id === view.me.user_id} class:teacher={m.from_teacher}>
				<div class="small"><strong>{m.name}</strong>{#if m.from_teacher}{' '}<span class="badge badge-soft badge-xs">teacher</span>{/if} <span class="muted">{time(m.created_at)}{#if audienceLabel(m, view.me.user_id)}{' · '}{audienceLabel(m, view.me.user_id)}{/if}{#if teacher && m.audience === 'one'}{' '}to {nameOf(m.to_user)}{/if}{#if teacher && m.audience === 'group'}{' '}in {groupName(m.group_id)}{/if}</span></div>
				<p class="m-0 text">{#each linkParts(m.text) as p, i (i)}{#if p.href}<a href={p.href} target="_blank" rel="noopener noreferrer nofollow">{p.text}</a>{:else}{p.text}{/if}{/each}</p>
				{#if teacher}
					<div class="acts">
						{#if !m.from_teacher}<button class="btn btn-xs btn-ghost" onclick={() => { to = m.user_id; onreply?.(m); }}>Reply privately</button>{/if}
						{#if m.audience === 'everyone'}<button class="btn btn-xs btn-ghost" onclick={() => pin(m)}>Pin</button>{/if}
						<button class="btn btn-xs btn-ghost" onclick={() => remove(m)}>Delete</button>
					</div>
				{/if}
			</li>
		{:else}
			<li class="muted small">No messages yet.</li>
		{/each}
	</ol>
	{#if canWrite}
		<form class="vstack gap-1" onsubmit={send}>
			{#if teacher}
				<label class="small" for="chat-to">To</label>
				<select id="chat-to" class="select select-sm w-full" bind:value={to}>
					<option value="">Everyone</option>
					{#if view.session.groups_open}{#each view.groups as g (g.id)}<option value={'g:' + g.id}>{g.name} (its chat)</option>{/each}{/if}
					{#each students as s (s.user_id)}<option value={s.user_id}>{s.name} (private)</option>{/each}
				</select>
			{/if}
			<div class="flex gap-2">
				<label class="sr-only" for="chat-text">Message</label>
				<input id="chat-text" class="input input-sm w-full" bind:value={text} maxlength="1000" placeholder={teacher ? 'Write to the class…' : view.session.chat_mode === 'to_teacher' ? 'Ask the teacher…' : 'Write a message…'} autocomplete="off" />
				<button class="btn btn-sm btn-primary" disabled={sending || !text.trim()}>Send</button>
			</div>
		</form>
	{/if}
</section>

<style>
	.chat { min-height: 0; }
	.msgs { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.5rem; max-height: 50vh; overflow-y: auto; }
	.msgs li { padding: 0.4rem 0.6rem; border-radius: 0.6rem; background: var(--color-base-200); }
	.msgs li.mine { background: color-mix(in oklab, var(--color-primary) 12%, var(--color-base-100)); }
	.text { white-space: pre-wrap; overflow-wrap: anywhere; }
	.acts { display: flex; gap: 0.25rem; flex-wrap: wrap; margin-top: 0.15rem; }
</style>
