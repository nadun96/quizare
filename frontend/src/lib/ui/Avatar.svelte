<script lang="ts">
	// A person's profile picture (D-49), or their initial on a neutral disc.
	// The image URL carries the picture's version, so browsers cache it for
	// good and fetch a new one only after a change. The name always appears
	// nearby, so the picture is decorative to screen readers.
	let { id, name, avatar = '', size = 32 }: { id: string; name: string; avatar?: string; size?: number } = $props();
	let failed = $state(false);
	$effect(() => {
		void avatar;
		failed = false;
	});
	const initial = $derived((name.trim()[0] ?? '?').toUpperCase());
</script>

<span class="avatar {avatar && !failed ? '' : 'avatar-placeholder'}" aria-hidden="true">
	<span class="grid place-items-center overflow-hidden rounded-full bg-neutral text-neutral-content" style:width="{size}px" style:height="{size}px" style:font-size="{Math.round(size * 0.42)}px">
		{#if avatar && !failed}
			<img src="/api/auth/users/{id}/avatar?v={avatar}" alt="" width={size} height={size} loading="lazy" decoding="async" onerror={() => (failed = true)} />
		{:else}
			<span class="leading-none font-semibold">{initial}</span>
		{/if}
	</span>
</span>
