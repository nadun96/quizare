<script lang="ts">
	// Editable list of choices (options, rows, columns, boxes, statements).
	import { flip } from 'svelte/animate';
	import Icon from '../ui/Icon.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { flipMs } from '../ui/motion';
	import type { Choice } from './types';

	let { items = $bindable([]), label, noun = 'option', min = 2, max = 50, letters = false }: { items?: Choice[]; label: string; noun?: string; min?: number; max?: number; letters?: boolean } = $props();
	let keySeq = 0;
	const keys = new WeakMap<Choice, number>();
	const keyOf = (c: Choice) => {
		if (!keys.has(c)) keys.set(c, ++keySeq);
		return keys.get(c)!;
	};
	let inputs: HTMLInputElement[] = $state([]);

	function add(after = items.length - 1) {
		items.splice(after + 1, 0, { id: '', text: '' });
		queueMicrotask(() => inputs[after + 1]?.focus());
	}
	function move(i: number, d: number) {
		const j = i + d;
		if (j < 0 || j >= items.length) return;
		[items[i], items[j]] = [items[j], items[i]];
	}
</script>

<fieldset class="choice-list">
	<legend class="small font-semibold">{label}</legend>
	<ol class="vstack">
		{#each items as c, i (keyOf(c))}
			<li class="flex items-center gap-2" animate:flip={{ duration: flipMs() }}>
				<span class="mark" aria-hidden="true">{letters ? String.fromCharCode(65 + i) : i + 1}</span>
				<input
					class="input input-sm w-full"
					bind:this={inputs[i]}
					bind:value={c.text}
					maxlength="300"
					aria-label="{noun} {i + 1}"
					placeholder="{noun[0].toUpperCase() + noun.slice(1)} {i + 1}"
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							e.preventDefault();
							if (items.length < max) add(i);
						}
					}}
				/>
				<IconBtn icon="arrow-up" label="Up" hint="Move {noun} {i + 1} up" disabled={i === 0} onclick={() => move(i, -1)} />
				<IconBtn icon="arrow-down" label="Down" hint="Move {noun} {i + 1} down" disabled={i === items.length - 1} onclick={() => move(i, 1)} />
				<IconBtn icon="x" label="Remove" hint="Remove {noun} {i + 1}" size={15} disabled={items.length <= min} onclick={() => items.splice(i, 1)} />
			</li>
		{/each}
	</ol>
	<button type="button" class="btn btn-sm mt-2" disabled={items.length >= max} onclick={() => add()}><Icon name="plus" size={14} />Add {noun}</button>
</fieldset>

<style>
	.choice-list { border: 0; padding: 0; margin: 0; }
	.choice-list legend { padding: 0; margin-bottom: 0.4rem; }
	ol { list-style: none; padding: 0; margin: 0; }
	ol > li + li { margin-top: 0.4rem; }
	.mark { width: 1.6rem; text-align: center; font-size: 0.8rem; font-weight: 700; color: var(--color-muted); flex: none; }
</style>
