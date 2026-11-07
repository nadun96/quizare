<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Question pools (D-123): the teacher's own question bank (D-66) and the
	// pool of every classroom they teach, each by category. Files imported
	// here go where their names say ("Classroom__Category.csv").
	import { api } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import Icon from '$lib/ui/Icon.svelte';
	import Skeleton from '$lib/ui/Skeleton.svelte';
	import PoolBrowser from '$lib/pools/PoolBrowser.svelte';
	import PoolImporter from '$lib/pools/PoolImporter.svelte';
	import PoolTabs from '$lib/pools/PoolTabs.svelte';
	import type { PoolSummary } from '$lib/pools/pools';

	const ready = requireRole('teacher');
	let pools = $state<PoolSummary[] | null>(null);
	let current = $state<string>('');
	let version = $state(0);
	async function load() {
		pools = (await api.get<{ pools: PoolSummary[] }>('/api/teacher/pools')).pools;
		if (!current) current = pools[0]?.quiz_id ?? '';
	}
	$effect(() => {
		if (ready()) load();
	});
	async function openPool(p: PoolSummary) {
		if (!p.quiz_id) p.quiz_id = (await api.get<{ id: string }>(`/api/teacher/classrooms/${p.classroom_id}/pool`)).id;
		current = p.quiz_id;
	}
	const name = (p: PoolSummary) => (p.kind === 'bank' ? 'My question bank' : p.classroom!);
	const currentPool = $derived(pools?.find((p) => p.quiz_id === current));
</script>

<svelte:head><title>{tr("Question pools · Classroom Quiz")}</title></svelte:head>

<div class="page-container vstack gap-4">
	<div>
		<h1 class="m-0">{tr("Question pools")}</h1>
		<p class="m-0 muted">{tr("Your own bank, and a pool for each classroom you teach, by category. Quizzes draw so many questions from each category.")}</p>
	</div>
	{#if !pools}
		<Skeleton lines={5} />
	{:else}
		<PoolImporter {pools} onimported={() => { version++; load(); }} />

		<div class="grid">
			{#each pools as p (p.kind + (p.classroom_id ?? ''))}
				<button class="pool card card-border bg-base-100" class:on={p.quiz_id && p.quiz_id === current} onclick={() => openPool(p)} aria-pressed={!!p.quiz_id && p.quiz_id === current}>
					<span class="flex items-center gap-2">
						<Icon name={p.kind === 'bank' ? 'user' : 'book'} size={16} />
						<strong class="truncate">{name(p)}</strong>
						{#if p.role === 'coteacher'}<span class="badge badge-soft badge-xs">{tr("co-teacher")}</span>{/if}
					</span>
					<span class="small muted">{p.questions} {p.questions === 1 ? 'question' : 'questions'}{p.categories.length ? ` · ${p.categories.length} ${p.categories.length === 1 ? 'category' : 'categories'}` : ''}</span>
					{#if p.categories.length}
						<span class="cats">{#each p.categories.slice(0, 6) as c (c.category)}<span class="badge badge-soft badge-sm">{c.category || tr("No category")} · {c.count}</span>{/each}{#if p.categories.length > 6}<span class="small muted">+{p.categories.length - 6}</span>{/if}</span>
					{/if}
				</button>
			{/each}
		</div>

		{#if current && currentPool}
			<section class="vstack gap-2" aria-label={name(currentPool)}>
				<h2 class="m-0 text-lg">{name(currentPool)}</h2>
				{#key current}
					<PoolTabs quizId={current} onchange={load}>
						{#snippet questions()}{#key version}<PoolBrowser quizId={current} onchange={load} />{/key}{/snippet}
					</PoolTabs>
				{/key}
			</section>
		{/if}
	{/if}
</div>

<style>
	.grid { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr)); }
	.pool { display: flex; flex-direction: column; align-items: flex-start; gap: 0.35rem; padding: 0.9rem; text-align: left; }
	.pool:hover { border-color: var(--color-primary); }
	.pool.on { border-color: var(--color-primary); box-shadow: inset 0 0 0 1px var(--color-primary); }
	.cats { display: flex; flex-wrap: wrap; gap: 0.25rem; }
</style>
