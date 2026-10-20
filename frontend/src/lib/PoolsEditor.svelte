<script lang="ts">
	// A quiz's question pools (D-66): how many questions each student draws
	// from each category of the quiz, or of the teacher's question bank.
	// Quiz questions in a category without a rule are always asked.
	import { api, ApiError } from './api';
	import type { PoolCount, PoolInfo, PoolRule } from './types';
	import Icon from './ui/Icon.svelte';
	import OptionGroup from './ui/OptionGroup.svelte';
	import Skeleton from './ui/Skeleton.svelte';
	import { toast } from './ui/toast.svelte';
	import { count as words } from './words';

	let { quizId, onchange }: { quizId: string; onchange?: () => void } = $props();
	let info = $state<PoolInfo | null>(null);
	let rules = $state<PoolRule[]>([]);
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);

	$effect(() => {
		api.get<PoolInfo>('/api/teacher/quizzes/' + quizId + '/pools').then((i) => {
			info = i;
			rules = i.pools.map((r) => ({ ...r }));
		});
	});

	const label = (c: string) => c || 'No category';
	const key = (r: { source: string; category: string }) => r.source + '|' + r.category.toLowerCase();
	const sizeOf = (r: PoolRule) => (r.source === 'bank' ? info?.bank : info?.quiz)?.find((c) => c.category.toLowerCase() === r.category.toLowerCase())?.count ?? 0;
	const drawn = (r: PoolRule) => (r.count > 0 ? Math.min(r.count, sizeOf(r)) : sizeOf(r));
	// Quiz categories without a rule: always asked.
	const fixed = $derived((info?.quiz ?? []).filter((c) => !rules.some((r) => key(r) === key({ source: 'quiz', category: c.category }))));
	const fixedCount = $derived(fixed.reduce((n, c) => n + c.count, 0));
	const total = $derived(fixedCount + rules.reduce((n, r) => n + drawn(r), 0));
	const unused = (source: 'quiz' | 'bank') => ((source === 'bank' ? info?.bank : info?.quiz) ?? []).filter((c) => !rules.some((r) => key(r) === key({ source, category: c.category })));

	function add(source: 'quiz' | 'bank', c: PoolCount) {
		rules = [...rules, { source, category: c.category, count: Math.min(c.count, source === 'bank' ? 5 : Math.max(1, Math.floor(c.count / 2))) }];
	}
	function remove(i: number) {
		rules = rules.filter((_, j) => j !== i);
	}
	async function save() {
		saving = true;
		errors = {};
		try {
			info = await api.put<PoolInfo>('/api/teacher/quizzes/' + quizId + '/pools', { pools: rules });
			rules = info.pools.map((r) => ({ ...r }));
			toast(rules.length ? 'Pools saved' : 'Pools removed: every student gets every question');
			onchange?.();
		} catch (e) {
			if (e instanceof ApiError) errors = e.fields;
			toast(e instanceof ApiError ? e.message : 'Could not save the pools', 'error');
		} finally {
			saving = false;
		}
	}
</script>

