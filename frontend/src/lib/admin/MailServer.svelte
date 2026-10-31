<script lang="ts">
	// The mail server (D-104), admins only: where the platform's email goes
	// out, set here instead of the config file, with a test that sends one
	// email at once and says which step failed. The password is never shown.
	import { api, ApiError } from '../api';
	import { auth } from '../session.svelte';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';

	type Server = { enabled: boolean; host: string; port: number; security: string; username: string; password?: string; password_set: boolean; from: string; from_name: string; updated_at?: string };
	type State = Server & { source: 'console' | 'config' | 'log'; config: { addr: string; from: string; user: string } };
	type Result = { ok: boolean; step?: string; error?: string; via: string };

	const SECURITY = [
		{ id: 'starttls', label: 'STARTTLS', hint: 'Port 587. Encrypts after connecting; refuses to send if the server can’t.' },
		{ id: 'tls', label: 'TLS', hint: 'Port 465. Encrypted from the first byte.' },
		{ id: 'auto', label: 'STARTTLS if offered', hint: 'Port 25 or 587. Encrypts when the server offers it.' },
		{ id: 'none', label: 'None', hint: 'Only for a mail server on this machine (localhost).' }
	];
	// Common providers, to fill host, port and security in.
	const PRESETS = [
		{ label: 'This server (Postfix)', host: 'localhost', port: 25, security: 'none' },
		{ label: 'This server with login', host: '', port: 587, security: 'starttls' },
		{ label: 'Gmail / Google Workspace', host: 'smtp.gmail.com', port: 587, security: 'starttls' },
		{ label: 'Microsoft 365', host: 'smtp.office365.com', port: 587, security: 'starttls' },
		{ label: 'Brevo', host: 'smtp-relay.brevo.com', port: 587, security: 'starttls' },
		{ label: 'Amazon SES (us-east-1)', host: 'email-smtp.us-east-1.amazonaws.com', port: 587, security: 'starttls' }
	];
	const STEPS: Record<string, string> = {
		connect: "Couldn't reach the server: check the host and port, and that the firewall lets this server out on that port.",
		tls: "Couldn't encrypt the connection: try the other security setting for this port (587 → STARTTLS, 465 → TLS), or check the server's certificate matches its name.",
		login: 'The server refused the login: check the username and password (Gmail and Microsoft 365 need an app password).',
		send: 'The server refused the email: check the from address is one this login may send as.'
	};

	let st = $state<State | null>(null);
	let f = $state<Server>({ enabled: false, host: '', port: 587, security: 'starttls', username: '', password: '', password_set: false, from: '', from_name: '' });
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);
	let testTo = $state(auth.user?.email ?? '');
	let result = $state<Result | null>(null);
	let changed = $state(false);

	async function load() {
		st = await api.get<State>('/api/admin/email/server');
		f = { ...st, password: '' };
		if (!f.host && st.config.addr) {
			// Start from the config file's settings.
			const [h, p] = st.config.addr.split(':');
			f.host = h;
			f.port = Number(p) || 587;
			f.security = 'auto';
			f.from = st.config.from;
			f.username = st.config.user;
		}
		changed = false;
	}
	load();

	function preset(i: number) {
		const p = PRESETS[i];
		if (!p) return;
		f.host = p.host || f.host;
		f.port = p.port;
		f.security = p.security;
		if (p.security === 'none') f.username = '';
		changed = true;
	}
	// Only the settings: the state's other fields (source, config…) aren't accepted back.
	const body = () => ({ enabled: f.enabled, host: f.host, port: Number(f.port), security: f.security, username: f.username, password: f.password ?? '', from: f.from, from_name: f.from_name });
	function failed(err: unknown, what: string) {
		if (err instanceof ApiError && Object.keys(err.fields).length) {
			errors = err.fields;
			toast(Object.values(err.fields)[0], 'error');
		} else toast(err instanceof ApiError ? err.message : what, 'error');
	}
	async function save(e?: SubmitEvent) {
		e?.preventDefault();
		busy = true;
		errors = {};
		try {
			st = await api.put<State>('/api/admin/email/server', body());
			f = { ...st, password: '' };
			changed = false;
			toast(st.enabled ? 'Saved: email now goes out through this server' : 'Saved, and off: the config file decides');
		} catch (err) {
			failed(err, 'Could not save it');
		} finally {
			busy = false;
		}
	}
	async function test(saved: boolean) {
		busy = true;
		errors = {};
		result = null;
		try {
			result = await api.post<Result>('/api/admin/email/server/test', saved ? { to: testTo } : { to: testTo, server: body() });
			if (result.ok) toast(`Sent to ${testTo}: check that inbox (and spam)`);
		} catch (err) {
			failed(err, 'Could not test it');
		} finally {
			busy = false;
		}
	}
	const SOURCES = { console: 'the settings below', config: 'the server’s config file', log: 'nothing: emails are only written to the log' };
</script>

