<script lang="ts">
	// Classroom screen: big QR + code + link, with a live joined count (UC-02 step 2).
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/state';
	import { requireRole } from '$lib/guard.svelte';
	import QrCode from '$lib/QrCode.svelte';
	import { LiveSocket } from '$lib/socket';
	import type { Dashboard } from '$lib/types';

	requireRole('teacher');
	let d = $state<Dashboard | null>(null);
	let socket: LiveSocket;
	let size = $state(480);
	onMount(() => {
		size = Math.min(window.innerHeight - 220, window.innerWidth - 40, 720);
		socket = new LiveSocket('/ws/sessions/' + page.params.id);
		socket.onMessage = (m) => {
			if (m.type === 'dashboard') d = m as unknown as Dashboard;
		};
		socket.open();
	});
	onDestroy(() => socket?.close());
	const joined = $derived(d ? Object.values(d.counts).reduce((a, b) => a + b, 0) : 0);
</script>

<div class="proj">
	{#if d}
		<h1>{d.session.title}</h1>
		<QrCode text={d.session.join_url} {size} />
		<p class="code">{d.session.join_code}</p>
		<p class="url">{d.session.join_url.replace(/^https?:\/\//, '')}</p>
		<p class="count">{joined} joined</p>
	{:else}<p>Loading…</p>{/if}
</div>

<style>
	.proj { min-height: 100dvh; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; gap: 0.5rem; padding: 1rem; background: #fff; color: #111; }
	h1 { font-size: 2rem; margin: 0; }
	.code { font-size: 3rem; font-weight: 800; letter-spacing: 0.15em; margin: 0; }
	.url { font-size: 1.4rem; margin: 0; }
	.count { font-size: 1.2rem; color: #555; }
</style>
