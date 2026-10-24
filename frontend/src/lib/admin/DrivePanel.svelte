<script lang="ts">
	// The Google Cloud app teachers connect their Drive through (D-71).
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';

	type Config = { enabled: boolean; client_id: string; client_secret?: string; client_secret_set?: boolean; api_key: string; app_id: string };
	let c = $state<Config | null>(null);
	let redirect = $state('');
	let origin = $state('');
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);

	$effect(() => {
		api.get<{ config: Config; redirect_url: string; origin: string }>('/api/admin/drive').then((r) => {
			c = { ...r.config, client_secret: '' };
			redirect = r.redirect_url;
			origin = r.origin;
		});
	});

	async function save(enabled = c?.enabled ?? false) {
		if (!c) return;
		saving = true;
		errors = {};
		try {
			const saved = await api.put<Config>('/api/admin/drive', { ...c, enabled });
			c = { ...saved, client_secret: '' };
			toast(enabled ? 'Teachers can connect Google Drive' : 'Settings saved');
		} catch (e) {
			if (e instanceof ApiError) {
				errors = e.fields;
				toast(Object.keys(e.fields).length ? 'Check the highlighted settings' : e.message, 'error');
			}
		} finally {
			saving = false;
		}
	}
	async function copy(v: string) {
		try {
			await navigator.clipboard.writeText(v);
			toast('Copied');
		} catch {
			toast(v);
		}
	}
</script>

{#snippet field(key: 'client_id' | 'api_key' | 'app_id', label: string, hint: string, placeholder: string)}
	<div class="srow">
		<div class="slabel"><label class="font-semibold" for={'g-' + key}>{label}</label><span class="small muted">{hint}</span></div>
		<div class="scontrol">
			<input class="input input-sm w-full" class:input-error={errors[key]} id={'g-' + key} {placeholder} autocomplete="off" spellcheck="false"
				value={c?.[key] ?? ''} oninput={(e) => c && (c[key] = e.currentTarget.value)} />
			{#if errors[key]}<p class="field-error m-0">{errors[key]}</p>{/if}
		</div>
	</div>
{/snippet}

{#if !c}
	<Skeleton lines={4} />
{:else}
	<div class="vstack">
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<div class="flex flex-wrap items-center gap-3">
				<span class="s-icon" aria-hidden="true"><Icon name="cloud" size={18} /></span>
				<div class="min-w-0 flex-1">
					<p class="m-0 font-semibold">Google Drive is {c.enabled ? 'on' : 'off'}</p>
					<p class="small muted m-0">{c.enabled ? 'Teachers connect their own Drive from their settings. The platform only reaches files they pick or it makes.' : 'Create a Google Cloud app (see below), fill in its details, then turn Drive on.'}</p>
				</div>
				{#if c.enabled}<button class="btn btn-sm" disabled={saving} onclick={() => save(false)}>Turn off</button>
				{:else}<button class="btn btn-sm btn-primary" disabled={saving} onclick={() => save(true)}><Icon name="check" size={15} />Turn on</button>{/if}
			</div>
		</div>

		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<h3 class="b-title"><Icon name="info" size={16} />In the Google Cloud console</h3>
			<ol class="small steps-list">
				<li>Turn on the <strong>Google Drive API</strong> and the <strong>Google Picker API</strong>.</li>
				<li>On the <strong>OAuth consent screen</strong>, add the scope <code>…/auth/drive.file</code> (it needs no Google review).</li>
				<li>Create an <strong>OAuth client ID</strong> of type <em>Web application</em>, with this redirect URI and JavaScript origin:
					<div class="copy"><code>{redirect}</code><button class="btn btn-xs" onclick={() => copy(redirect)}><Icon name="copy" size={13} />Copy</button></div>
					<div class="copy"><code>{origin}</code><button class="btn btn-xs" onclick={() => copy(origin)}><Icon name="copy" size={13} />Copy</button></div>
				</li>
				<li>Create an <strong>API key</strong>, restricted to the Picker API and to this site.</li>
			</ol>
		</div>

		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<h3 class="b-title"><Icon name="link" size={16} />The app</h3>
			{@render field('client_id', 'OAuth client ID', 'Ends in .apps.googleusercontent.com.', '1234567890-abc.apps.googleusercontent.com')}
			<div class="srow">
				<div class="slabel"><label class="font-semibold" for="g-secret">OAuth client secret</label><span class="small muted">{c.client_secret_set ? 'Stored, encrypted. Leave empty to keep it.' : 'Stored encrypted with the master key.'}</span></div>
				<div class="scontrol">
					<input class="input input-sm w-full max-w-md" class:input-error={errors.client_secret} id="g-secret" type="password" autocomplete="new-password" placeholder={c.client_secret_set ? '••••••••' : ''} bind:value={c.client_secret} />
					{#if errors.client_secret}<p class="field-error m-0">{errors.client_secret}</p>{/if}
				</div>
			</div>
			{@render field('api_key', 'API key', 'For Google’s file picker, in teachers’ browsers.', 'AIza…')}
			{@render field('app_id', 'Project number', 'On the Cloud console’s dashboard.', '1234567890')}
			<div class="mt-3"><button class="btn btn-sm btn-primary" disabled={saving} onclick={() => save()}>Save</button></div>
		</div>
	</div>
{/if}

<style>
	.s-icon { width: 2.25rem; height: 2.25rem; display: grid; place-items: center; border-radius: 0.6rem; background: color-mix(in oklab, var(--color-primary) 12%, var(--color-base-100)); color: var(--color-primary); }
	.b-title { display: flex; align-items: center; gap: 0.45rem; margin: 0 0 0.25rem; font-size: 1rem; }
	.b-title :global(svg) { color: var(--color-primary); }
	.srow { display: grid; grid-template-columns: minmax(0, 15rem) minmax(0, 1fr); gap: 0.5rem 1.25rem; align-items: start; padding: 0.75rem 0; border-top: 1px solid var(--color-base-300); }
	@media (max-width: 40rem) { .srow { grid-template-columns: minmax(0, 1fr); } }
	.slabel { display: grid; gap: 0.15rem; padding-top: 0.3rem; }
	.scontrol { display: grid; gap: 0.35rem; justify-items: stretch; min-width: 0; }
	.steps-list { margin: 0.25rem 0 0; padding-left: 1.25rem; display: grid; gap: 0.4rem; }
	.copy { display: flex; flex-wrap: wrap; align-items: center; gap: 0.4rem; margin-top: 0.3rem; padding: 0.35rem 0.6rem; border-radius: 0.5rem; background: var(--color-base-200); }
	.copy code { min-width: 0; flex: 1 1 12rem; overflow-wrap: anywhere; }
</style>
