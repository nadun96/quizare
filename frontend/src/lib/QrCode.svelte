<script lang="ts">
	// QR codes are rendered in the browser; no server CPU is spent (stack sheet).
	import QRCode from 'qrcode';
	let { text, size = 320 }: { text: string; size?: number } = $props();
	let canvas = $state<HTMLCanvasElement>();
	$effect(() => {
		if (canvas && text) QRCode.toCanvas(canvas, text, { width: size, margin: 2, errorCorrectionLevel: 'M' });
	});
</script>

<figure aria-label={'QR code for ' + text} style="margin:0">
	<canvas bind:this={canvas} aria-hidden="true" style="max-width:100%;height:auto;background:#fff;border-radius:8px"></canvas>
</figure>
