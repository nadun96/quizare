<script lang="ts">
	// Edits the sparse overrides of one configuration level (BA §7). Unset keys
	// inherit, and the inherited value is shown as the default choice, so a
	// teacher sees what applies without reading "Inherit (…)" everywhere.
	// Settings come in sections; a closed section says in one line what
	// applies, so a teacher opens only the one they want to change (D-63).
	import { describe, forLevel, SECTIONS, type Level, type Meta } from './settingsMeta';
	import type { Effective, Overrides } from './types';
	import Icon from './ui/Icon.svelte';
	import OptionGroup from './ui/OptionGroup.svelte';

	let {
		level,
		value = {},
		effective = {},
		onsave,
		errors = {}
	}: { level: Level; value?: Overrides; effective?: Effective; onsave: (o: Overrides) => Promise<void> | void; errors?: Record<string, string> } = $props();

	let draft = $state<Overrides>({});
	let saving = $state(false);
	let saved = $state(false);
	$effect.pre(() => {
		draft = { ...value };
	});
	const metas = $derived(forLevel(level));
	const sections = $derived(SECTIONS.map((s) => ({ ...s, metas: metas.filter((m) => m.section === s.id) })).filter((s) => s.metas.length));
	// A short list is shown open; a long one starts closed with its summaries.
	const openAll = $derived(metas.length <= 6);
	const dirty = $derived(JSON.stringify(sorted(draft)) !== JSON.stringify(sorted(value)));

	function sorted(o: Overrides) {
		return Object.fromEntries(Object.entries(o).sort(([a], [b]) => a.localeCompare(b)));
	}
	function set(key: string, v: string | number | boolean | undefined) {
		const d = { ...draft };
		if (v === undefined || v === '') delete d[key];
		else d[key] = v;
		draft = d;
		saved = false;
	}
	const applies = (m: Meta) => draft[m.key] ?? effective[m.key];
	const get = (key: string) => draft[key] ?? effective[key];
	const relevant = (m: Meta) => !m.when || m.when(get);
	const changedIn = (ms: Meta[]) => ms.filter((m) => draft[m.key] !== undefined).length;
	const summary = (ms: Meta[]) =>
		ms.filter(relevant).map((m) => [m.short, describe(m, applies(m))]).filter(([, v]) => v).map(([k, v]) => `${k}: ${v}`).join('; ');
	const hasError = (ms: Meta[]) => ms.some((m) => errors['settings.' + m.key] || errors[m.key]);
	const boolOptions = (m: Meta) => [
		{ v: 'true', label: m.on ?? 'On', icon: 'check' as const },
		{ v: 'false', label: m.off ?? 'Off', icon: 'x' as const }
	];
	const fallbackOf = (m: Meta) => {
		const e = effective[m.key];
		return e === undefined ? undefined : String(e);
	};

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		try {
			await onsave(draft);
			saved = true;
		} finally {
			saving = false;
		}
	}
</script>

