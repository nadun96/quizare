<script lang="ts">
	// A student's presentations (D-76): published in their classrooms, with how far they are.
	import { api } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { progressText, type MyPresentation } from './presentation';

	let list = $state<MyPresentation[]>([]);
	$effect(() => {
		api.get<{ presentations: MyPresentation[] }>('/api/my/presentations').then((r) => (list = r.presentations), () => {});
	});
	const when = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
	const notYet = (p: MyPresentation) => !!p.opens_at && new Date(p.opens_at) > new Date();
</script>

{#if list.length}
	<h2>Presentations</h2>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6">
		<ul class="items" aria-label="My presentations">
			{#each list as p (p.id)}
				<li>
					<span class="icon" aria-hidden="true"><Icon name={p.finished ? 'check-circle' : 'monitor'} size={18} /></span>
					<div class="min-w-0 flex-1">
						<p class="m-0 truncate font-semibold">{p.title}{#if p.live}<span class="badge badge-error badge-soft badge-sm ml-2">Live now</span>{/if}</p>
						<p class="small muted m-0">{p.classroom} · {progressText(p)}{p.due_at && !p.finished ? ` · by ${when(p.due_at)}` : ''}</p>
					</div>
					{#if p.live}<a class="btn btn-sm btn-primary" href={`/slides/${p.id}/live`}>Join</a>
					{:else if notYet(p)}<span class="small muted whitespace-nowrap">Opens {when(p.opens_at!)}</span>
					{:else}<a class="btn btn-sm" class:btn-primary={!p.finished} href={'/slides/' + p.id}>{p.finished ? 'Look again' : p.started ? 'Continue' : 'Start'}</a>{/if}
				</li>
			{/each}
		</ul>
	</div>
{/if}

<style>
	.items { list-style: none; margin: 0; padding: 0; display: grid; }
	.items li { display: flex; align-items: center; gap: 0.75rem; padding: 0.6rem 0; border-top: 1px solid var(--color-base-300); }
	.items li:first-child { border-top: 0; }
	.icon { display: grid; place-items: center; width: 2.25rem; height: 2.25rem; border-radius: 0.6rem; background: color-mix(in oklab, var(--color-primary) 12%, var(--color-base-100)); color: var(--color-primary); flex: none; }
</style>
