<script lang="ts">
	// Profile picture and password (D-49). Pictures are checked here for a
	// quick answer (type, 512 KB) and again by the server, which re-encodes
	// them. Changing the password signs out your other devices.
	import { ApiError, api } from '../api';
	import { auth } from '../session.svelte';
	import Avatar from '../ui/Avatar.svelte';
	import Icon from '../ui/Icon.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';

	const MAX = 512 * 1024;
	const TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];
	let file = $state<HTMLInputElement>();
	let uploading = $state(false);
	let picError = $state('');

	async function upload(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		picError = '';
		if (!f || !auth.user) return;
		if (!TYPES.includes(f.type)) picError = 'Use a PNG, JPEG, WebP or GIF picture.';
		else if (f.size > MAX) picError = `That picture is ${(f.size / 1024).toFixed(0)} KB; the limit is 512 KB.`;
		if (picError) {
			if (file) file.value = '';
			return;
		}
		uploading = true;
		try {
			const res = await fetch('/api/auth/me/avatar', { method: 'PUT', headers: { 'X-Requested-With': 'fetch', 'Content-Type': f.type }, body: f, credentials: 'same-origin' });
			const data = await res.json().catch(() => ({}));
			if (!res.ok) throw new ApiError(res.status, data.code ?? 'error', data.fields?.avatar ?? data.message ?? 'Upload failed', data.fields ?? {});
			auth.user = { ...auth.user, avatar: data.avatar };
			toast('Profile picture updated');
		} catch (err) {
			picError = err instanceof ApiError ? err.message : 'Upload failed. Check your connection.';
		} finally {
			uploading = false;
			if (file) file.value = '';
		}
	}
	async function removePicture() {
		if (!auth.user) return;
		if (!(await confirmDialog({ title: 'Remove your picture?', body: 'Your initial is shown instead.', confirm: 'Remove' }))) return;
		await api.del('/api/auth/me/avatar');
		auth.user = { ...auth.user, avatar: '' };
		toast('Picture removed');
	}

	// ---- password ----
	let current = $state('');
	let next = $state('');
	let again = $state('');
	let show = $state(false);
	let saving = $state(false);
	let errors = $state<Record<string, string>>({});
	const mismatch = $derived(again.length > 0 && again !== next);
	async function changePassword(e: SubmitEvent) {
		e.preventDefault();
		errors = {};
		if (next !== again) {
			errors = { again: "The two new passwords don't match." };
			return;
		}
		saving = true;
		try {
			await api.post('/api/auth/me/password', { current_password: current, new_password: next });
			current = next = again = '';
			toast('Password changed. Your other devices were signed out.');
		} catch (err) {
			if (err instanceof ApiError && Object.keys(err.fields).length) errors = err.fields;
			else errors = { _: err instanceof ApiError ? (err.status === 429 ? 'Too many attempts. Wait a minute and try again.' : err.message) : 'Could not change the password' };
		} finally {
			saving = false;
		}
	}
	const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1) + (/[.!?]$/.test(s) ? '' : '.');
</script>

{#if auth.user}
	<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack" aria-labelledby="pic-h">
		<h2 id="pic-h" class="mt-0">Profile picture</h2>
		<div class="flex flex-wrap items-center gap-4">
			<Avatar id={auth.user.id} name={auth.user.name} avatar={auth.user.avatar} size={80} />
			<div class="vstack min-w-0 flex-1">
				<div class="flex flex-wrap gap-2">
					<label class="btn btn-sm m-0" class:btn-disabled={uploading}>
						{#if uploading}<span class="loading loading-spinner loading-xs"></span>{:else}<Icon name="upload" size={15} />{/if}
						{auth.user.avatar ? 'Change picture' : 'Upload a picture'}
						<input bind:this={file} type="file" class="sr-only" accept={TYPES.join(',')} onchange={upload} disabled={uploading} />
					</label>
					{#if auth.user.avatar}<button type="button" class="btn btn-sm btn-ghost" onclick={removePicture}><Icon name="x" size={15} />Remove</button>{/if}
				</div>
				<p class="small muted m-0">PNG, JPEG, WebP or GIF up to 512 KB. It's cropped to a square. Your teachers see it (and your students, if you teach); other students don't.</p>
				{#if picError}<p class="alert alert-soft alert-error small m-0" role="alert"><Icon name="alert" size={15} />{picError}</p>{/if}
			</div>
		</div>
	</section>

	<section class="card card-border bg-base-100 shadow-sm p-4 sm:p-6" aria-labelledby="pw-h">
		<h2 id="pw-h" class="mt-0">Change password</h2>
		<form class="vstack" onsubmit={changePassword} novalidate>
			<div>
				<label for="pw-cur">Current password</label>
				<input id="pw-cur" class="input w-full" class:input-error={!!errors.current_password} type={show ? 'text' : 'password'} autocomplete="current-password" bind:value={current} required aria-invalid={!!errors.current_password} aria-describedby={errors.current_password ? 'pw-cur-e' : undefined} />
				{#if errors.current_password}<p id="pw-cur-e" class="field-error">{cap(errors.current_password)}</p>{/if}
			</div>
			<div>
				<label for="pw-new">New password</label>
				<input id="pw-new" class="input w-full" class:input-error={!!errors.new_password} type={show ? 'text' : 'password'} autocomplete="new-password" minlength="8" maxlength="128" bind:value={next} required aria-describedby="pw-new-h{errors.new_password ? ' pw-new-e' : ''}" aria-invalid={!!errors.new_password} />
				<p id="pw-new-h" class="small muted m-0 mt-1">At least 8 characters. A short sentence is easy to remember and hard to guess.</p>
				{#if errors.new_password}<p id="pw-new-e" class="field-error">{cap(errors.new_password)}</p>{/if}
			</div>
			<div>
				<label for="pw-again">New password again</label>
				<input id="pw-again" class="input w-full" class:input-error={mismatch || !!errors.again} type={show ? 'text' : 'password'} autocomplete="new-password" bind:value={again} required aria-invalid={mismatch} aria-describedby={mismatch || errors.again ? 'pw-again-e' : undefined} />
				{#if mismatch || errors.again}<p id="pw-again-e" class="field-error">{errors.again ?? "The two new passwords don't match."}</p>{/if}
			</div>
			<label class="m-0 flex items-center gap-2 font-normal small"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={show} />Show passwords</label>
			{#if errors._}<p class="alert alert-soft alert-error small m-0" role="alert"><Icon name="alert" size={15} />{errors._}</p>{/if}
			<div class="flex flex-wrap items-center gap-3">
				<button class="btn btn-primary" disabled={saving || !current || next.length < 8 || mismatch}>{#if saving}<span class="loading loading-spinner loading-sm"></span>{/if}Change password</button>
				<span class="small muted">Your other devices will be signed out.</span>
			</div>
			<p class="small m-0"><a href="/forgot-password">Forgot your current password?</a></p>
		</form>
	</section>
{/if}