<form class="vstack" onsubmit={save}>
	<div class="sections">
		{#each sections as s (s.id)}
			{@const changed = changedIn(s.metas)}
			<details class="section" open={openAll || hasError(s.metas)}>
				<summary>
					<span class="s-icon" aria-hidden="true"><Icon name={s.icon} size={17} /></span>
					<span class="min-w-0 flex-1">
						<span class="s-title">{s.title}</span>
						<span class="s-sum">{summary(s.metas) || 'Uses the values from above'}</span>
					</span>
					{#if changed}<span class="badge badge-soft badge-primary badge-sm whitespace-nowrap">{changed} set here</span>{/if}
					<span class="chev" aria-hidden="true"><Icon name="chevron-right" size={16} /></span>
				</summary>
				<div class="rows">
					{#each s.metas.filter(relevant) as m (m.key)}
						{@const own = draft[m.key] !== undefined}
						{@const err = errors['settings.' + m.key] ?? errors[m.key]}
						<div class="srow">
							<div class="slabel">
								<span class="font-semibold" id={'l-' + m.key}>{m.label}</span>
								{#if m.help}<span class="small muted">{m.help}</span>{/if}
							</div>
							<div class="scontrol">
								{#if m.kind === 'enum'}
									<OptionGroup label={m.label} options={m.options ?? []} value={draft[m.key] as string | undefined} fallback={fallbackOf(m)} onchange={(v) => set(m.key, v)} />
								{:else if m.kind === 'bool'}
									<OptionGroup label={m.label} options={boolOptions(m)} value={draft[m.key] === undefined ? undefined : String(draft[m.key])} fallback={fallbackOf(m)} onchange={(v) => set(m.key, v === 'true')} />
								{:else if m.kind === 'int'}
									<label class="input input-sm w-36" aria-labelledby={'l-' + m.key}>
										<input type="number" min="0" inputmode="numeric" placeholder={effective[m.key] !== undefined ? String(effective[m.key]) : ''} value={draft[m.key] ?? ''}
											oninput={(e) => set(m.key, e.currentTarget.value === '' ? undefined : Number(e.currentTarget.value))} aria-labelledby={'l-' + m.key} />
										{#if m.unit}<span class="muted">{m.unit}</span>{/if}
									</label>
									{#if !own && effective[m.key] !== undefined}<span class="small muted">Default: {describe(m, effective[m.key])}</span>{/if}
								{:else}
									<input class="input input-sm w-full max-w-xs" aria-labelledby={'l-' + m.key} placeholder={effective[m.key] ? String(effective[m.key]) : 'Default'} value={draft[m.key] ?? ''} oninput={(e) => set(m.key, e.currentTarget.value || undefined)} />
								{/if}
								<!-- `effective` already includes this level's own value, so the default it would fall back to isn't known here. -->
								{#if own}<button type="button" class="link link-hover small reset" onclick={() => set(m.key, undefined)}>Use the default</button>{/if}
								{#if err}<p class="field-error m-0">{err}</p>{/if}
							</div>
						</div>
					{/each}
				</div>
			</details>
		{/each}
	</div>
	<div class="row">
		<button class="btn btn-primary" disabled={saving || !dirty}>Save settings</button>
		{#if dirty}<span class="small muted">Not saved yet</span>{:else if saved}<span class="badge badge-soft badge-success">Saved</span>{/if}
	</div>
</form>

<style>
	.sections { display: grid; gap: 0.5rem; }
	.section { border: 1px solid var(--color-base-300); border-radius: var(--radius-box); background: var(--color-base-100); }
	.section > summary { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem 0.9rem; cursor: pointer; list-style: none; border-radius: var(--radius-box); }
	.section > summary::-webkit-details-marker { display: none; }
	.section > summary:hover { background: var(--color-base-200); }
	.section > summary:focus-visible { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.s-icon { width: 2rem; height: 2rem; flex: none; display: grid; place-items: center; border-radius: 0.55rem; background: color-mix(in oklab, var(--color-primary) 12%, var(--color-base-100)); color: var(--color-primary); }
	.s-title { display: block; font-weight: 650; }
	.s-sum { display: block; font-size: 0.825rem; color: var(--color-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.chev { color: var(--color-muted); transition: transform var(--motion-fast); }
	.section[open] .chev { transform: rotate(90deg); }
	.section[open] .s-sum { display: none; }
	.rows { display: grid; padding: 0 0.9rem 0.4rem; }
	.srow { display: grid; grid-template-columns: minmax(0, 15rem) minmax(0, 1fr); gap: 0.5rem 1.25rem; align-items: start; padding: 0.8rem 0; border-top: 1px solid var(--color-base-300); }
	@media (max-width: 40rem) { .srow { grid-template-columns: minmax(0, 1fr); } }
	.slabel { display: grid; gap: 0.15rem; padding-top: 0.4rem; }
	.scontrol { display: grid; gap: 0.35rem; justify-items: start; min-width: 0; }
	.reset { background: none; border: 0; padding: 0; color: var(--color-primary); cursor: pointer; }
</style>
