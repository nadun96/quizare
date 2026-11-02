<script lang="ts">
	// Sign-in limits (D-126), admins only: how many sign-ins one internet
	// address may make at once and then each second (a school behind one
	// router signs in together), and how many passwords the server checks at
	// a time. The per-account limit that stops password guessing is fixed.
	import { api, ApiError } from '../api';
	import Icon from '../ui/Icon.svelte';
	import { toast } from '../ui/toast.svelte';
	import { classSignInSeconds, type SignInLimits } from './signin';

	let l = $state<SignInLimits | null>(null);
	let f = $state({ network_burst: 600, network_per_sec: 20, workers: '' as string });
	let errors = $state<Record<string, string>>({});
	let busy = $state(false);

	function fill(x: SignInLimits) {
		l = x;
		f = { network_burst: x.network_burst, network_per_sec: x.network_per_sec, workers: x.password_workers == null ? '' : String(x.password_workers) };
	}
	api.get<SignInLimits>('/api/admin/sign-in-limits').then(fill);

	const wait = $derived(classSignInSeconds(500, Number(f.network_burst), Number(f.network_per_sec), 30));

	async function save(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		errors = {};
		try {
			fill(await api.put<SignInLimits>('/api/admin/sign-in-limits', { network_burst: Number(f.network_burst), network_per_sec: Number(f.network_per_sec), password_workers: f.workers === '' ? null : Number(f.workers) }));
			toast('Sign-in limits saved; they apply now');
		} catch (err) {
			if (err instanceof ApiError) errors = err.fields;
			toast(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : 'Could not save', 'error');
		} finally {
			busy = false;
		}
	}
</script>

<section aria-labelledby="signin-limits-h">
	{#if l}
		<form class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-3" onsubmit={save}>
			<div>
				<h3 id="signin-limits-h" class="m-0 flex items-center gap-2 text-base"><Icon name="key" size={18} />Sign-in limits</h3>
				<p class="m-0 small muted">A whole class on one school network shares one internet address, so the server lets that address sign in many people at once, then so many a second. Each account still gets only 10 tries, then one every 30 seconds, which is what stops password guessing.</p>
			</div>
			<div class="grid gap-3 sm:grid-cols-3">
				<label class="flex flex-col gap-1">
					<span class="small">Sign-ins at once from one network</span>
					<input class="input" class:input-error={errors.network_burst} type="number" min="50" max="5000" bind:value={f.network_burst} />
					{#if errors.network_burst}<span class="field-error">{errors.network_burst}</span>{/if}
				</label>
				<label class="flex flex-col gap-1">
					<span class="small">Then, each second</span>
					<input class="input" class:input-error={errors.network_per_sec} type="number" min="1" max="200" step="0.5" bind:value={f.network_per_sec} />
					{#if errors.network_per_sec}<span class="field-error">{errors.network_per_sec}</span>{/if}
				</label>
				<label class="flex flex-col gap-1">
					<span class="small">Passwords checked at a time</span>
					<select class="select" bind:value={f.workers}>
						<option value="">The server’s setting ({l.default_workers})</option>
						{#each [1, 2, 3, 4, 5, 6, 7, 8] as n (n)}<option value={String(n)} disabled={n > l.cpus}>{n}{n > l.cpus ? ' (more than the cores)' : ''}</option>{/each}
					</select>
				</label>
			</div>
			<p class="m-0 small">
				<Icon name="info" size={14} />
				{wait <= 1 ? 'With these, 500 students on one network arriving over 30 seconds all sign in without waiting.' : `With these, the last of 500 students on one network arriving over 30 seconds waits about ${Math.round(wait)} s.`}
				Each password check uses about 19 MB of memory and one core for a moment; this server has {l.cpus} cores.
			</p>
			<div class="flex flex-wrap items-center gap-2">
				<button class="btn btn-primary" disabled={busy}>Save</button>
				<button type="button" class="btn btn-ghost" disabled={busy} onclick={() => (f = { network_burst: 600, network_per_sec: 20, workers: '' })}>Recommended: 600, then 20 a second</button>
				<span class="small muted">Check them with Capacity test, “All from one school network”.</span>
			</div>
		</form>
	{/if}
</section>
