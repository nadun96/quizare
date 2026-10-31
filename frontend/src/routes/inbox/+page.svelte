<script lang="ts">
	// The inbox (D-103): announcements from (or to) classrooms and messages
	// between teachers and their students, with the choice of getting new
	// ones by email. ?tab=messages&c=… opens a conversation (the email links
	// here); ?classroom=…&new=1 starts an announcement to a classroom.
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import { auth, loginUrl } from '$lib/session.svelte';
	import { badge, inbox } from '$lib/inbox/inbox.svelte';
	import Announcements from '$lib/inbox/Announcements.svelte';
	import Messages from '$lib/inbox/Messages.svelte';
	import { urlState } from '$lib/urlstate';
	import { toast } from '$lib/ui/toast.svelte';

	const q = page.url.searchParams;
	let tab = $state<'announcements' | 'messages'>(q.get('tab') === 'messages' || q.get('c') ? 'messages' : 'announcements');
	const conversation = q.get('c') ?? '';
	const classroom = q.get('classroom') ?? '';
	const compose = q.get('new') === '1';

	$effect(() => {
		if (!auth.loaded) return;
		if (!auth.user) return void goto(loginUrl(page.url.pathname + page.url.search));
		if (auth.user.role !== 'student' && auth.user.role !== 'teacher') return void goto(auth.home());
		inbox.load(true);
	});
	const teacher = $derived(auth.user?.role === 'teacher');

	function show(t: typeof tab) {
		tab = t;
		const p = urlState.read();
		p.set('tab', t);
		if (t !== 'messages') p.delete('c');
		urlState.write(p);
	}
	function opened(id: string) {
		const p = urlState.read();
		p.set('tab', 'messages');
		p.set('c', id);
		urlState.write(p);
	}
	async function setEmail(on: boolean) {
		try {
			await api.put('/api/my/inbox/prefs', { email: on });
			inbox.current = inbox.current ? { ...inbox.current, email: on } : null;
			toast(on ? 'New announcements and messages come by email too' : 'No more emails for announcements and messages');
		} catch {
			toast('Could not change it', 'error');
		}
	}
</script>

<svelte:head><title>Inbox · Classroom Quiz</title></svelte:head>

{#if auth.user && (auth.user.role === 'student' || auth.user.role === 'teacher')}
	<div class="page-container vstack py-6">
		<div class="flex flex-wrap items-end gap-3">
			<div class="grow">
				<h1 class="m-0">Inbox</h1>
				<p class="m-0 muted">{teacher ? 'Announcements to your classrooms, and messages with your students.' : 'Announcements from your classrooms, and messages with your teachers.'}</p>
			</div>
			{#if inbox.current}
				<label class="flex items-center gap-2 small"><input type="checkbox" class="toggle toggle-sm toggle-primary" checked={inbox.current.email} onchange={(e) => setEmail(e.currentTarget.checked)} />Email me new ones</label>
			{/if}
		</div>

		<div role="tablist" class="tabs tabs-box w-fit" aria-label="Inbox">
			<button class="tab gap-2" role="tab" aria-selected={tab === 'announcements'} class:tab-active={tab === 'announcements'} onclick={() => show('announcements')}>
				Announcements{#if !teacher && inbox.current?.announcements}<span class="badge badge-primary badge-sm" aria-label="{inbox.current.announcements} unread">{badge(inbox.current.announcements)}</span>{/if}
			</button>
			<button class="tab gap-2" role="tab" aria-selected={tab === 'messages'} class:tab-active={tab === 'messages'} onclick={() => show('messages')}>
				Messages{#if inbox.current?.messages}<span class="badge badge-primary badge-sm" aria-label="{inbox.current.messages} unread">{badge(inbox.current.messages)}</span>{/if}
			</button>
		</div>

		{#if tab === 'announcements'}
			<Announcements {teacher} {classroom} {compose} />
		{:else}
			<Messages {teacher} initial={conversation} onopen={opened} />
		{/if}
	</div>
{/if}
