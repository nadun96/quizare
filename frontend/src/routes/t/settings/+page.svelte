<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	import { confirmDialog } from '$lib/ui/dialog.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
	import DriveCard from '$lib/drive/DriveCard.svelte';
	import type { Effective, Overrides } from '$lib/types';

	type Key = { id: string; provider: string; label: string; model: string; last4: string; is_default: boolean; last_test_at: string | null; last_test_ok: boolean | null; last_test_error?: string };
	const ready = requireRole('teacher');
	let overrides = $state<Overrides>({});
	let effective = $state<Effective>({});
	let keys = $state<Key[]>([]);
	let errors = $state<Record<string, string>>({});
	let form = $state({ provider: 'anthropic', model: '', label: '', api_key: '' });
	let consent = $state(false);
	let keyError = $state('');
	// The school's own server (D-115): no key, its models only.
	type Local = { enabled: boolean; models: string[]; default_model: string; for_everyone: boolean };
	let local = $state<Local>({ enabled: false, models: [], default_model: '', for_everyone: false });
	const isLocal = $derived(form.provider === 'ollama');
	const PROVIDER_NAMES: Record<string, string> = { anthropic: 'Anthropic', openai: 'OpenAI', google: 'Google', ollama: "this school's server" };

	async function load() {
		const s = await api.get<{ overrides: Overrides; effective: Effective }>('/api/teacher/settings');
		overrides = s.overrides;
		effective = s.effective;
		keys = (await api.get<{ keys: Key[] }>('/api/teacher/llm-keys')).keys ?? [];
		local = await api.get<Local>('/api/teacher/llm-local').catch(() => local);
	}
	$effect(() => {
		if (ready()) load();
	});
	async function save(o: Overrides) {
		errors = {};
		try {
			await api.put('/api/teacher/settings', o);
			await load();
		} catch (e) {
			if (e instanceof ApiError) errors = e.fields;
			throw e;
		}
	}
	async function addKey(e: SubmitEvent) {
		e.preventDefault();
		keyError = '';
		try {
			await api.post('/api/teacher/llm-keys', isLocal ? { provider: 'ollama', model: form.model || local.default_model, label: form.label } : form);
			form = { provider: form.provider, model: '', label: '', api_key: '' }; // never keep the key in memory
			load();
		} catch (err) {
			keyError = err instanceof ApiError ? Object.values(err.fields).join('; ') || err.message : 'Could not save the key';
		}
	}
	async function test(k: Key) {
		await api.post('/api/teacher/llm-keys/' + k.id + '/test');
		load();
	}
	async function makeDefault(k: Key) {
		await api.patch('/api/teacher/llm-keys/' + k.id, { is_default: true });
		load();
	}
	async function del(k: Key) {
if (await confirmDialog({ title: tr("Delete this API key?"), body: tr("Quizzes that use it fall back to your default key, or to manual marking."), confirm: tr("Delete key"), danger: true })) {
			await api.del('/api/teacher/llm-keys/' + k.id);
			load();
		}
	}
	const placeholder: Record<string, string> = { anthropic: 'claude-opus-5-5 (default)', openai: 'e.g. the model name from your OpenAI account', google: 'e.g. a Gemini model name' };
</script>

