<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// "Report for": the whole classroom, a module, a topic, or one quiz or tutorial (D-130).
	import type { ScopeOption } from './grading';

	let { options, value, onchange }: { options: ScopeOption[]; value: string; onchange: (v: string) => void } = $props();
	const groups = $derived([...new Set(options.filter((o) => o.group).map((o) => o.group))]);
</script>

<label class="flex items-center gap-2 small">
	<span class="muted whitespace-nowrap">{tr("Report for")}</span>
	<select class="select select-sm w-auto max-w-72" class:select-primary={value !== 'all'} {value} onchange={(e) => onchange(e.currentTarget.value)}>
		{#each options.filter((o) => !o.group) as o (o.value)}<option value={o.value}>{tr(o.label)}</option>{/each}
		{#each groups as gr (gr)}
			<optgroup label={gr}>{#each options.filter((o) => o.group === gr) as o (o.value)}<option value={o.value}>{tr(o.label)}</option>{/each}</optgroup>
		{/each}
	</select>
</label>
