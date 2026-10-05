<script lang="ts">
	import { goto } from '$app/navigation';
	import Icon from '$lib/ui/Icon.svelte';
	import { flyIn } from '$lib/ui/motion';

	let code = $state('');
	let kind = $state<'session' | 'classroom' | 'poll'>('session');
	function submit(e: SubmitEvent) {
		e.preventDefault();
		const c = code.trim().toUpperCase();
		if (c) goto((kind === 'session' ? '/j/' : kind === 'poll' ? '/p/' : '/c/') + encodeURIComponent(c));
	}
</script>

<div class="narrow">
	<form class="card card-border vstack bg-base-100 p-5 shadow-sm sm:p-7" onsubmit={submit} in:flyIn>
		<div class="mx-auto grid size-14 place-items-center rounded-2xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="qr" size={28} /></div>
		<h1 class="center">Join</h1>
		<div class="join w-full" role="radiogroup" aria-label="What are you joining?">
			<button type="button" role="radio" aria-checked={kind === 'session'} class="btn join-item flex-1" class:btn-primary={kind === 'session'} onclick={() => (kind = 'session')}>A quiz</button>
			<button type="button" role="radio" aria-checked={kind === 'poll'} class="btn join-item flex-1" class:btn-primary={kind === 'poll'} onclick={() => (kind = 'poll')}>A poll</button>
			<button type="button" role="radio" aria-checked={kind === 'classroom'} class="btn join-item flex-1" class:btn-primary={kind === 'classroom'} onclick={() => (kind = 'classroom')}>A classroom</button>
		</div>
		<div>
			<label for="code">Code from your teacher</label>
			<input
				class="input input-lg code-input w-full"
				id="code"
				bind:value={() => code, (v) => (code = v.toUpperCase())}
				autocapitalize="characters"
				autocomplete="off"
				spellcheck="false"
				inputmode="text"
				maxlength="12"
				required
				placeholder="K7Q2XM"
			/>
		</div>
		<button class="btn btn-primary btn-lg w-full" disabled={!code.trim()}>Continue<Icon name="arrow-right" size={18} /></button>
		<p class="small muted center">Scanning the QR code on the board does the same thing.</p>
	</form>
</div>

<style>
	.code-input { font-family: var(--font-mono); font-size: 1.5rem; letter-spacing: 0.25em; text-align: center; text-transform: uppercase; }
	.code-input::placeholder { letter-spacing: 0.25em; opacity: 0.35; }
</style>
