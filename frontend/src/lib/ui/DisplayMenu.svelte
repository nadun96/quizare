<script lang="ts">
	// Display preferences (D-39, D-48): theme (ours or any daisyUI theme),
	// main colour, button labels, text size and motion. The research found that
	// letting students tune the screen reduces stress; also WCAG 1.4.4
	// (resize) and 2.3.3 (motion). Every theme and colour is kept at AA.
	import Icon, { type IconName } from './Icon.svelte';
	import { daisyTheme, prefs, themeValue, type TextSize } from './prefs.svelte';
	import { DAISY_THEMES } from './themes.gen';

	let { idPrefix = 'disp' }: { idPrefix?: string } = $props();
	const own: [string, string, IconName][] = [['system', 'System', 'monitor'], ['light', 'Light', 'sun'], ['dark', 'Dark', 'moon']];
	const sizes: [TextSize, string, string][] = [['md', 'A', 'Normal'], ['lg', 'A+', 'Larger'], ['xl', 'A++', 'Largest']];
	// Main-colour presets; each is tuned to the theme when applied.
	const COLORS: [string, string][] = [['#1f5aa6', 'Blue'], ['#0f766e', 'Teal'], ['#15803d', 'Green'], ['#6d28d9', 'Purple'], ['#be185d', 'Pink'], ['#c2410c', 'Orange'], ['#b91c1c', 'Red'], ['#334155', 'Slate']];
	// daisyUI's plain "light" and "dark" sit beside ours, so they get distinct names.
	const title = (n: string) => (n === 'light' ? 'Neutral light' : n === 'dark' ? 'Neutral dark' : n.charAt(0).toUpperCase() + n.slice(1));
	const groups = $derived([
		{ label: 'Light themes', list: DAISY_THEMES.filter((t) => t.scheme === 'light') },
		{ label: 'Dark themes', list: DAISY_THEMES.filter((t) => t.scheme === 'dark') }
	]);
</script>

