<script lang="ts">
	// An option's shape (D-150).
	import { INK, markFor } from './toon';

	let { index, size = 24, fill = INK }: { index: number; size?: number; fill?: string } = $props();
</script>

<svg viewBox="0 0 24 24" width={size} height={size} aria-hidden="true"><path d={markFor(index).path} {fill} /></svg>
