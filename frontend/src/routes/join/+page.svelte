<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	import { goto } from '$app/navigation';
	import Icon, { type IconName } from '$lib/ui/Icon.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { t, type Key } from '$lib/i18n/i18n.svelte';

	let code = $state('');
	let kind = $state<'session' | 'classroom' | 'poll' | 'meeting'>('session');
	const PREFIX = { session: '/j/', poll: '/p/', classroom: '/c/', meeting: '/m/' };
	const KINDS: { id: typeof kind; label: Key; icon: IconName }[] = [
		{ id: 'session', label: 'join.quiz', icon: 'check-circle' },
		{ id: 'poll', label: 'join.poll', icon: 'chart' },
		{ id: 'classroom', label: 'join.classroom', icon: 'users' },
		{ id: 'meeting', label: 'join.meeting', icon: 'video' }
	];
	function submit(e: SubmitEvent) {
		e.preventDefault();
		const c = code.trim().toUpperCase();
		if (c) goto(PREFIX[kind] + encodeURIComponent(c));
	}
</script>

<svelte:head><title>{tr("Join a quiz, poll, classroom or meeting · Classroom Quiz")}</title><meta name="description" content="Enter the code from your teacher, or scan the QR code on the board, to join a live quiz, poll, classroom or meeting." /></svelte:head>


<div class="narrow">
	<form class="card card-border vstack bg-base-100 p-5 shadow-sm sm:p-7" onsubmit={submit} in:flyIn>
		<div class="mx-auto grid size-14 place-items-center rounded-2xl bg-primary/10 text-primary" aria-hidden="true"><Icon name="qr" size={28} /></div>
		<h1 class="center">{t('join.title')}</h1>
		<!-- An icon and a word for each (D-111): read at a glance, and the word never wraps. -->
		<div class="kinds" role="radiogroup" aria-label={t('join.what')}>
			{#each KINDS as k (k.id)}
				<button type="button" role="radio" aria-checked={kind === k.id} class="kind" class:on={kind === k.id} onclick={() => (kind = k.id)}>
					<Icon name={k.icon} size={22} /><span>{t(k.label)}</span>
				</button>
			{/each}
		</div>
		<div>
			<label for="code">{t('join.code')}</label>
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
				placeholder={tr("K7Q2XM")}
			/>
		</div>
		<button class="btn btn-primary btn-lg w-full" disabled={!code.trim()}>{t('join.continue')}<Icon name="arrow-right" size={18} /></button>
		<p class="small muted center">{t('join.qr')}</p>
	</form>
</div>

<style>
	.kinds { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 2px; background: var(--color-base-300); border: 1px solid var(--color-base-300); }
	.kind { display: grid; justify-items: center; gap: 0.3rem; padding: 0.7rem 0.25rem; background: var(--color-base-200); color: var(--color-base-content); font-weight: 600; font-size: 0.85rem; line-height: 1.2; cursor: pointer; border: 0; }
	.kind span { white-space: nowrap; }
	.kind:hover { background: color-mix(in oklab, var(--color-primary) 8%, var(--color-base-200)); }
	.kind.on { background: var(--color-primary); color: var(--color-primary-content); }
	.kind:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; position: relative; }
	@media (max-width: 22rem) { .kinds { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
	.code-input { font-family: var(--font-mono); font-size: 1.5rem; letter-spacing: 0.25em; text-align: center; text-transform: uppercase; }
	.code-input::placeholder { letter-spacing: 0.25em; opacity: 0.35; }
</style>
