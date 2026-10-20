<script lang="ts">
	// School sign-in (D-64): students and teachers sign in with their Windows
	// Active Directory or LDAP account. The admin fills in the connection, tests
	// it with a real account step by step, then turns it on.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import OptionGroup from '../ui/OptionGroup.svelte';
	import Skeleton from '../ui/Skeleton.svelte';
	import { toast } from '../ui/toast.svelte';

	type Config = {
		enabled: boolean;
		kind: 'ad' | 'ldap';
		url: string;
		start_tls: boolean;
		ca_pem: string;
		bind_dn: string;
		bind_password?: string;
		bind_password_set?: boolean;
		base_dn: string;
		user_filter: string;
		id_attr: string;
		email_attr: string;
		name_attr: string;
		teacher_group: string;
		student_group: string;
		nested_groups: boolean;
		link_by_email: boolean;
	};
	type Step = { name: string; ok: boolean; detail?: string };
	type TestResult = { ok: boolean; steps: Step[]; account?: { email: string; name: string; role: string; dn: string } };

	let c = $state<Config | null>(null);
	let defaults = $state<Record<string, Partial<Config>>>({});
	let errors = $state<Record<string, string>>({});
	let saving = $state(false);
	let testLogin = $state('');
	let testPassword = $state('');
	let testing = $state(false);
	let result = $state<TestResult | null>(null);
	let advanced = $state(false);

	$effect(() => {
		api.get<{ config: Config; defaults: Record<string, Partial<Config>> }>('/api/admin/directory').then((r) => {
			defaults = r.defaults;
			c = { ...r.config, bind_password: '' };
		});
	});

	// Switching the kind fills in that kind's filter and attributes, unless they were changed.
	function setKind(k: string) {
		if (!c) return;
		const from = defaults[c.kind] ?? {};
		const to = defaults[k] ?? {};
		for (const key of ['user_filter', 'id_attr', 'email_attr', 'name_attr'] as const) {
			if (!c[key] || c[key] === from[key]) c[key] = (to[key] as string) ?? '';
		}
		c.nested_groups = k === 'ad';
		c.kind = k as Config['kind'];
	}
	const secure = $derived(c ? (c.url.startsWith('ldap://') ? 'starttls' : 'ldaps') : 'ldaps');
	function setSecure(v: string) {
		if (!c) return;
		c.start_tls = v === 'starttls';
		if (v === 'starttls' && c.url.startsWith('ldaps://')) c.url = 'ldap://' + c.url.slice(8);
		if (v === 'ldaps' && c.url.startsWith('ldap://')) c.url = 'ldaps://' + c.url.slice(7);
	}

	async function save(enabled = c?.enabled ?? false) {
		if (!c) return;
		saving = true;
		errors = {};
		try {
			const saved = await api.put<Config>('/api/admin/directory', { ...c, enabled });
			c = { ...saved, bind_password: '' };
			toast(enabled ? 'School sign-in is on' : 'Settings saved');
		} catch (e) {
			if (e instanceof ApiError) {
				errors = e.fields;
				toast(Object.keys(e.fields).length ? 'Check the highlighted settings' : e.message, 'error');
			}
		} finally {
			saving = false;
		}
	}
	async function test() {
		if (!c) return;
		testing = true;
		errors = {};
		result = null;
		try {
			result = await api.post<TestResult>('/api/admin/directory/test', { config: c, login: testLogin, password: testPassword });
		} catch (e) {
			if (e instanceof ApiError) {
				errors = e.fields;
				toast(Object.keys(e.fields).length ? 'Check the highlighted settings' : e.message, 'error');
			}
		} finally {
			testing = false;
			testPassword = '';
		}
	}
</script>

