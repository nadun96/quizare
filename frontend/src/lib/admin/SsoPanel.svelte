<script lang="ts">
	// Sign-in with OpenID Connect providers (D-70): Google, Microsoft,
	// Keycloak, Okta or any other. Each enabled provider is a button on the
	// login page. The admin registers the redirect URL with the provider,
	// pastes its client ID and secret here, and decides who gets in as what.
	import { api, ApiError } from '../api';
	import { blankProvider, describe, fromList, PRESETS, toList, type SSOProvider } from '../sso';
	import Icon from '../ui/Icon.svelte';
	import OptionGroup from '../ui/OptionGroup.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';

	type TestResult = { ok: boolean; message: string; authorization_endpoint?: string; pkce: boolean };

	let providers = $state<SSOProvider[] | null>(null);
	let redirectUrl = $state('');
	let edit = $state<SSOProvider | null>(null);
	let preset = $state('google');
	// List fields, edited as text.
	let domains = $state('');
	let scopes = $state('');
	let teacherValues = $state('');
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	let advanced = $state(false);
	let testing = $state(false);
	let result = $state<TestResult | null>(null);

	async function load() {
		const r = await api.get<{ providers: SSOProvider[]; redirect_url: string }>('/api/admin/sso');
		providers = r.providers;
		redirectUrl = r.redirect_url;
	}
	$effect(() => {
		load();
	});

	function open(p: SSOProvider | null) {
		edit = p ? { ...p, client_secret: '' } : blankProvider(preset);
		domains = fromList(edit.allowed_domains);
		scopes = fromList(edit.scopes, ' ');
		teacherValues = fromList(edit.teacher_values);
		errors = {};
		result = null;
		advanced = false;
	}
	function usePreset(v: string) {
		preset = v;
		if (edit && !edit.id) {
			const keep = { client_id: edit.client_id, client_secret: edit.client_secret, allowed_domains: edit.allowed_domains, new_accounts: edit.new_accounts };
			edit = { ...blankProvider(v), ...keep };
			scopes = fromList(edit.scopes, ' ');
		}
	}

	async function save(enabled?: boolean) {
		if (!edit) return;
		saving = true;
		errors = {};
		const body = { ...edit, enabled: enabled ?? edit.enabled, allowed_domains: toList(domains), scopes: toList(scopes), teacher_values: toList(teacherValues) };
		try {
			if (edit.id) await api.put('/api/admin/sso/' + edit.id, body);
			else await api.post('/api/admin/sso', body);
			toast(body.enabled ? `“${body.name}” is on the login page` : 'Provider saved');
			edit = null;
			await load();
		} catch (e) {
			if (e instanceof ApiError) {
				errors = e.fields;
				toast(Object.keys(e.fields).length ? 'Check the highlighted settings' : e.message, 'error');
			}
		} finally {
			saving = false;
		}
	}

	async function toggle(p: SSOProvider) {
		try {
			await api.put('/api/admin/sso/' + p.id, { ...p, client_secret: '', enabled: !p.enabled });
			toast(p.enabled ? `“${p.name}” is off` : `“${p.name}” is on the login page`);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? (Object.values(e.fields)[0] ?? e.message) : 'Could not save', 'error');
		}
	}

	async function remove(p: SSOProvider) {
		if (!(await confirmDialog({ title: `Remove “${p.name}”?`, body: 'Its button leaves the login page, and accounts lose this way of signing in. Accounts made through it stay; their owners can set a password with “Forgot password”.', confirm: 'Remove', danger: true }))) return;
		await api.del('/api/admin/sso/' + p.id);
		toast('Provider removed');
		await load();
	}

	async function move(i: number, by: number) {
		if (!providers) return;
		const list = [...providers];
		const [p] = list.splice(i, 1);
		list.splice(i + by, 0, p);
		providers = list;
		await api.put('/api/admin/sso-order', { ids: list.map((x) => x.id) });
	}

	async function test() {
		if (!edit) return;
		testing = true;
		result = null;
		try {
			result = await api.post<TestResult>('/api/admin/sso/test', { issuer: edit.issuer });
		} finally {
			testing = false;
		}
	}

	async function copy() {
		try {
			await navigator.clipboard.writeText(redirectUrl);
			toast('Redirect URL copied');
		} catch {
			toast(redirectUrl);
		}
	}
