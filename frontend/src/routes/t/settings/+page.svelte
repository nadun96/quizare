<script lang="ts">
	import { api, ApiError } from '$lib/api';
	import { requireRole } from '$lib/guard.svelte';
	import SettingsEditor from '$lib/SettingsEditor.svelte';
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

	async function load() {
		const s = await api.get<{ overrides: Overrides; effective: Effective }>('/api/teacher/settings');
		overrides = s.overrides;
		effective = s.effective;
		keys = (await api.get<{ keys: Key[] }>('/api/teacher/llm-keys')).keys ?? [];
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
			await api.post('/api/teacher/llm-keys', form);
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
		if (confirm('Delete this API key?')) {
			await api.del('/api/teacher/llm-keys/' + k.id);
			load();
		}
	}
	const placeholder: Record<string, string> = { anthropic: 'claude-opus-5-5 (default)', openai: 'e.g. the model name from your OpenAI account', google: 'e.g. a Gemini model name' };
</script>

<div class="container stack" style="max-width:900px">
	<h1>Settings</h1>
	<h2>My defaults</h2>
	<p class="small muted">These apply to all your classrooms and quizzes unless a lower level overrides them.</p>
	<div class="card"><SettingsEditor level="teacher" value={overrides} {effective} onsave={save} {errors} /></div>

	<h2>LLM API keys</h2>
	<p class="small">Keys are encrypted and can never be shown again after saving: only the last 4 characters appear here. They are used only for marking and feedback on your own quizzes.</p>
	<div class="card table-wrap">
		<table><thead><tr><th>Provider</th><th>Model</th><th>Key</th><th>Status</th><th></th></tr></thead><tbody>
			{#each keys as k (k.id)}
				<tr>
					<td>{k.provider}{k.label ? ` · ${k.label}` : ''} {#if k.is_default}<span class="badge ok">default</span>{/if}</td>
					<td class="small">{k.model}</td>
					<td><code>••••{k.last4}</code></td>
					<td class="small">{#if k.last_test_ok === true}<span class="badge ok">works</span>{:else if k.last_test_ok === false}<span class="badge danger" title={k.last_test_error}>failed</span> {k.last_test_error}{:else}untested{/if}</td>
					<td class="row"><button class="small" onclick={() => test(k)}>Test</button>{#if !k.is_default}<button class="small" onclick={() => makeDefault(k)}>Make default</button>{/if}<button class="small danger" onclick={() => del(k)}>Delete</button></td>
				</tr>
			{:else}<tr><td colspan="5" class="muted">No keys. Without a key, essay answers are marked by you (manual marking).</td></tr>{/each}
		</tbody></table>
	</div>
	<form class="card stack" onsubmit={addKey}>
		<strong>Add a key</strong>
		<div class="grid">
			<div><label for="prov">Provider</label><select id="prov" bind:value={form.provider}><option value="anthropic">Anthropic</option><option value="openai">OpenAI</option><option value="google">Google Gemini</option></select></div>
			<div><label for="model">Model</label><input id="model" bind:value={form.model} placeholder={placeholder[form.provider]} required={form.provider !== 'anthropic'} /></div>
			<div><label for="label">Label (optional)</label><input id="label" bind:value={form.label} /></div>
		</div>
		<div><label for="key">API key</label><input id="key" type="password" autocomplete="off" bind:value={form.api_key} required /></div>
		<label class="row" style="font-weight:400;align-items:flex-start">
			<input type="checkbox" bind:checked={consent} style="margin-top:0.3rem" />
			<span class="small">I understand that when AI marking or feedback is on, students' answers (without names, emails or student IDs) are sent to {form.provider === 'openai' ? 'OpenAI' : form.provider === 'google' ? 'Google' : 'Anthropic'} under my account, and that the provider's own retention terms apply. Students are told when a quiz uses AI marking.</span>
		</label>
		{#if keyError}<p class="alert danger">{keyError}</p>{/if}
		<button class="primary" disabled={!consent}>Save key</button>
	</form>
</div>