<div class="vstack">
	{#if st}
		<div class="alert {st.source === 'log' ? 'alert-warning' : 'alert-info'} alert-soft" role="status">
			<Icon name="info" size={18} />
			<div>
				<p class="m-0">Email goes out through <b>{SOURCES[st.source]}</b>{#if st.source === 'config'} ({st.config.addr}, from {st.config.from}){/if}.</p>
				{#if st.source !== 'console'}<p class="m-0 small">Turn the settings below on to take over from it.</p>{/if}
			</div>
		</div>
	{/if}

	<div class="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
		<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" onsubmit={save} oninput={() => (changed = true)} aria-labelledby="ms-h">
			<div class="flex flex-wrap items-center gap-2">
				<h2 id="ms-h" class="m-0 grow text-lg">Mail server</h2>
				<label class="flex items-center gap-2 font-semibold"><input type="checkbox" class="toggle toggle-primary" bind:checked={f.enabled} />Use these settings</label>
			</div>
			<label class="small">Fill in for
				<select class="select select-sm ml-1 w-64 max-w-full" onchange={(e) => { preset(Number(e.currentTarget.value)); e.currentTarget.value = ''; }}>
					<option value="">Choose a provider…</option>
					{#each PRESETS as p, i (p.label)}<option value={i}>{p.label}</option>{/each}
				</select>
			</label>
			<div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem]">
				<div>
					<label for="ms-host" class="small font-semibold">Server</label>
					<input id="ms-host" class="input w-full" class:input-error={errors.host} placeholder="smtp.gmail.com" autocomplete="off" bind:value={f.host} />
					{#if errors.host}<p class="m-0 small text-error">{errors.host}</p>{/if}
				</div>
				<div>
					<label for="ms-port" class="small font-semibold">Port</label>
					<input id="ms-port" class="input w-full" class:input-error={errors.port} type="number" min="1" max="65535" bind:value={f.port} />
				</div>
			</div>
			<fieldset>
				<legend class="small font-semibold">Encryption</legend>
				<div class="grid gap-1 sm:grid-cols-2">
					{#each SECURITY as s (s.id)}
						<label class="flex items-start gap-2 rounded-box border border-base-300 p-2 cursor-pointer" class:border-primary={f.security === s.id}>
							<input type="radio" class="radio radio-sm mt-0.5" name="ms-sec" value={s.id} bind:group={f.security} />
							<span><span class="font-semibold">{s.label}</span><br /><span class="small muted">{s.hint}</span></span>
						</label>
					{/each}
				</div>
				{#if errors.security}<p class="m-0 small text-error">{errors.security}</p>{/if}
			</fieldset>
			<div class="grid gap-3 sm:grid-cols-2">
				<div>
					<label for="ms-user" class="small font-semibold">Username</label>
					<input id="ms-user" class="input w-full" placeholder="Empty for no login" autocomplete="off" bind:value={f.username} />
				</div>
				<div>
					<label for="ms-pw" class="small font-semibold">Password</label>
					<input id="ms-pw" class="input w-full" class:input-error={errors.password} type="password" autocomplete="new-password" placeholder={f.password_set ? 'Saved: type to replace' : ''} bind:value={f.password} disabled={!f.username} />
					{#if errors.password}<p class="m-0 small text-error">{errors.password}</p>{/if}
				</div>
			</div>
			<div class="grid gap-3 sm:grid-cols-2">
				<div>
					<label for="ms-from" class="small font-semibold">From address</label>
					<input id="ms-from" class="input w-full" class:input-error={errors.from} type="email" placeholder="no-reply@school.lk" bind:value={f.from} />
					{#if errors.from}<p class="m-0 small text-error">{errors.from}</p>{/if}
				</div>
				<div>
					<label for="ms-name" class="small font-semibold">From name</label>
					<input id="ms-name" class="input w-full" placeholder="Classroom Quiz" maxlength="100" bind:value={f.from_name} />
				</div>
			</div>
			<div class="flex flex-wrap gap-2">
				<button class="btn btn-primary" disabled={busy}>Save</button>
				{#if changed}<span class="small muted self-center">Not saved yet</span>{/if}
			</div>
			{#if st?.updated_at && st.host}<p class="m-0 small muted">Last changed {new Date(st.updated_at).toLocaleString()}.</p>{/if}
		</form>

		<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="ms-t">
			<h2 id="ms-t" class="m-0 text-lg">Send a test</h2>
			<p class="m-0 small muted">Sends one email now, without the queue, and tells you where it failed.</p>
			<div>
				<label for="ms-to" class="small font-semibold">To</label>
				<input id="ms-to" class="input w-full" type="email" bind:value={testTo} />
			</div>
			<div class="flex flex-wrap gap-2">
				<button class="btn" onclick={() => test(false)} disabled={busy || !testTo || !f.host}>Test these settings</button>
				<button class="btn btn-ghost" onclick={() => test(true)} disabled={busy || !testTo}>Test what sends now</button>
			</div>
			{#if busy}<p class="m-0 small muted" role="status"><span class="loading loading-spinner loading-xs"></span> Talking to the server…</p>{/if}
			{#if result}
				<div class="alert {result.ok ? 'alert-success' : 'alert-error'} alert-soft items-start" role="status">
					<Icon name={result.ok ? 'check' : 'alert'} size={18} />
					<div class="min-w-0">
						{#if result.ok}
							<p class="m-0">The server took it ({result.via}). If it doesn't arrive, look in spam, then at the Queue and the server's mail log.</p>
						{:else}
							<p class="m-0 font-semibold">{STEPS[result.step ?? ''] ?? 'It failed.'}</p>
							<p class="m-0 mt-1 small break-words font-mono">{result.error}</p>
						{/if}
					</div>
				</div>
			{/if}
			<details class="small">
				<summary class="cursor-pointer font-semibold">Accepted here but returned later?</summary>
				<p>That's the receiving provider refusing it after your server took it, so it shows in the server's mail log, not here. For Gmail's "550 5.7.25 … PTR record": the sending server's address (often its IPv6 one) needs reverse DNS pointing at your domain, set at the hosting provider; or have Postfix send over IPv4 only (<code>postconf -e inet_protocols=ipv4</code>, then restart Postfix); or use a provider like Brevo above. SPF, DKIM and DMARC records for the from domain help too.</p>
			</details>
		</section>
	</div>
</div>
