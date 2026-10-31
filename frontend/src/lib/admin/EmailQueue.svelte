<script lang="ts">
	// The email queue (D-104): every email the platform sent this week, as
	// waiting, sent (the mail server took it), retrying or failed with the
	// server's words, and sending failed ones again. Never the text: it holds
	// sign-in links. Refreshes every 10 seconds while anything is on its way.
	import { onDestroy, untrack } from 'svelte';
	import { api } from '../api';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import ListSearch from '../ui/ListSearch.svelte';
	import Pager from '../ui/Pager.svelte';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { STATUS, type Delivery, type Progress } from './emailqueue';

	let { send = '', label = '' }: { send?: string; label?: string } = $props();

	let status = $state(Paged.fromUrl(urlState, 'status', 'eq'));
	let counts = $state<Progress>({});
	const list = new Paged<Delivery>(() => '/api/admin/email/queue', 'emails', {
		url: untrack(() => send) ? null : urlState,
		prefix: 'eq',
		sort: 'created',
		desc: true,
		filters: () => ({ status: status || undefined, send: send || undefined }),
		get: async (url) => {
			const r = await api.get<Record<string, unknown>>(url);
			counts = (r.counts as Progress) ?? {};
			return r;
		}
	});
	list.load();
	const timer = setInterval(() => {
		if (document.visibilityState === 'visible' && ((counts.waiting ?? 0) > 0 || (counts.retrying ?? 0) > 0)) list.load();
	}, 10_000);
	onDestroy(() => clearInterval(timer));

	function show(s: string) {
		status = status === s ? '' : s;
		list.refilter();
	}
	async function retry(id?: number) {
		try {
			const r = await api.post<{ retried: number }>('/api/admin/email/queue/retry', id ? { id } : { send_id: send || undefined });
			toast(r.retried ? `Sending ${r.retried} again` : 'Nothing to send again');
			list.load();
		} catch {
			toast('Could not send it again', 'error');
		}
	}
	const date = (iso: string) => new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
</script>

<div class="vstack">
	{#if label}<p class="m-0 font-semibold">{label}</p>{/if}
	<div class="flex flex-wrap gap-2" role="group" aria-label="Show">
		{#each STATUS as s (s.id)}
			<button class="btn btn-sm {status === s.id ? 'btn-active' : ''}" aria-pressed={status === s.id} onclick={() => show(s.id)}>
				<span class="badge badge-sm {s.badge}">{counts[s.id] ?? 0}</span>{s.label}
			</button>
		{/each}
		<span class="spacer"></span>
		{#if (counts.failed ?? 0) > 0}<button class="btn btn-sm" onclick={() => retry()}><Icon name="undo" size={14} />Send failed ones again</button>{/if}
	</div>
	{#if !send}<ListSearch {list} placeholder="Search address or subject" label="Search emails" />{/if}
	<div class="table-wrap">
		<table class="table table-sm">
			<thead><tr><th>To</th><th>Subject</th><th>Status</th><th>Queued</th><th><span class="sr-only">Actions</span></th></tr></thead>
			<tbody>
				{#each list.rows as d (d.id)}
					{@const s = STATUS.find((x) => x.id === d.status)}
					<tr>
						<td class="break-all">{d.to}</td>
						<td>{d.subject}</td>
						<td>
							<span class="badge badge-sm {s?.badge}">{s?.word ?? d.status}</span>
							{#if d.status === 'retrying'}<span class="small muted"> try {d.attempts + 1} of {d.max_attempts}{#if d.at}, {new Date(d.at).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}{/if}</span>{/if}
							{#if d.error && d.status !== 'sent'}<p class="m-0 mt-1 small text-error break-words max-w-md">{d.error}</p>{/if}
						</td>
						<td class="small tabular whitespace-nowrap">{date(d.created_at)}</td>
						<td class="text-right">{#if d.status === 'failed'}<button class="btn btn-xs" onclick={() => retry(d.id)}>Send again</button>{/if}</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">{list.loaded ? (status || list.q ? 'No emails match.' : 'No emails this week.') : 'Loading…'}</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
	<Pager {list} label="Emails" />
	<p class="m-0 small muted">"Sent" means the mail server took it. A provider refusing it later (a bounce) shows in the server's mail log. Emails are kept here for a week.</p>
</div>
