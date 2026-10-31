<script lang="ts">
	// One dashboard list (D-99), a page at a time, its own page in the address.
	import { untrack } from 'svelte';
	import { Paged } from '../paged.svelte';
	import { urlState } from '../urlstate';
	import Icon from '../ui/Icon.svelte';
	import Pager from '../ui/Pager.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { KIND, tone, when, type Item, type ListName } from './dashboard';
	import { t } from '../i18n/i18n.svelte';

	let { name, title, empty }: { name: ListName; title: string; empty: string } = $props();
	// A list keeps its name for its life; its page sits under that name in the address.
	const list = new Paged<Item>(() => `/api/my/dashboard/${name}`, 'items', { url: urlState, prefix: untrack(() => name) });
	list.load();
	const now = Date.now();
</script>

<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-5 vstack gap-2" aria-labelledby="dash-{name}">
	<h2 id="dash-{name}" class="m-0 text-lg flex items-center gap-2">
		{title}
		{#if list.loaded && list.total}<span class="badge badge-sm" class:badge-error={name === 'overdue'} class:badge-primary={name === 'live'}>{list.total}</span>{/if}
	</h2>
	{#if !list.loaded}
		<Skeleton card={false} lines={2} />
	{:else if !list.rows.length}
		<p class="m-0 small muted">{empty}</p>
	{:else}
		<ul class="m-0 p-0 list-none">
			{#each list.rows as it (it.kind + it.id)}
				<li class="row-item">
					<span class="kind tone-{tone(it.status)}" aria-hidden="true"><Icon name={KIND[it.kind].icon} size={18} /></span>
					<span class="min-w-0 flex-1">
						{#if it.href}<a class="title" href={it.href}>{it.title}</a>{:else}<span class="title">{it.title}</span>{/if}
						<span class="meta small">
							<span class="sr-only">{t(KIND[it.kind].label)}.</span>
							<span class="when tone-{tone(it.status)}">{when(it, now)}</span>
							{#if it.classroom}<span class="muted">{it.classroom}</span>{/if}
							{#if it.detail}<span class="muted">{it.detail}</span>{/if}
						</span>
					</span>
				</li>
			{/each}
		</ul>
		<Pager {list} label={title} />
	{/if}
</section>

<style>
	.row-item { display: flex; align-items: flex-start; gap: 0.75rem; padding: 0.6rem 0; border-top: 1px solid var(--color-base-200); }
	.row-item:first-child { border-top: none; }
	.kind { flex: none; display: grid; place-items: center; width: 2.1rem; height: 2.1rem; border-radius: 0.6rem; background: var(--color-base-200); }
	.title { display: block; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	a.title { color: var(--color-base-content); text-decoration: none; }
	a.title:hover { text-decoration: underline; }
	.meta { display: flex; flex-wrap: wrap; gap: 0.15rem 0.75rem; }
	.when { font-weight: 600; }
	.kind.tone-live { background: color-mix(in oklab, var(--color-primary) 16%, var(--color-base-100)); color: var(--color-primary); }
	.kind.tone-bad { background: color-mix(in oklab, var(--color-error) 14%, var(--color-base-100)); color: var(--color-error); }
	.kind.tone-warn { background: color-mix(in oklab, var(--color-warning) 16%, var(--color-base-100)); color: var(--color-warning); }
	.when.tone-live { color: var(--color-primary); }
	.when.tone-bad { color: var(--color-error); }
	.when.tone-warn { color: var(--color-warning); }
</style>