</script>

{#snippet field(key: 'name' | 'issuer' | 'client_id' | 'email_claim' | 'teacher_claim', label: string, hint: string, placeholder = '')}
	<div class="srow">
		<div class="slabel"><label class="font-semibold" for={'o-' + key}>{label}</label>{#if hint}<span class="small muted">{hint}</span>{/if}</div>
		<div class="scontrol">
			<input class="input input-sm w-full" class:input-error={errors[key]} id={'o-' + key} {placeholder} autocomplete="off" spellcheck="false"
				value={edit?.[key] ?? ''} oninput={(e) => edit && (edit[key] = e.currentTarget.value)} />
			{#if errors[key]}<p class="field-error m-0">{errors[key]}</p>{/if}
		</div>
	</div>
{/snippet}

{#if !providers}
	<Skeleton lines={4} />
{:else}
	<div class="vstack">
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<div class="flex flex-wrap items-center gap-3">
				<span class="s-icon" aria-hidden="true"><Icon name="link" size={18} /></span>
				<div class="min-w-0 flex-1">
					<p class="m-0 font-semibold">Sign-in with other accounts</p>
					<p class="small muted m-0">Google, Microsoft, Keycloak, Okta or any OpenID Connect provider. Each one you turn on is a button on the login page; passwords keep working.</p>
				</div>
				{#if !edit}<button class="btn btn-sm btn-primary" onclick={() => open(null)}><Icon name="plus" size={15} />Add a provider</button>{/if}
			</div>
			<div class="redirect mt-3">
				<span class="small font-semibold">Redirect URL</span>
				<code class="truncate">{redirectUrl}</code>
				<button class="btn btn-xs" onclick={copy}><Icon name="copy" size={13} />Copy</button>
				<span class="small muted w-full">Register this address with each provider (it may call it the callback or reply URL).</span>
			</div>
		</div>

		{#if edit}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
				<h3 class="b-title"><Icon name="link" size={16} />{edit.id ? `Edit “${edit.name}”` : 'Add a provider'}</h3>
				{#if !edit.id}
					<div class="srow">
						<div class="slabel"><span class="font-semibold">Provider</span></div>
						<div class="scontrol">
							<OptionGroup label="Provider" value={preset} onchange={usePreset} options={PRESETS.map((x) => ({ v: x.id, label: x.label, hint: x.hint }))} />
						</div>
					</div>
				{/if}
				{@render field('name', 'Name on the button', '“Sign in with …”', 'Google')}
				<div class="srow">
					<div class="slabel"><label class="font-semibold" for="o-issuer">Issuer</label><span class="small muted">The provider’s address; it publishes its settings under /.well-known/openid-configuration.</span></div>
					<div class="scontrol">
						<div class="flex gap-2">
							<input class="input input-sm w-full" class:input-error={errors.issuer} id="o-issuer" autocomplete="off" spellcheck="false" bind:value={edit.issuer} />
							<button type="button" class="btn btn-sm" disabled={testing || !edit.issuer} onclick={test}>{#if testing}<span class="loading loading-spinner loading-xs"></span>{/if}Test</button>
						</div>
						{#if errors.issuer}<p class="field-error m-0">{errors.issuer}</p>{/if}
						{#if result}
							<p class="small m-0 flex items-start gap-1" class:text-success={result.ok} class:text-error={!result.ok} role="status">
								<Icon name={result.ok ? 'check-circle' : 'alert'} size={15} />
								<span>{result.ok ? `Found it. ${result.pkce ? 'It supports PKCE.' : 'It doesn’t list PKCE support; sign-in still uses it.'}` : result.message}</span>
							</p>
						{/if}
					</div>
				</div>
				{@render field('client_id', 'Client ID', 'From the app you registered with the provider.')}
				<div class="srow">
					<div class="slabel"><label class="font-semibold" for="o-secret">Client secret</label><span class="small muted">{edit.client_secret_set ? 'Stored, encrypted. Leave empty to keep it.' : 'Stored encrypted with the master key. Leave empty for a public client.'}</span></div>
					<div class="scontrol">
						<input class="input input-sm w-full max-w-md" class:input-error={errors.client_secret} id="o-secret" type="password" autocomplete="new-password" placeholder={edit.client_secret_set ? '••••••••' : ''} bind:value={edit.client_secret} />
						{#if errors.client_secret}<p class="field-error m-0">{errors.client_secret}</p>{/if}
					</div>
				</div>

				<h3 class="b-title mt-4"><Icon name="users" size={16} />Who gets in, as what</h3>
				<div class="srow">
					<div class="slabel"><label class="font-semibold" for="o-domains">Email domains</label><span class="small muted">Only accounts at these domains, one per line. Empty: any. Set this for Google, or anyone with Gmail could sign in.</span></div>
					<div class="scontrol">
						<textarea class="textarea textarea-sm w-full max-w-md" class:textarea-error={errors.allowed_domains} id="o-domains" rows="2" spellcheck="false" placeholder="school.edu" bind:value={domains}></textarea>
						{#if errors.allowed_domains}<p class="field-error m-0">{errors.allowed_domains}</p>{/if}
					</div>
				</div>
				<div class="srow">
					<div class="slabel"><span class="font-semibold">People without an account</span></div>
					<div class="scontrol">
						<OptionGroup label="People without an account" value={edit.new_accounts} onchange={(v) => edit && (edit.new_accounts = v as SSOProvider['new_accounts'])} options={[
							{ v: 'none', label: 'Turned away', icon: 'x', hint: 'Only people with an account here (made by them or by you) can use this sign-in.' },
							{ v: 'student', label: 'Become students', icon: 'user', hint: 'Their first sign-in makes a student account.' },
							{ v: 'teacher', label: 'Become teachers', icon: 'users', hint: 'Their first sign-in makes a teacher account (approved by an admin, if your policy says so).' }
						]} />
					</div>
				</div>
				{#if edit.new_accounts === 'student'}
					{@render field('teacher_claim', 'Teachers by claim', 'Optional: new accounts whose claim has one of the values below become teachers instead.', 'groups')}
					{#if edit.teacher_claim}
						<div class="srow">
							<div class="slabel"><label class="font-semibold" for="o-tv">Teacher values</label><span class="small muted">One per line, exactly as the provider sends them.</span></div>
							<div class="scontrol">
								<textarea class="textarea textarea-sm w-full max-w-md" class:textarea-error={errors.teacher_values} id="o-tv" rows="2" spellcheck="false" placeholder="staff" bind:value={teacherValues}></textarea>
								{#if errors.teacher_values}<p class="field-error m-0">{errors.teacher_values}</p>{/if}
							</div>
						</div>
					{/if}
				{/if}
				<div class="srow">
					<div class="slabel"><span class="font-semibold">Existing accounts</span><span class="small muted">A student or teacher who already has an account here with the same email.</span></div>
					<div class="scontrol">
						<OptionGroup label="Existing accounts" value={String(edit.link_by_email)} onchange={(v) => edit && (edit.link_by_email = v === 'true')} options={[
							{ v: 'true', label: 'Sign them in', icon: 'arrow-right', hint: 'Their first sign-in joins the account; their password keeps working. Never for admins or managers.' },
							{ v: 'false', label: 'Only after they connect it', icon: 'link', hint: 'They connect it from their account page, signed in with their password.' }
						]} />
					</div>
				</div>

				<button type="button" class="link link-hover small mt-2 self-start" onclick={() => (advanced = !advanced)} aria-expanded={advanced}>{advanced ? 'Hide' : 'Show'} scopes and claims</button>
				{#if advanced}
					<div class="srow">
						<div class="slabel"><label class="font-semibold" for="o-scopes">Scopes</label><span class="small muted">openid is always asked for.</span></div>
						<div class="scontrol"><input class="input input-sm w-full max-w-md" class:input-error={errors.scopes} id="o-scopes" spellcheck="false" bind:value={scopes} />{#if errors.scopes}<p class="field-error m-0">{errors.scopes}</p>{/if}</div>
					</div>
					{@render field('email_claim', 'Email claim', 'Where the email is. Microsoft: preferred_username.', 'email')}
					<div class="srow">
						<div class="slabel"><span class="font-semibold">Unconfirmed emails</span><span class="small muted">When the provider doesn’t say the email is confirmed (Microsoft doesn’t).</span></div>
						<div class="scontrol">
							<OptionGroup label="Unconfirmed emails" value={String(edit.trust_email)} onchange={(v) => edit && (edit.trust_email = v === 'true')} options={[
								{ v: 'false', label: 'Don’t trust', icon: 'x', hint: 'Such sign-ins can’t match or make accounts by email.' },
								{ v: 'true', label: 'Trust them', icon: 'check', hint: 'Only for a provider your school runs, where people can’t choose their own email.' }
							]} />
						</div>
					</div>
				{/if}

				<div class="flex flex-wrap gap-2 mt-4">
					{#if edit.enabled}
						<button class="btn btn-primary btn-sm" disabled={saving} onclick={() => save()}>Save</button>
					{:else}
						<button class="btn btn-primary btn-sm" disabled={saving} onclick={() => save(true)}><Icon name="check" size={15} />Save and turn on</button>
						<button class="btn btn-sm" disabled={saving} onclick={() => save(false)}>Save, still off</button>
					{/if}
					<button class="btn btn-ghost btn-sm" onclick={() => (edit = null)}>Cancel</button>
				</div>
			</div>
		{/if}

		{#each providers as p, i (p.id)}
			<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
				<div class="flex flex-wrap items-start gap-3">
					<div class="min-w-0 flex-1">
						<p class="m-0 font-semibold">{p.name} <span class="badge badge-soft badge-sm" class:ok={p.enabled}>{p.enabled ? 'On' : 'Off'}</span></p>
						<p class="small muted m-0 truncate">{p.issuer}</p>
						<p class="small m-0 mt-1">{describe(p)}</p>
					</div>
					<div class="flex flex-wrap items-center gap-1">
						{#if providers.length > 1}
							<button class="btn btn-xs btn-ghost btn-square" disabled={i === 0} aria-label="Move {p.name} up" title="Move up" onclick={() => move(i, -1)}><Icon name="arrow-up" size={14} /></button>
							<button class="btn btn-xs btn-ghost btn-square" disabled={i === providers.length - 1} aria-label="Move {p.name} down" title="Move down" onclick={() => move(i, 1)}><Icon name="arrow-down" size={14} /></button>
						{/if}
						<button class="btn btn-sm" onclick={() => toggle(p)}>{p.enabled ? 'Turn off' : 'Turn on'}</button>
						<button class="btn btn-sm" onclick={() => open(p)}><Icon name="pencil" size={14} />Edit</button>
						<button class="btn btn-sm btn-ghost" aria-label="Remove {p.name}" onclick={() => remove(p)}><Icon name="trash" size={14} /></button>
					</div>
				</div>
			</div>
		{:else}
			{#if !edit}<p class="small muted m-0">No providers yet.</p>{/if}
		{/each}
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
	.redirect { display: flex; flex-wrap: wrap; align-items: center; gap: 0.35rem 0.6rem; padding: 0.6rem 0.75rem; border-radius: 0.6rem; background: var(--color-base-200); }
	.redirect code { min-width: 0; flex: 1 1 14rem; font-size: 0.85rem; }
</style>