{#if !info}
	<Skeleton lines={4} />
{:else}
	<div class="vstack">
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<p class="m-0 font-semibold">{rules.length ? `Each student gets ${words(total, 'question')}` : 'No pools: every student gets every question'}</p>
			<p class="small muted m-0">
				{#if rules.length}
					{fixedCount ? `${words(fixedCount, 'question')} always asked, plus the draws below.` : 'All drawn from the pools below.'} Each student's are picked at random when they join.
				{:else}
					Give questions a category (Easy, Hard, Plants…) in the editor or the CSV, then choose how many each student draws from each.
				{/if}
			</p>
		</div>

		{#if rules.length}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
				<h3 class="b-title"><Icon name="shuffle" size={16} />Drawn at random</h3>
				{#each rules as r, i (key(r))}
					<div class="prow">
						<span class="pname">
							<span class="badge badge-soft badge-sm">{r.source === 'bank' ? 'Bank' : 'This quiz'}</span>
							<span class="font-semibold">{label(r.category)}</span>
							<span class="small muted">{words(sizeOf(r), 'question')}</span>
						</span>
						<span class="flex flex-wrap items-center gap-2">
							<OptionGroup label="How many from {label(r.category)}" value={r.count === 0 ? 'all' : 'some'} onchange={(v) => (r.count = v === 'all' ? 0 : Math.max(1, Math.min(sizeOf(r), r.count || 1)))} options={[
								{ v: 'some', label: 'Some', icon: 'shuffle' },
								{ v: 'all', label: 'All of them', icon: 'check' }
							]} />
							{#if r.count > 0}
								<label class="input input-sm w-28" class:input-error={errors['pools.' + i] || r.count > sizeOf(r)}>
									<input type="number" min="1" max={sizeOf(r)} bind:value={r.count} aria-label="How many from {label(r.category)}" />
									<span class="muted">of {sizeOf(r)}</span>
								</label>
							{/if}
							<button type="button" class="btn btn-sm btn-ghost btn-square" aria-label="Remove the pool {label(r.category)}" title="Remove" onclick={() => remove(i)}><Icon name="x" size={16} /></button>
						</span>
					</div>
					{#if errors['pools.' + i]}<p class="field-error m-0">{errors['pools.' + i]}</p>{:else if r.count > sizeOf(r)}<p class="field-error m-0">Only {sizeOf(r)} in this pool.</p>{/if}
				{/each}
			</div>
		{/if}

		{#if fixed.length && rules.length}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
				<h3 class="b-title"><Icon name="check" size={16} />Always asked</h3>
				<p class="small muted m-0">These categories have no pool, so every student gets all of their questions.</p>
				<div class="flex flex-wrap gap-2 mt-2">{#each fixed as c (c.category)}<span class="badge badge-soft">{label(c.category)} · {c.count}</span>{/each}</div>
			</div>
		{/if}

		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<h3 class="b-title"><Icon name="plus" size={16} />Add a pool</h3>
			<p class="small muted m-0 mb-1">From this quiz's categories</p>
			<div class="flex flex-wrap gap-2">
				{#each unused('quiz') as c (c.category)}<button type="button" class="btn btn-sm" onclick={() => add('quiz', c)}><Icon name="plus" size={14} />{label(c.category)} <span class="muted">({c.count})</span></button>
				{:else}<span class="small muted">{info.quiz.length ? 'Every category has a pool.' : 'No questions yet.'}</span>{/each}
			</div>
			<p class="small muted m-0 mt-3 mb-1">From your <a href="/t/bank">question bank</a></p>
			<div class="flex flex-wrap gap-2">
				{#each unused('bank') as c (c.category)}<button type="button" class="btn btn-sm" onclick={() => add('bank', c)}><Icon name="plus" size={14} />{label(c.category)} <span class="muted">({c.count})</span></button>
				{:else}<span class="small muted">{info.bank.length ? 'Every bank category has a pool.' : 'Your bank is empty. Add questions to it to draw them into any quiz.'}</span>{/each}
			</div>
		</div>

		<div class="row">
			<button class="btn btn-primary" disabled={saving} onclick={save}>Save pools</button>
			{#if rules.length}<button class="btn btn-ghost" disabled={saving} onclick={() => { rules = []; save(); }}>Remove all pools</button>{/if}
		</div>
		<p class="small muted m-0">With pools, “Questions per student” in Settings doesn't apply. Running sessions keep the questions they started with.</p>
	</div>
{/if}

<style>
	.b-title { display: flex; align-items: center; gap: 0.45rem; margin: 0 0 0.5rem; font-size: 1rem; }
	.b-title :global(svg) { color: var(--color-primary); }
	.prow { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 0.5rem 1rem; padding: 0.6rem 0; border-top: 1px solid var(--color-base-300); }
	.pname { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; min-width: 0; }
</style>