{#snippet field(key: keyof Config, label: string, hint: string, placeholder = '', type = 'text')}
	<div class="srow">
		<div class="slabel"><label class="font-semibold" for={'d-' + key}>{label}</label>{#if hint}<span class="small muted">{hint}</span>{/if}</div>
		<div class="scontrol">
			<input class="input input-sm w-full" class:input-error={errors[key]} id={'d-' + key} {type} {placeholder} autocomplete="off" spellcheck="false"
				value={(c?.[key] as string) ?? ''} oninput={(e) => c && ((c[key] as string) = e.currentTarget.value)} />
			{#if errors[key]}<p class="field-error m-0">{errors[key]}</p>{/if}
		</div>
	</div>
{/snippet}

{#if !c}
	<Skeleton lines={5} />
{:else}
	<div class="vstack">
		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<div class="flex flex-wrap items-center gap-3">
				<span class="s-icon" aria-hidden="true"><Icon name="users" size={18} /></span>
				<div class="min-w-0 flex-1">
					<p class="m-0 font-semibold">School sign-in is {c.enabled ? 'on' : 'off'}</p>
					<p class="small muted m-0">{c.enabled ? 'Students and teachers sign in with their school account. Admins and managers keep their own password.' : 'Students and teachers sign in with an account made here. Test the settings below, then turn school sign-in on.'}</p>
				</div>
				{#if c.enabled}<button class="btn btn-sm" disabled={saving} onclick={() => save(false)}>Turn off</button>
				{:else}<button class="btn btn-sm btn-primary" disabled={saving} onclick={() => save(true)}><Icon name="check" size={15} />Turn on</button>{/if}
			</div>
		</div>

		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<h3 class="b-title"><Icon name="link" size={16} />Connection</h3>
			<div class="srow">
				<div class="slabel"><span class="font-semibold">Directory</span></div>
				<div class="scontrol">
					<OptionGroup label="Directory" value={c.kind} onchange={setKind} options={[
						{ v: 'ad', label: 'Active Directory', icon: 'monitor', hint: 'Windows Server: users sign in with their Windows name, user@domain, or email.' },
						{ v: 'ldap', label: 'Other LDAP', icon: 'list', hint: 'OpenLDAP and similar: users sign in with their uid or email.' }
					]} />
				</div>
			</div>
			{@render field('url', 'Server', 'A domain controller or LDAP server.', c.kind === 'ad' ? 'ldaps://dc1.school.local' : 'ldaps://ldap.school.local')}
			<div class="srow">
				<div class="slabel"><span class="font-semibold">Encryption</span><span class="small muted">Passwords never cross the network unencrypted.</span></div>
				<div class="scontrol">
					<OptionGroup label="Encryption" value={secure} onchange={setSecure} options={[
						{ v: 'ldaps', label: 'LDAPS', icon: 'check', hint: 'Encrypted from the start, usually port 636.' },
						{ v: 'starttls', label: 'StartTLS', icon: 'arrow-up', hint: 'A plain connection upgraded to encryption, usually port 389.' }
					]} />
					{#if errors.start_tls}<p class="field-error m-0">{errors.start_tls}</p>{/if}
				</div>
			</div>
			<div class="srow">
				<div class="slabel"><label class="font-semibold" for="d-ca">Certificate authority</label><span class="small muted">Only if the server's certificate comes from your own CA. Paste it in PEM.</span></div>
				<div class="scontrol">
					<textarea class="textarea textarea-sm w-full font-mono" class:textarea-error={errors.ca_pem} id="d-ca" rows="3" spellcheck="false" placeholder="-----BEGIN CERTIFICATE-----" bind:value={c.ca_pem}></textarea>
					{#if errors.ca_pem}<p class="field-error m-0">{errors.ca_pem}</p>{/if}
				</div>
			</div>
			{@render field('bind_dn', 'Service account', 'A read-only account that looks users up.', 'CN=svc-quiz,OU=Service Accounts,DC=school,DC=local')}
			<div class="srow">
				<div class="slabel"><label class="font-semibold" for="d-pw">Service account password</label><span class="small muted">{c.bind_password_set ? 'Stored, encrypted. Leave empty to keep it.' : 'Stored encrypted with the master key.'}</span></div>
				<div class="scontrol">
					<input class="input input-sm w-full max-w-xs" class:input-error={errors.bind_password} id="d-pw" type="password" autocomplete="new-password" placeholder={c.bind_password_set ? '••••••••' : ''} bind:value={c.bind_password} />
					{#if errors.bind_password}<p class="field-error m-0">{errors.bind_password}</p>{/if}
				</div>
			</div>
			{@render field('base_dn', 'Where to look for people', 'Searches include everything below it.', 'OU=People,DC=school,DC=local')}
		</div>

		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<h3 class="b-title"><Icon name="users" size={16} />Who signs in as what</h3>
			{@render field('teacher_group', 'Teachers group', 'Members sign in as teachers.', 'CN=Staff,OU=Groups,DC=school,DC=local')}
			{@render field('student_group', 'Students group', 'Members sign in as students. Leave empty to let everyone else found sign in as a student.', 'CN=Students,OU=Groups,DC=school,DC=local')}
			{#if c.kind === 'ad'}
				<div class="srow">
					<div class="slabel"><span class="font-semibold">Groups inside the groups</span></div>
					<div class="scontrol">
						<OptionGroup label="Groups inside the groups" value={String(c.nested_groups)} onchange={(v) => c && (c.nested_groups = v === 'true')} options={[
							{ v: 'true', label: 'Count too', icon: 'check', hint: 'Members of Year 10, when Year 10 is inside Students, sign in as students.' },
							{ v: 'false', label: 'Direct members only', icon: 'x' }
						]} />
					</div>
				</div>
			{/if}
			<div class="srow">
				<div class="slabel"><span class="font-semibold">Existing accounts</span><span class="small muted">A student or teacher who already has an account here with the same email.</span></div>
				<div class="scontrol">
					<OptionGroup label="Existing accounts" value={String(c.link_by_email)} onchange={(v) => c && (c.link_by_email = v === 'true')} options={[
						{ v: 'true', label: 'Move to school sign-in', icon: 'arrow-right', hint: 'They keep their classrooms and results; their old password stops working.' },
						{ v: 'false', label: 'Leave them', icon: 'x', hint: 'They can’t sign in with their school account until an admin removes the old one.' }
					]} />
				</div>
			</div>
			<button type="button" class="link link-hover small mt-2" onclick={() => (advanced = !advanced)} aria-expanded={advanced}>{advanced ? 'Hide' : 'Show'} the search filter and attributes</button>
			{#if advanced}
				{@render field('user_filter', 'Search filter', '{login} is replaced by what the person types.')}
				{@render field('id_attr', 'Unique id attribute', 'Never changes, even when someone is renamed.')}
				{@render field('email_attr', 'Email attribute', '')}
				{@render field('name_attr', 'Name attribute', '')}
			{/if}
		</div>

		<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-5">
			<h3 class="b-title"><Icon name="play" size={16} />Test with a real account</h3>
			<p class="small muted m-0 mb-3">Tries the settings above, saved or not. The password is used once and not kept.</p>
			<form class="flex flex-wrap items-end gap-2" onsubmit={(e) => { e.preventDefault(); test(); }}>
				<div><label class="small font-semibold" for="t-login">School username or email</label><input class="input input-sm w-56" id="t-login" autocomplete="off" bind:value={testLogin} /></div>
				<div><label class="small font-semibold" for="t-pw">Their password</label><input class="input input-sm w-48" id="t-pw" type="password" autocomplete="off" bind:value={testPassword} /></div>
				<button class="btn btn-sm" disabled={testing}>{#if testing}<span class="loading loading-spinner loading-xs"></span>{/if}{testLogin ? 'Test sign-in' : 'Test the connection'}</button>
			</form>
			{#if result}
				<ol class="steps-list" aria-live="polite">
					{#each result.steps as s (s.name)}
						<li class:bad={!s.ok}><Icon name={s.ok ? 'check-circle' : 'alert'} size={16} /><span><span class="font-semibold">{s.name}</span>{#if s.detail}<span class="small muted block break-all">{s.detail}</span>{/if}</span></li>
					{/each}
				</ol>
				{#if result.account}
					<p class="alert alert-soft alert-success small m-0 mt-2"><Icon name="check" size={15} />{result.account.name} ({result.account.email}) would sign in as a {result.account.role}.</p>
				{/if}
			{/if}
		</div>

		<div class="row">
			<button class="btn btn-primary" disabled={saving} onclick={() => save()}>Save settings</button>
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
	.steps-list { list-style: none; margin: 0.75rem 0 0; padding: 0; display: grid; gap: 0.4rem; }
	.steps-list li { display: flex; gap: 0.5rem; align-items: flex-start; }
	.steps-list li :global(svg) { color: var(--color-success); flex: none; margin-top: 0.15rem; }
	.steps-list li.bad :global(svg) { color: var(--color-error); }
</style>
