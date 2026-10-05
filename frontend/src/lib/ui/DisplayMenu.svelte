<script lang="ts">
	// Theme, text size and motion. The research found that letting students
	// tune the screen reduces stress; also WCAG 1.4.4 (resize) and 2.3.3 (motion).
	import Icon, { type IconName } from './Icon.svelte';
	import { prefs, type TextSize, type Theme } from './prefs.svelte';

	let { idPrefix = 'disp' }: { idPrefix?: string } = $props();
	const themes: [Theme, string, IconName][] = [['system', 'System', 'monitor'], ['light', 'Light', 'sun'], ['dark', 'Dark', 'moon']];
	const sizes: [TextSize, string, string][] = [['md', 'A', 'Normal'], ['lg', 'A+', 'Larger'], ['xl', 'A++', 'Largest']];
</script>

<div class="vstack">
	<div role="radiogroup" aria-labelledby="{idPrefix}-theme">
		<p id="{idPrefix}-theme" class="small font-semibold m-0 mb-1">Theme</p>
		<div class="join w-full">
			{#each themes as [v, l, icon] (v)}
				<button type="button" role="radio" aria-checked={prefs.theme === v} class="join-item btn btn-sm flex-1 gap-1" class:btn-primary={prefs.theme === v} onclick={() => prefs.set('theme', v)}>
					<Icon name={icon} size={15} />{l}
				</button>
			{/each}
		</div>
	</div>
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
</div>
