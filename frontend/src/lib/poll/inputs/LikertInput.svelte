<script lang="ts">
	// One radio group per statement. A table on wide screens; on phones each
	// statement becomes a card with the scale stacked, so nothing scrolls sideways.
	import type { Choice } from '../types';
	let { rows, scale, value = {}, disabled = false, name, onchange }: { rows: Choice[]; scale: string[]; value?: Record<string, string>; disabled?: boolean; name: string; onchange: (v: Record<string, string>) => void } = $props();
	function set(row: string, point: number) {
		onchange({ ...value, [row]: String(point) });
	}
</script>

<div class="likert" style:--points={scale.length}>
	<div class="head" aria-hidden="true">
		<span></span>
		{#each scale as l (l)}<span>{l}</span>{/each}
	</div>
	{#each rows as row (row.id)}
		<fieldset class="row-set">
			<legend class="stmt">{row.text}</legend>
			<div class="points">
				{#each scale as l, i (l)}
					<label class="pt" class:on={value[row.id] === String(i + 1)}>
						<input type="radio" class="radio radio-primary" name="{name}-{row.id}" checked={value[row.id] === String(i + 1)} {disabled} onchange={() => set(row.id, i + 1)} />
						<span class="pt-label">{l}</span>
					</label>
				{/each}
			</div>
		</fieldset>
	{/each}
</div>

<style>
	.likert { display: grid; gap: 0.5rem; }
	.head { display: none; }
	.row-set { border: 1px solid var(--color-base-300); border-radius: var(--radius-box); padding: 0.6rem 0.75rem 0.75rem; margin: 0; }
	.stmt { font-weight: 600; padding: 0 0.25rem; }
	.points { display: grid; gap: 0.35rem; }
	.pt { display: flex; align-items: center; gap: 0.6rem; font-weight: 400; margin: 0; padding: 0.45rem 0.6rem; border-radius: var(--radius-field); cursor: pointer; min-height: 2.75rem; }
	.pt:hover { background: var(--color-base-200); }
	.pt.on { background: color-mix(in oklab, var(--color-primary) 10%, transparent); }
	@media (min-width: 720px) {
		.head, .points { display: grid; grid-template-columns: minmax(10rem, 1.4fr) repeat(var(--points), minmax(0, 1fr)); align-items: center; }
		.head { gap: 0.25rem; font-size: 0.8rem; color: var(--color-muted); text-align: center; font-weight: 600; }
		.row-set { display: grid; grid-template-columns: minmax(10rem, 1.4fr) repeat(var(--points), minmax(0, 1fr)); align-items: center; border: 0; border-bottom: 1px solid var(--color-base-300); border-radius: 0; padding: 0.35rem 0; }
		.stmt { float: left; padding: 0; }
		.points { display: contents; }
		.pt { justify-content: center; padding: 0.35rem; }
		.pt-label { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
	}
</style>
