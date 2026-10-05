<script lang="ts">
	// A plain code editor: monospace, line numbers, Tab indents. Esc releases
	// the Tab key so keyboard users can still move on (WCAG 2.1.2 no keyboard trap).
	let { value = '', language = '', maxLength = 10000, disabled = false, label, onchange }: { value?: string; language?: string; maxLength?: number; disabled?: boolean; label: string; onchange: (v: string) => void } = $props();
	let text = $state('');
	let trap = $state(true);
	let area = $state<HTMLTextAreaElement>();
	let gutter = $state<HTMLElement>();
	let timer: ReturnType<typeof setTimeout> | null = null;
	let seeded = false;
	$effect.pre(() => {
		if (!seeded) {
			text = value;
			seeded = true;
		}
	});
	const lines = $derived(Math.max(1, text.split('\n').length));

	function emit() {
		if (timer) clearTimeout(timer);
		timer = setTimeout(() => onchange(text), 600);
	}
	function key(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			trap = false;
			return;
		}
		if (e.key !== 'Tab' || !trap || !area) return;
		e.preventDefault();
		const { selectionStart: s, selectionEnd: end } = area;
		if (e.shiftKey) {
			const lineStart = text.lastIndexOf('\n', s - 1) + 1;
			if (text.slice(lineStart, lineStart + 1) === '\t') {
				text = text.slice(0, lineStart) + text.slice(lineStart + 1);
				queueMicrotask(() => area?.setSelectionRange(Math.max(lineStart, s - 1), Math.max(lineStart, end - 1)));
			}
		} else {
			text = text.slice(0, s) + '\t' + text.slice(end);
			queueMicrotask(() => area?.setSelectionRange(s + 1, s + 1));
		}
		emit();
	}
</script>

<div class="code-box" class:disabled>
	<div class="code-bar small">
		<span class="font-semibold">{language || 'Code'}</span>
		<span class="spacer"></span>
		<span class="muted tabular">{text.length.toLocaleString()} / {maxLength.toLocaleString()}</span>
	</div>
	<div class="code-wrap">
		<pre class="gutter" aria-hidden="true" bind:this={gutter}>{Array.from({ length: lines }, (_, i) => i + 1).join('\n')}</pre>
		<textarea
			bind:this={area}
			bind:value={text}
			oninput={emit}
			onblur={() => onchange(text)}
			onkeydown={key}
			onfocus={() => (trap = true)}
			onscroll={() => gutter && area && (gutter.scrollTop = area.scrollTop)}
			spellcheck="false"
			autocapitalize="off"
			autocomplete="off"
			maxlength={maxLength}
			rows="10"
			{disabled}
			aria-label={label}
			aria-describedby="code-help"
		></textarea>
	</div>
	<p id="code-help" class="small muted m-0 px-3 py-1">Tab indents · Shift+Tab outdents · Esc, then Tab, to leave the editor</p>
</div>

<style>
	.code-box { border: 1px solid var(--color-field); border-radius: var(--radius-field); background: var(--color-base-100); overflow: hidden; }
	.code-box:focus-within { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.code-bar { display: flex; align-items: center; gap: 0.5rem; padding: 0.35rem 0.75rem; background: var(--color-base-200); border-bottom: 1px solid var(--color-base-300); }
	.code-wrap { display: flex; max-height: 26rem; }
	.gutter { margin: 0; padding: 0.6rem 0.5rem; text-align: right; color: var(--color-muted); background: var(--color-base-200); font: 0.9rem/1.5 var(--font-mono); user-select: none; overflow: hidden; min-width: 2.5rem; }
	textarea { flex: 1; min-height: 14rem; padding: 0.6rem 0.75rem; border: 0; outline: none; resize: vertical; font: 0.9rem/1.5 var(--font-mono); tab-size: 4; background: transparent; color: inherit; white-space: pre; overflow: auto; }
</style>