<div class="vstack">
	<fieldset class="m-0 border-0 p-0">
		<legend class="small font-semibold mb-1 p-0">Theme</legend>
		<div class="join w-full" role="radiogroup" aria-label="Our themes">
			{#each own as [v, l, icon] (v)}
				<button type="button" role="radio" aria-checked={prefs.theme === v} class="join-item btn btn-sm flex-1 gap-1" class:btn-primary={prefs.theme === v} onclick={() => prefs.set('theme', v)}><Icon name={icon} size={15} />{l}</button>
			{/each}
		</div>
		<details class="collapse collapse-arrow mt-2 border border-base-300 bg-base-100" open={!own.some(([v]) => v === prefs.theme)}>
			<summary class="collapse-title min-h-0 py-2 text-sm font-semibold">More themes{#if !own.some(([v]) => v === prefs.theme)}<span class="badge badge-soft badge-primary badge-sm ml-2">{title(daisyTheme(prefs.theme)?.name ?? '')}</span>{/if}</summary>
			<div class="collapse-content themes">
				{#each groups as g (g.label)}
					<p class="small muted m-0 mt-1">{g.label}</p>
					<div class="grid">
						{#each g.list as t (t.name)}
							<label class="tile" class:on={prefs.theme === themeValue(t.name)} style:background={t.base} style:color={t.text}>
								<input type="radio" class="sr-only" name="{idPrefix}-theme" value={themeValue(t.name)} checked={prefs.theme === themeValue(t.name)} onchange={() => prefs.set('theme', themeValue(t.name))} />
								<span class="dots" aria-hidden="true"><i style:background={t.primary}></i><i style:background={t.secondary}></i><i style:background={t.accent}></i></span>
								<span class="truncate">{title(t.name)}</span>
							</label>
						{/each}
					</div>
				{/each}
				<p class="small muted m-0 mt-2">Colours in these themes are adjusted where needed so text stays readable (WCAG AA).</p>
			</div>
		</details>
	</fieldset>

	<fieldset class="m-0 border-0 p-0">
		<legend class="small font-semibold mb-1 p-0">Main colour</legend>
		<div class="flex flex-wrap items-center gap-1.5">
			<span class="tooltip" data-tip="The theme's own colour">
				<button type="button" class="swatch none" aria-label="Theme colour" aria-pressed={!prefs.color} onclick={() => prefs.set('color', '')}><Icon name="x" size={12} /></button>
			</span>
			{#each COLORS as [c, name] (c)}
				<span class="tooltip" data-tip={name}>
					<button type="button" class="swatch" style:background={c} aria-label={name} aria-pressed={prefs.color.toLowerCase() === c} onclick={() => prefs.set('color', c)}></button>
				</span>
			{/each}
			<label class="tooltip m-0" data-tip="Pick any colour">
				<span class="sr-only">Pick any colour</span>
				<input type="color" class="picker" value={prefs.color || '#1f5aa6'} onchange={(e) => prefs.set('color', e.currentTarget.value)} />
			</label>
		</div>
		{#if prefs.color && prefs.colorAdjusted}<p class="small muted m-0 mt-1 flex items-center gap-1"><Icon name="info" size={13} />Adjusted a little to stay readable on this theme.</p>{/if}
	</fieldset>

	<label class="flex items-center justify-between gap-3 font-normal m-0">
		<span><span class="small font-semibold block">Always show button labels</span><span class="small muted">Otherwise they appear when you point at or tab to an icon.</span></span>
		<input type="checkbox" class="toggle toggle-sm toggle-primary" checked={prefs.labels === 'always'} onchange={(e) => prefs.set('labels', e.currentTarget.checked ? 'always' : 'hover')} />
	</label>

	<div role="radiogroup" aria-labelledby="{idPrefix}-text">
		<p id="{idPrefix}-text" class="small font-semibold m-0 mb-1">Text size</p>
		<div class="join w-full">
			{#each sizes as [v, l, name] (v)}
				<button type="button" role="radio" aria-checked={prefs.text === v} aria-label="{name} text" class="join-item btn btn-sm flex-1" class:btn-primary={prefs.text === v} onclick={() => prefs.set('text', v)}>{l}</button>
			{/each}
		</div>
	</div>
	<label class="flex items-center justify-between gap-3 font-normal m-0">
		<span class="small font-semibold">Reduce motion</span>
		<input type="checkbox" class="toggle toggle-sm toggle-primary" checked={prefs.motion === 'reduce'} onchange={(e) => prefs.set('motion', e.currentTarget.checked ? 'reduce' : 'system')} />
	</label>
	<button type="button" class="btn btn-ghost btn-xs w-fit" onclick={() => prefs.reset()}><Icon name="undo" size={13} />Reset display settings</button>
</div>

<style>
	.themes { max-height: 16rem; overflow-y: auto; }
	.grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.3rem; margin-top: 0.25rem; }
	.tile { display: flex; align-items: center; gap: 0.4rem; margin: 0; padding: 0.35rem 0.5rem; border-radius: var(--radius-field); border: 1.5px solid color-mix(in oklab, currentColor 20%, transparent); font-size: 0.8rem; font-weight: 600; cursor: pointer; }
	.tile.on { outline: 2.5px solid var(--color-primary); outline-offset: 1px; }
	.tile:has(input:focus-visible) { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.dots { display: inline-flex; gap: 2px; flex: none; }
	.dots i { width: 0.55rem; height: 0.9rem; border-radius: 0.2rem; }
	.swatch { width: 1.6rem; height: 1.6rem; border-radius: 999px; border: 2px solid var(--color-base-100); box-shadow: 0 0 0 1px var(--color-base-300); cursor: pointer; display: grid; place-items: center; }
	.swatch[aria-pressed='true'] { box-shadow: 0 0 0 2.5px var(--color-base-content); }
	.swatch.none { background: var(--color-base-200); color: var(--color-muted); }
	.picker { width: 1.9rem; height: 1.9rem; padding: 0; border: 0; background: none; cursor: pointer; }
</style>
