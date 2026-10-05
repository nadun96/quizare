<script lang="ts">
	// Edits the sparse overrides of one configuration level. Unset keys
	// inherit; the inherited value is shown so teachers see what applies (BA §7).
	import { forLevel, type Level } from './settingsMeta';
	import type { Effective, Overrides } from './types';

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

	function set(key: string, v: string | number | boolean | undefined) {
		const d = { ...draft };
		if (v === undefined || v === '') delete d[key];
		else d[key] = v;
		draft = d;
		saved = false;
	}
	function inherited(key: string) {
		const v = effective[key];
		if (v === undefined) return 'default';
		if (typeof v === 'boolean') return v ? 'on' : 'off';
		return String(v);
	}
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
	<div class="settings">
		{#each metas as m (m.key)}
			<div class="setting">
				<label for={'s-' + m.key}>{m.label}{m.unit ? ` (${m.unit})` : ''}</label>
				{#if m.kind === 'enum'}
					<select class="select w-full" id={'s-' + m.key} value={draft[m.key] ?? ''} onchange={(e) => set(m.key, e.currentTarget.value || undefined)}>
						<option value="">Inherit ({m.options?.find((o) => o[0] === effective[m.key])?.[1] ?? inherited(m.key)})</option>
						{#each m.options ?? [] as [v, l] (v)}<option value={v}>{l}</option>{/each}
					</select>
				{:else if m.kind === 'bool'}
					<select class="select w-full" id={'s-' + m.key} value={draft[m.key] === undefined ? '' : String(draft[m.key])}
						onchange={(e) => set(m.key, e.currentTarget.value === '' ? undefined : e.currentTarget.value === 'true')}>
						<option value="">Inherit ({inherited(m.key)})</option>
						<option value="true">On</option>
						<option value="false">Off</option>
					</select>
				{:else if m.kind === 'int'}
					<input class="input w-full" id={'s-' + m.key} type="number" min="0" inputmode="numeric" placeholder={'Inherit: ' + inherited(m.key)}
						value={draft[m.key] ?? ''} oninput={(e) => set(m.key, e.currentTarget.value === '' ? undefined : Number(e.currentTarget.value))} />
				{:else}
					<input class="input w-full" id={'s-' + m.key} placeholder={'Inherit: ' + inherited(m.key)} value={draft[m.key] ?? ''} oninput={(e) => set(m.key, e.currentTarget.value || undefined)} />
				{/if}
				{#if m.help}<p class="small muted">{m.help}</p>{/if}
				{#if errors['settings.' + m.key] || errors[m.key]}<p class="field-error">{errors['settings.' + m.key] ?? errors[m.key]}</p>{/if}
			</div>
		{/each}
	</div>
	<div class="row">
		<button class="btn btn-primary" disabled={saving}>Save settings</button>
		{#if saved}<span class="badge badge-soft badge-success">Saved</span>{/if}
	</div>
</form>

<style>
	.settings { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); }
</style>
