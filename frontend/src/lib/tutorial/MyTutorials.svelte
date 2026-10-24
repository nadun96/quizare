<script lang="ts">
	// A student's tutorials (D-74): published in their classrooms, with how far they are.
	import { api } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { progressText, type MyTutorial } from './tutorial';

	let list = $state<MyTutorial[]>([]);
	$effect(() => {
		api.get<{ tutorials: MyTutorial[] }>('/api/my/tutorials').then((r) => (list = r.tutorials), () => {});
	});
	const when = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
	const notYet = (t: MyTutorial) => !!t.opens_at && new Date(t.opens_at) > new Date();
</script>

{#if list.length}
	<h2>Tutorials</h2>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		<ul class="tutorials" aria-label="My tutorials">
			{#each list as t (t.id)}
				<li>
					<span class="icon" aria-hidden="true"><Icon name={t.finished ? 'check-circle' : 'book'} size={18} /></span>
					<div class="min-w-0 flex-1">
						<p class="m-0 truncate font-semibold">{t.title}</p>
						<p class="small muted m-0">{t.classroom} · {progressText(t)}{t.due_at && !t.finished ? ` · due ${when(t.due_at)}` : ''}</p>
					</div>
					{#if notYet(t)}<span class="small muted whitespace-nowrap">Opens {when(t.opens_at!)}</span>
					{:else}<a class="btn btn-sm" class:btn-primary={!t.finished} href={'/learn/' + t.id}>{t.finished ? 'Review' : t.started ? 'Continue' : 'Start'}</a>{/if}
				</li>
			{/each}
		</ul>
	</div>
{/if}

<style>
	.tutorials { list-style: none; margin: 0; padding: 0; display: grid; }
	.tutorials li { display: flex; align-items: center; gap: 0.75rem; padding: 0.6rem 0; border-top: 1px solid var(--color-base-300); }
	.tutorials li:first-child { border-top: 0; }
	.icon { display: grid; place-items: center; width: 2.25rem; height: 2.25rem; border-radius: 0.6rem; background: color-mix(in oklab, var(--color-primary) 12%, var(--color-base-100)); color: var(--color-primary); flex: none; }
</style>