<div class="page-container vstack" style="max-width:900px">
	<h1>{tr("Settings")}</h1>
	<h2>{tr("My defaults")}</h2>
	<p class="small muted">{tr("These apply to all your classrooms and quizzes unless a lower level overrides them.")}</p>
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6"><SettingsEditor level="teacher" value={overrides} {effective} onsave={save} {errors} /></div>

	<DriveCard />
	<h2>{tr("LLM API keys")}</h2>
	<p class="small">{tr("Keys are encrypted and can never be shown again after saving: only the last 4 characters appear here. They are used only for marking and feedback on your own quizzes.")}</p>
	{#if local.enabled}
		<p class="alert alert-info alert-soft small m-0"><span>{tr("Your school runs its own AI models, so answers marked with them never leave the school.")}{#if local.for_everyone && !keys.length} {tr("Until you add a key, your AI marking uses")} <strong>{local.default_model}</strong> {tr("on the school's server.")}{/if}</span></p>
	{/if}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 table-wrap">
		<table class="table"><thead><tr><th>{tr("Provider")}</th><th>{tr("Model")}</th><th>{tr("Key")}</th><th>{tr("Status")}</th><th></th></tr></thead><tbody>
			{#each keys as k (k.id)}
				<tr>
					<td>{k.provider === 'ollama' ? tr("School server") : k.provider}{k.label ? ` · ${k.label}` : ''} {#if k.is_default}<span class="badge badge-soft badge-success">{tr("default")}</span>{/if}</td>
					<td class="small">{k.model}</td>
					<td>{#if k.provider === 'ollama'}<span class="small muted">{tr("no key needed")}</span>{:else}<code>••••{k.last4}</code>{/if}</td>
					<td class="small">{#if k.last_test_ok === true}<span class="badge badge-soft badge-success">{tr("works")}</span>{:else if k.last_test_ok === false}<span class="badge badge-soft badge-error" title={k.last_test_error}>{tr("failed")}</span> {k.last_test_error}{:else}{tr("untested")}{/if}</td>
					<td class="row"><button class="btn btn-sm" onclick={() => test(k)}>{tr("Test")}</button>{#if !k.is_default}<button class="btn btn-sm" onclick={() => makeDefault(k)}>{tr("Make default")}</button>{/if}<button class="btn btn-sm btn-error btn-outline" onclick={() => del(k)}>{tr("Delete")}</button></td>
				</tr>
			{:else}<tr><td colspan="5" class="muted">{local.enabled && local.for_everyone ? tr("No keys of your own: AI marking uses {0} on the school's server.", [local.default_model]) : tr("No keys. Without a key, essay answers are marked by you (manual marking).")}</td></tr>{/each}
		</tbody></table>
	</div>
	<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={addKey}>
		<strong>{tr("Add a key")}</strong>
		<div class="auto-grid">
			<div><label for="prov">{tr("Provider")}</label><select class="select w-full" id="prov" bind:value={form.provider}><option value="anthropic">{tr("Anthropic")}</option><option value="openai">{tr("OpenAI")}</option><option value="google">{tr("Google Gemini")}</option>{#if local.enabled}<option value="ollama">{tr("This school's server (Ollama)")}</option>{/if}</select></div>
			{#if isLocal}
				<div><label for="model">{tr("Model")}</label><select class="select w-full" id="model" bind:value={form.model}>
					<option value="">{local.default_model ? tr("{0} (the school's default)", [local.default_model]) : tr("Choose…")}</option>
					{#each local.models as m (m)}<option value={m}>{m}</option>{/each}
				</select></div>
			{:else}
				<div><label for="model">{tr("Model")}</label><input class="input w-full" id="model" bind:value={form.model} placeholder={placeholder[form.provider]} required={form.provider !== 'anthropic'} /></div>
			{/if}
			<div><label for="label">{tr("Label (optional)")}</label><input class="input w-full" id="label" bind:value={form.label} /></div>
		</div>
		{#if !isLocal}<div><label for="key">{tr("API key")}</label><input class="input w-full" id="key" type="password" autocomplete="off" bind:value={form.api_key} required /></div>{/if}
		<label class="row" style="font-weight:400;align-items:flex-start">
			<input class="checkbox" type="checkbox" bind:checked={consent} style="margin-top:0.3rem" />
			{#if isLocal}
				<span class="small">{tr("I understand that when AI marking or feedback is on, students' answers (without names, emails or student IDs) are marked by a model on the school's own server, and that AI marks can be wrong and I check them. Students are told when a quiz uses AI marking.")}</span>
			{:else}
				<span class="small">{tr("I understand that when AI marking or feedback is on, students' answers (without names, emails or student IDs) are sent to {0} under my account, and that the provider's own retention terms apply. Students are told when a quiz uses AI marking.", [PROVIDER_NAMES[form.provider]])}</span>
			{/if}
		</label>
		{#if keyError}<p class="alert alert-soft alert-error">{keyError}</p>{/if}
		<button class="btn btn-primary" disabled={!consent || (isLocal && !form.model && !local.default_model)}>{isLocal ? tr("Use this model") : tr("Save key")}</button>
	</form>
</div>
