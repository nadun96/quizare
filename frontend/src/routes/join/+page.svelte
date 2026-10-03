<script lang="ts">
	import { goto } from '$app/navigation';
	let code = $state('');
	let kind = $state<'session' | 'classroom'>('session');
	function submit(e: SubmitEvent) {
		e.preventDefault();
		const c = code.trim().toUpperCase();
		if (c) goto((kind === 'session' ? '/j/' : '/c/') + encodeURIComponent(c));
	}
</script>

<div class="narrow">
	<form class="card stack" onsubmit={submit}>
		<h1>Join</h1>
		<fieldset class="row" style="border:none;padding:0">
			<legend class="sr-only">What are you joining?</legend>
			<label class="row" style="font-weight:400"><input type="radio" bind:group={kind} value="session" /> A quiz session</label>
			<label class="row" style="font-weight:400"><input type="radio" bind:group={kind} value="classroom" /> A classroom</label>
		</fieldset>
		<div>
			<label for="code">Code from your teacher</label>
			<input id="code" bind:value={code} autocapitalize="characters" autocomplete="off" required placeholder="e.g. K7Q2XM" />
		</div>
		<button class="primary">Continue</button>
		<p class="small muted">Scanning the QR code on the board does the same thing.</p>
	</form>
</div>
