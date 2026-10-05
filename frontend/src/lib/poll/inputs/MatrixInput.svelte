<script lang="ts">
	// Rows × columns of radios, checkboxes or text boxes. A table where it fits;
	// below 640 px each row is a card (no sideways scrolling on phones).
	import type { Choice, PollAnswer } from '../types';
	let { rows, columns, mode = 'single', value = {}, disabled = false, name, onchange }: {
		rows: Choice[]; columns: Choice[]; mode?: 'single' | 'multi' | 'text'; value?: PollAnswer; disabled?: boolean; name: string; onchange: (v: PollAnswer) => void;
	} = $props();

	const single = (r: string, c: string) => onchange({ rows: { ...(value.rows ?? {}), [r]: c } });
	function multi(r: string, c: string, on: boolean) {
		const cur = new Set(value.multi?.[r] ?? []);
		if (on) cur.add(c);
		else cur.delete(c);
		const m = { ...(value.multi ?? {}) };
		if (cur.size) m[r] = columns.map((x) => x.id).filter((id) => cur.has(id));
		else delete m[r];
		onchange({ multi: m });
	}
	let timer: ReturnType<typeof setTimeout> | null = null;
	let cells = $state<Record<string, string>>({});
	$effect.pre(() => {
		cells = { ...(value.cells ?? {}) };
	});
	function text(r: string, c: string, v: string) {
		cells[r + '|' + c] = v;
		if (timer) clearTimeout(timer);
		timer = setTimeout(() => onchange({ cells: Object.fromEntries(Object.entries(cells).filter(([, x]) => x.trim())) }), 600);
	}
</script>

<div class="matrix" style:--cols={columns.length}>
	<div class="mhead" aria-hidden="true">
		<span></span>
		{#each columns as c (c.id)}<span>{c.text}</span>{/each}
	</div>
	{#each rows as r (r.id)}
		<fieldset class="mrow">
			<legend class="mlabel">{r.text}</legend>
			<div class="mcells">
				{#each columns as c (c.id)}
					{#if mode === 'text'}
						<label class="mcell text">
							<span class="clabel">{c.text}</span>
							<input class="input input-sm w-full" value={cells[r.id + '|' + c.id] ?? ''} {disabled} aria-label="{r.text}: {c.text}" oninput={(e) => text(r.id, c.id, e.currentTarget.value)} maxlength="200" />
						</label>
					{:else}
						<label class="mcell" class:on={mode === 'single' ? value.rows?.[r.id] === c.id : value.multi?.[r.id]?.includes(c.id)}>
							{#if mode === 'single'}
								<input type="radio" class="radio radio-primary" name="{name}-{r.id}" checked={value.rows?.[r.id] === c.id} {disabled} onchange={() => single(r.id, c.id)} />
							{:else}
								<input type="checkbox" class="checkbox checkbox-primary" checked={value.multi?.[r.id]?.includes(c.id) ?? false} {disabled} onchange={(e) => multi(r.id, c.id, e.currentTarget.checked)} />
							{/if}
							<span class="clabel">{c.text}</span>
						</label>
					{/if}
				{/each}
			</div>
		</fieldset>
	{/each}
</div>

<style>
	.matrix { display: grid; gap: 0.5rem; }
	.mhead { display: none; }
	.mrow { border: 1px solid var(--color-base-300); border-radius: var(--radius-box); padding: 0.6rem 0.75rem 0.75rem; margin: 0; }
	.mlabel { font-weight: 600; padding: 0 0.25rem; }
	.mcells { display: grid; gap: 0.35rem; }
	.mcell { display: flex; align-items: center; gap: 0.6rem; margin: 0; font-weight: 400; padding: 0.4rem 0.6rem; border-radius: var(--radius-field); min-height: 2.75rem; cursor: pointer; }
	.mcell.text { display: grid; gap: 0.25rem; cursor: default; }
	.mcell.on { background: color-mix(in oklab, var(--color-primary) 10%, transparent); }
	@media (min-width: 640px) {
		.mhead, .mrow { display: grid; grid-template-columns: minmax(8rem, 1.3fr) repeat(var(--cols), minmax(0, 1fr)); align-items: center; gap: 0.25rem; }
		.mhead { font-size: 0.8rem; color: var(--color-muted); text-align: center; font-weight: 600; }
		.mrow { border: 0; border-bottom: 1px solid var(--color-base-300); border-radius: 0; padding: 0.35rem 0; }
		.mlabel { float: left; padding: 0; }
		.mcells { display: contents; }
		.mcell { justify-content: center; }
		.mcell .clabel { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
	}
</style>
