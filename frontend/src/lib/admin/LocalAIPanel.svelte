<script lang="ts">
	import { localeTag, tr } from '$lib/i18n/i18n.svelte';
	// Local AI models (D-115), admins only: the school's Ollama server, which
	// of its models teachers may use, and whether teachers without a key of
	// their own mark with one by default. Answers marked this way never
	// leave the school.
	import { api, ApiError } from '../api';
	import { fmtBytes } from '../staff';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';

	type Installed = { name: string; size: number; family?: string; parameters?: string; modified_at: string };
	type State = { enabled: boolean; base_url: string; models: string[]; default_model: string; for_everyone: boolean; updated_at?: string; reachable: boolean; error?: string; installed: Installed[] };
	type Test = { ok: boolean; error?: string; seconds: number; score: number };

	let st = $state<State | null>(null);
	let f = $state({ enabled: false, base_url: 'http://127.0.0.1:11434', models: [] as string[], default_model: '', for_everyone: false });
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);
	let tests = $state<Record<string, Test | 'running'>>({});

	async function load() {
		st = await api.get<State>('/api/admin/ai/local');
		f = { enabled: st.enabled, base_url: st.base_url || 'http://127.0.0.1:11434', models: [...st.models], default_model: st.default_model, for_everyone: st.for_everyone };
	}
	load();

	// An empty list means "every installed model".
	const open = $derived(f.models.length === 0);
	const choosable = $derived(open ? (st?.installed ?? []).map((m) => m.name) : f.models);
	function toggle(name: string, on: boolean) {
		if (open && !on) f.models = (st?.installed ?? []).map((m) => m.name).filter((n) => n !== name);
		else f.models = on ? [...f.models, name] : f.models.filter((n) => n !== name);
		if (f.default_model && !f.models.includes(f.default_model) && f.models.length) f.default_model = '';
	}
	const allowed = (name: string) => open || f.models.includes(name);

	async function save(e?: SubmitEvent) {
		e?.preventDefault();
		busy = true;
		errors = {};
		try {
			st = await api.put<State>('/api/admin/ai/local', f);
			f = { enabled: st.enabled, base_url: st.base_url, models: [...st.models], default_model: st.default_model, for_everyone: st.for_everyone };
			toast(st.reachable ? tr("Saved") : tr("Saved, but the server didn’t answer: see below"), st.reachable ? 'success' : 'warning');
		} catch (err) {
			if (err instanceof ApiError) {
				errors = err.fields;
				toast(Object.values(err.fields)[0] ?? err.message, 'error');
			} else toast(tr("Could not save it"), 'error');
		} finally {
			busy = false;
		}
	}
	async function test(model: string) {
		tests = { ...tests, [model]: 'running' };
		try {
			tests = { ...tests, [model]: await api.post<Test>('/api/admin/ai/local/test', { model }) };
		} catch (err) {
			tests = { ...tests, [model]: { ok: false, error: err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Failed', seconds: 0, score: 0 } };
		}
	}
</script>

<div class="vstack">
	<div class="alert alert-info alert-soft">
		<Icon name="info" size={18} />
		<div class="small">
			<p class="m-0">{tr("Run AI marking on your own server with")} <a href="https://ollama.com" target="_blank" rel="noopener">{tr("Ollama")}</a>{tr(": students' answers stay in the school, and there's no bill per answer.")}</p>
			<p class="m-0 mt-1">{tr("On the server: install Ollama, then pull a model, for example")} <code>ollama pull llama3.2:3b</code> {tr("(small, fast) or")} <code>ollama pull qwen2.5:7b</code> {tr("(better marking; wants about 8 GB of free memory). Ollama listens on")} <code>http://127.0.0.1:11434</code>.</p>
		</div>
	</div>

	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={save} aria-labelledby="ai-h">
		<div class="flex flex-wrap items-center gap-2">
			<h2 id="ai-h" class="m-0 grow text-lg">{tr("The school's AI server")}</h2>
			<label class="flex items-center gap-2 font-semibold"><input type="checkbox" class="toggle toggle-primary" bind:checked={f.enabled} />{tr("Offer to teachers")}</label>
		</div>
		<div class="flex flex-wrap items-end gap-2">
			<div class="grow">
				<label for="ai-url" class="small font-semibold">{tr("Ollama address")}</label>
				<input id="ai-url" class="input w-full" class:input-error={errors.base_url} bind:value={f.base_url} placeholder={tr("http://127.0.0.1:11434")} />
				{#if errors.base_url}<p class="m-0 small text-error">{errors.base_url}</p>{/if}
			</div>
			<button class="btn" disabled={busy}>{tr("Save and check")}</button>
		</div>
		{#if st}
			{#if st.error}
				<p class="alert alert-error alert-soft m-0 small"><Icon name="alert" size={16} />{st.error}</p>
			{:else if st.reachable}
				<p class="m-0 small text-success"><Icon name="check" size={14} /> {tr("Ollama answers: {0} {1} installed.", [st.installed.length, st.installed.length === 1 ? 'model' : 'models'])}</p>
			{/if}
		{/if}

		{#if st?.installed.length}
			<fieldset class="vstack gap-2">
				<legend class="small font-semibold">{tr("Models teachers may use")}</legend>
				<p class="m-0 small muted">{tr("{0} Test one to see how long a mark takes on this server.", [open ? tr("All of them (none ticked off). Untick any you don’t want teachers to choose.") : tr("Only the ticked ones.")])}</p>
				<div class="table-wrap">
					<table class="table table-sm">
						<thead><tr><th>{tr("Model")}</th><th>{tr("Size")}</th><th>{tr("Teachers")}</th><th>{tr("Default")}</th><th>{tr("Test")}</th></tr></thead>
						<tbody>
							{#each st.installed as m (m.name)}
								{@const t = tests[m.name]}
								<tr>
									<td><span class="font-semibold">{m.name}</span>{#if m.parameters || m.family}<br /><span class="small muted">{[m.family, m.parameters].filter(Boolean).join(' · ')}</span>{/if}</td>
									<td class="tabular small">{fmtBytes(m.size)}</td>
									<td><input type="checkbox" class="checkbox checkbox-sm" aria-label={tr("Teachers may use {0}", [m.name])} checked={allowed(m.name)} onchange={(e) => toggle(m.name, e.currentTarget.checked)} /></td>
									<td><input type="radio" class="radio radio-sm" name="ai-default" aria-label={tr("{0} is the default", [m.name])} checked={f.default_model === m.name} disabled={!allowed(m.name)} onchange={() => (f.default_model = m.name)} /></td>
									<td class="small">
										{#if t === 'running'}<span class="loading loading-spinner loading-xs"></span> {tr("marking…")}
										{:else if t}<span class={t.ok ? 'text-success' : 'text-error'}>{t.ok ? tr("Works · {0}s", [t.seconds]) : t.error}</span>
										{:else}<button type="button" class="btn btn-xs" onclick={() => test(m.name)} disabled={!st.base_url}>{tr("Test")}</button>{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				{#if errors.default_model}<p class="m-0 small text-error">{errors.default_model}</p>{/if}
			</fieldset>
			<label class="flex items-start gap-2">
				<input type="checkbox" class="checkbox checkbox-sm mt-0.5" bind:checked={f.for_everyone} />
				<span><span class="font-semibold">{tr("Teachers without a key of their own mark with the default")}</span><br /><span class="small muted">{tr("AI marking then works for every teacher with no set-up. A teacher's own key, when they add one, comes first.")}</span></span>
			</label>
			<div><button class="btn btn-primary" disabled={busy}>{tr("Save")}</button></div>
		{:else if st?.reachable}
			<p class="m-0 small muted">{tr("No models yet. On the server, run")} <code>ollama pull llama3.2:3b</code>{tr(", then Save and check again.")}</p>
		{/if}
		{#if st?.updated_at}<p class="m-0 small muted">{tr("Last changed {0}.", [new Date(st.updated_at).toLocaleString(localeTag())])}</p>{/if}
	</form>
	{#if choosable.length && f.enabled}
		<p class="m-0 small muted">{tr("Teachers find these under Settings → LLM API keys → \"This school's server\".")}</p>
	{/if}
</div>
