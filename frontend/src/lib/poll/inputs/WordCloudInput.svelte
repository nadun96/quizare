<script lang="ts">
	// Up to N short entries, added as chips (Enter or comma adds one).
	import Icon from '../../ui/Icon.svelte';
	import { scaleIn } from '../../ui/motion';
	let { max = 3, value = [], disabled = false, label, onchange }: { max?: number; value?: string[]; disabled?: boolean; label: string; onchange: (v: string[]) => void } = $props();
	let draft = $state('');
	let input = $state<HTMLInputElement>();
	// Local copy, so quick entries are checked against each other before the
	// parent echoes the saved answer back.
	let list = $state<string[]>([]);
	$effect.pre(() => {
		list = [...value];
	});
	const full = $derived(list.length >= max);
	function set(next: string[]) {
		list = next;
		onchange(next);
	}

	function add() {
		const w = draft.trim().replace(/\s+/g, ' ').slice(0, 40);
		draft = '';
		if (!w || full || list.some((x) => x.toLowerCase() === w.toLowerCase())) return;
		set([...list, w]);
	}
	function key(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			e.preventDefault();
			add();
		} else if (e.key === 'Backspace' && !draft && list.length) set(list.slice(0, -1));
	}
</script>

<div class="vstack">
	<div class="flex gap-2">
		<input
			class="input w-full"
			bind:this={input}
			bind:value={draft}
			onkeydown={key}
			maxlength="40"
			placeholder={full ? `That's ${max} — remove one to change it` : list.length ? 'Add another…' : 'Type a word or short phrase'}
			disabled={disabled || full}
			aria-label={label}
			autocomplete="off"
		/>
		<button type="button" class="btn btn-primary" onclick={add} disabled={disabled || full || !draft.trim()}><Icon name="plus" size={16} />Add</button>
	</div>
	<div class="flex flex-wrap items-center gap-2" aria-live="polite">
		{#each list as w (w)}
			<span class="badge badge-lg badge-soft badge-primary gap-1 pr-1" in:scaleIn>
				{w}
				{#if !disabled}<button type="button" class="btn btn-ghost btn-xs btn-circle" aria-label="Remove {w}" onclick={() => set(list.filter((x) => x !== w))}><Icon name="x" size={12} /></button>{/if}
			</span>
		{/each}
		<span class="small muted">{list.length} of {max}</span>
	</div>
</div>
