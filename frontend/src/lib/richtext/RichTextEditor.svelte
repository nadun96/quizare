<script lang="ts">
	import { tr } from '$lib/i18n/i18n.svelte';
	// Rich text editor for question text and feedback (D-38). Tiptap loads on
	// first use (teachers only); `value` is always Markdown in our dialect
	// (./syntax.ts). A plain-text value (format '') is converted on load with
	// every character kept literal.
	import { onDestroy, onMount } from 'svelte';
	import type { Editor } from '@tiptap/core';
	import type { MathEdit } from './editor';
	import type { TextFormat } from './syntax';
	import Icon from '../ui/Icon.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { promptDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import { IMAGE_TYPES, imageFiles, uploadImage } from './images';

	let {
		value = $bindable(''),
		format = 'markdown',
		id,
		label = 'Text',
		placeholder = '',
		blanks = false,
		compact = false,
		required = false,
		onlink
	}: {
		value?: string;
		format?: TextFormat;
		id: string;
		label?: string;
		placeholder?: string;
		blanks?: boolean;
		compact?: boolean;
		required?: boolean;
		/** Picks a file to link to (Google Drive, D-71); its name becomes the link text. */
		onlink?: () => Promise<{ text: string; href: string } | null>;
	} = $props();
	let linking = $state(false);
	async function linkFile() {
		if (!onlink || !editor) return;
		linking = true;
		try {
			const l = await onlink();
			if (!l) return;
			const text = editor.state.selection.empty ? l.text : editor.state.doc.textBetween(editor.state.selection.from, editor.state.selection.to);
			run((c) => c.insertContent({ type: 'text', text, marks: [{ type: 'link', attrs: { href: l.href } }] }).unsetMark('link'));
		} finally {
			linking = false;
		}
	}

	let host = $state<HTMLElement>();
	let editor = $state<Editor | null>(null);
	let failed = $state(false);
	let source = $state(false); // raw Markdown view
	let tick = $state(0); // bumps on every transaction, so toolbar state re-reads
	let panel = $state<null | { kind: 'link'; href: string } | { kind: 'math'; latex: string; display: boolean; pos: number | null }>(null);
	let panelInput = $state<HTMLInputElement>();

	onMount(async () => {
		try {
			const { createEditor, markdownOf } = await import('./editor');
			// svelte-ignore state_referenced_locally
			const initial = value;
			editor = createEditor({
				element: host!,
				value: initial,
				format,
				placeholder,
				onchange: (md) => (value = md),
				// Later, not now: a blur can arrive while Svelte is drawing (a dialog taking focus), when state mustn't change.
				onstate: () => queueMicrotask(() => tick++),
				onmath: (m: MathEdit) => openMath(m.latex, m.display, m.pos),
				onimages: (files, at) => addImages(files, at)
			});
			editor.view.dom.setAttribute('id', id);
			editor.view.dom.setAttribute('aria-label', label);
			editor.view.dom.setAttribute('aria-multiline', 'true');
			editor.view.dom.setAttribute('role', 'textbox');
			value = format === 'markdown' ? initial : markdownOf(editor);
		} catch (e) {
			console.error(e);
			failed = true; // fall back to the Markdown textarea
			source = true;
		}
	});
	onDestroy(() => editor?.destroy());

	const is = (name: string, attrs?: Record<string, unknown>) => (void tick, editor?.isActive(name, attrs) ?? false);
	const can = (fn: (e: Editor) => boolean) => (void tick, editor ? fn(editor) : false);
	const run = (fn: (c: ReturnType<Editor['chain']>) => ReturnType<Editor['chain']>) => editor && fn(editor.chain().focus()).run();

	const block = $derived.by(() => {
		void tick;
		for (const l of [2, 3, 4]) if (editor?.isActive('heading', { level: l })) return 'h' + l;
		return 'p';
	});
	function setBlock(v: string) {
		if (v === 'p') run((c) => c.setParagraph());
		else run((c) => c.toggleHeading({ level: Number(v[1]) as 2 | 3 | 4 }));
	}

	function nextBlank(): string {
		const used = new Set([...value.matchAll(/\[\[(\d{1,2})\]\]/g)].map((m) => Number(m[1])));
		let n = 1;
		while (used.has(n)) n++;
		return String(n);
	}

	async function openPanel(p: NonNullable<typeof panel>) {
		panel = p;
		await Promise.resolve();
		panelInput?.focus();
		panelInput?.select();
	}
	function openLink() {
		openPanel({ kind: 'link', href: (editor?.getAttributes('link').href as string) ?? 'https://' });
	}
	// Pictures (D-113): picked, dropped or pasted; each is uploaded, then put
	// where it was dropped (or at the cursor) with a description for students
	// who can't see it.
	let picker = $state<HTMLInputElement>();
	let uploading = $state(0);
	async function addImages(files: File[], at: number | null = null) {
		for (const file of imageFiles(files)) {
			uploading++;
			try {
				const img = await uploadImage(file);
				const alt = await promptDialog({ title: tr("Describe the picture"), body: tr("Students who use a screen reader, or whose picture doesn’t load, read this instead."), label: tr("Description"), value: file.name.replace(/\.[^.]+$/, '').replace(/[-_]+/g, ' '), allowEmpty: true, maxlength: 200, confirm: tr("Insert") });
				if (alt === null) continue;
				const node = { type: 'image', attrs: { src: img.url, alt: alt.trim() } };
				if (at !== null) run((c) => c.insertContentAt(at, node));
				else run((c) => c.insertContent(node));
			} catch (e) {
				toast(e instanceof Error ? e.message : tr("Could not add the picture"), 'error');
			} finally {
				uploading--;
			}
		}
	}

	function openMath(latex = '', display = false, pos: number | null = null) {
		openPanel({ kind: 'math', latex, display, pos });
	}
	function applyPanel(e?: Event) {
		e?.preventDefault();
		if (!editor || !panel) return;
		if (panel.kind === 'link') {
			const href = panel.href.trim();
			if (!href || href === 'https://') run((c) => c.extendMarkRange('link').unsetLink());
			else if (!/^(https?:\/\/|mailto:)/i.test(href)) return; // keep the panel open; the hint explains
			else if (editor.state.selection.empty && !editor.isActive('link'))
				// Nothing selected: insert the address itself as the link text.
				run((c) => c.insertContent({ type: 'text', text: href, marks: [{ type: 'link', attrs: { href } }] }).unsetMark('link'));
			else run((c) => c.extendMarkRange('link').setLink({ href }));
		} else {
			const latex = panel.latex.trim();
			const p = panel;
			const pos = p.pos ?? undefined;
			if (pos !== undefined && !latex) run((c) => (p.display ? c.deleteBlockMath({ pos }) : c.deleteInlineMath({ pos })));
			else if (pos !== undefined) run((c) => (p.display ? c.updateBlockMath({ latex, pos }) : c.updateInlineMath({ latex, pos })));
			else if (latex) run((c) => (p.display ? c.insertBlockMath({ latex }) : c.insertInlineMath({ latex })));
		}
		panel = null;
	}
	function panelKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			panel = null;
			editor?.commands.focus();
		}
	}

	function toggleSource() {
		if (failed) return;
		if (source && editor) editor.commands.setContent(value, { contentType: 'markdown', emitUpdate: false });
		source = !source;
	}

	// Toolbar buttons must not take focus from the text: otherwise the first
	// keystrokes after a click land on the button while Tiptap refocuses.
	function keepFocus(e: MouseEvent) {
		if ((e.target as HTMLElement).closest('button')) e.preventDefault();
	}

	const linkOk = $derived(panel?.kind === 'link' ? /^(https?:\/\/|mailto:)/i.test(panel.href.trim()) || !panel.href.trim() || panel.href.trim() === 'https://' : true);
</script>

<div class="rte" class:compact class:empty-required={required && !value.trim()}>
	<div class="bar" role="toolbar" tabindex="-1" aria-label={tr("{0} formatting", [label])} onmousedown={keepFocus}>
		{#if !source}
			<select class="select select-sm w-auto min-w-36" aria-label={tr("Text style")} value={block} onchange={(e) => setBlock(e.currentTarget.value)} disabled={!editor}>
				<option value="p">{tr("Paragraph")}</option>
				<option value="h2">{tr("Heading")}</option>
				<option value="h3">{tr("Subheading")}</option>
				<option value="h4">{tr("Small heading")}</option>
			</select>
			<span class="sep"></span>
			<IconBtn icon="bold" label={tr("Bold")} hint={tr("Bold (Ctrl+B)")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('bold')} onclick={() => run((c) => c.toggleBold())} />
			<IconBtn icon="italic" label={tr("Italic")} hint={tr("Italic (Ctrl+I)")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('italic')} onclick={() => run((c) => c.toggleItalic())} />
			<IconBtn icon="underline" label={tr("Underline")} hint={tr("Underline (Ctrl+U)")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('underline')} onclick={() => run((c) => c.toggleUnderline())} />
			<IconBtn icon="strike" label={tr("Strikethrough")} hint={tr("Strikethrough (Ctrl+Shift+S)")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('strike')} onclick={() => run((c) => c.toggleStrike())} />
			<IconBtn icon="code" label={tr("Inline code")} hint={tr("Inline code (Ctrl+E)")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('code')} onclick={() => run((c) => c.toggleCode())} />
			<IconBtn icon="link" label={tr("Link")} hint={tr("Link (Ctrl+K)")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('link')} onclick={openLink} />
			<IconBtn icon="image" label={tr("Picture")} hint={tr("Picture: pick one, or drop or paste it into the text")} class="btn-ghost btn-sm" size={16} tip="bottom" disabled={uploading > 0} onclick={() => picker?.click()} />
			<input type="file" class="hidden" accept={IMAGE_TYPES.join(',')} multiple bind:this={picker} onchange={(e) => { addImages([...(e.currentTarget.files ?? [])]); e.currentTarget.value = ''; }} />
			{#if uploading}<span class="loading loading-spinner loading-xs" aria-label={tr("Uploading a picture")}></span>{/if}
			{#if onlink}<IconBtn icon="cloud" label={tr("Drive file")} hint={tr("Link a Google Drive file (shared by link)")} class="btn-ghost btn-sm" size={16} tip="bottom" disabled={linking} onclick={linkFile} />{/if}
			<span class="sep"></span>
			<IconBtn icon="list" label={tr("Bulleted list")} hint={tr("Bulleted list")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('bulletList')} onclick={() => run((c) => c.toggleBulletList())} />
			<IconBtn icon="list-ordered" label={tr("Numbered list")} hint={tr("Numbered list")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('orderedList')} onclick={() => run((c) => c.toggleOrderedList())} />
			<IconBtn icon="quote" label={tr("Quote")} hint={tr("Quote")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('blockquote')} onclick={() => run((c) => c.toggleBlockquote())} />
			<IconBtn icon="braces" label={tr("Code block")} hint={tr("Code block")} class="btn-ghost btn-sm" size={16} tip="bottom" aria-pressed={is('codeBlock')} onclick={() => run((c) => c.toggleCodeBlock())} />
			<IconBtn icon="minus" label={tr("Divider")} hint={tr("Divider")} class="btn-ghost btn-sm" size={16} tip="bottom" onclick={() => run((c) => c.setHorizontalRule())} />
			{#if !compact}
				<span class="sep"></span>
				<IconBtn icon="table" label={tr("Insert table")} hint={tr("Insert table")} class="btn-ghost btn-sm" size={16} tip="bottom" onclick={() => run((c) => c.insertTable({ rows: 3, cols: 3, withHeaderRow: true }))} />
				<IconBtn icon="sigma" label={tr("Inline math")} hint={tr("Inline math: Math, inline (type $$x^2$$ as a shortcut)")} class="btn-ghost btn-sm" size={16} tip="bottom" onclick={() => openMath('', false)} />
				<IconBtn icon="sigma-block" label={tr("Display math")} hint={tr("Display math: Math, on its own line")} class="btn-ghost btn-sm" size={16} tip="bottom" onclick={() => openMath('', true)} />
			{/if}
			{#if blanks}
				<span class="sep"></span>
				<span class="tooltip tooltip-bottom" data-tip={tr("Insert a blank (or type [[1]])")}><button type="button" class="btn btn-ghost btn-sm blank-btn" onclick={() => run((c) => c.insertContent({ type: 'blank', attrs: { n: nextBlank() } }))}><Icon name="plus" size={14} />{tr("Blank")}</button></span>
			{/if}
			<span class="grow"></span>
			<IconBtn icon="undo" label={tr("Undo")} hint={tr("Undo (Ctrl+Z)")} class="btn-ghost btn-sm" size={16} tip="bottom" disabled={!can((e) => e.can().undo())} onclick={() => run((c) => c.undo())} />
			<IconBtn icon="redo" label={tr("Redo")} hint={tr("Redo (Ctrl+Shift+Z)")} class="btn-ghost btn-sm" size={16} tip="bottom" disabled={!can((e) => e.can().redo())} onclick={() => run((c) => c.redo())} />
			<IconBtn icon="remove-format" label={tr("Clear formatting")} hint={tr("Clear formatting")} class="btn-ghost btn-sm" size={16} tip="bottom" onclick={() => run((c) => c.unsetAllMarks().clearNodes())} />
		{:else}
			<span class="small muted">{failed ? tr("Editor unavailable: editing Markdown directly.") : tr("Markdown")}</span>
			<span class="grow"></span>
		{/if}
		{#if !failed}<IconBtn icon="markdown" label={tr("Markdown")} hint={tr("Show the Markdown source")} class="btn-ghost btn-sm" size={16} tip="left" aria-pressed={source} onclick={toggleSource} />{/if}
	</div>

	{#if is('table') && !source}
		<div class="bar sub" role="toolbar" tabindex="-1" aria-label={tr("Table")} onmousedown={keepFocus}>
			<span class="small muted">{tr("Table:")}</span>
			<button type="button" class="btn btn-ghost btn-sm" onclick={() => run((c) => c.addRowAfter())}>{tr("+ Row")}</button>
			<button type="button" class="btn btn-ghost btn-sm" onclick={() => run((c) => c.addColumnAfter())}>{tr("+ Column")}</button>
			<button type="button" class="btn btn-ghost btn-sm" onclick={() => run((c) => c.deleteRow())}>{tr("− Row")}</button>
			<button type="button" class="btn btn-ghost btn-sm" onclick={() => run((c) => c.deleteColumn())}>{tr("− Column")}</button>
			<button type="button" class="btn btn-ghost btn-sm" onclick={() => run((c) => c.toggleHeaderRow())}>{tr("Header row")}</button>
			<button type="button" class="btn btn-ghost btn-sm" onclick={() => run((c) => c.deleteTable())}>{tr("Delete table")}</button>
		</div>
	{/if}

	{#if panel}
		<form class="bar sub" onsubmit={applyPanel}>
			{#if panel.kind === 'link'}
				<label class="small" for={id + '-panel'}>{tr("Link")}</label>
				<input class="input input-sm" id={id + '-panel'} bind:this={panelInput} bind:value={panel.href} onkeydown={panelKey} placeholder={tr("https://…")} />
				{#if !linkOk}<span class="small danger">{tr("Use an https://, http:// or mailto: address")}</span>{/if}
			{:else}
				<label class="small" for={id + '-panel'}>{tr("LaTeX")}</label>
				<input id={id + '-panel'} class="input input-sm mono" bind:this={panelInput} bind:value={panel.latex} onkeydown={panelKey} placeholder={tr("\\frac{a}{b}, x^2, \\sqrt{2}")} />
				<label class="small row" style="font-weight:400"><input type="checkbox" class="checkbox checkbox-sm" bind:checked={panel.display} disabled={panel.pos !== null} /> {tr("Own line")}</label>
			{/if}
			<button type="submit" class="btn btn-primary btn-sm">{panel.kind === 'math' && panel.pos !== null && !panel.latex.trim() ? tr("Remove") : tr("Apply")}</button>
			<button type="button" class="btn btn-sm" onclick={() => (panel = null)}>{tr("Cancel")}</button>
		</form>
	{/if}

	<div class="surface" class:hidden={source} bind:this={host} onkeydown={(e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); openLink(); } }} role="presentation"></div>
	{#if source}
		<textarea id={failed ? id : id + '-md'} class="textarea mono" aria-label={tr("{0} (Markdown)", [label])} bind:value rows={compact ? 3 : 6} {required} {placeholder}></textarea>
	{/if}
</div>

<style>
	.rte { border: 1px solid var(--color-field); border-radius: 8px; background: var(--surface); }
	.rte:focus-within { border-color: var(--accent); box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 25%, transparent); }
	.bar { display: flex; flex-wrap: wrap; gap: 0.2rem; align-items: center; padding: 0.3rem; border-bottom: 1px solid var(--color-base-300); background: var(--bg); border-radius: 8px 8px 0 0; }
	.bar.sub { border-radius: 0; gap: 0.4rem; }
	.bar.sub input:not([type='checkbox']) { flex: 1; min-width: 10rem; width: auto; padding: 0.3rem 0.5rem; }
	.bar :global(.btn) { min-width: 2rem; min-height: 2rem; padding: 0 0.45rem; font-size: 0.9rem; font-weight: 600; }
	.bar :global(.btn[aria-pressed='true']) { background: color-mix(in oklab, var(--color-primary) 16%, transparent); color: var(--color-primary); }
	.bar select { width: auto; padding: 0.25rem 0.4rem; font-size: 0.9rem; }
	.sep { width: 1px; align-self: stretch; background: var(--color-base-300); margin: 0 0.2rem; }
	.grow { flex: 1; }
	.blank-btn { font-weight: 600; }
	.danger { color: var(--danger); }
	.mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
	.hidden { display: none; }
	textarea { border: 0; border-radius: 0 0 8px 8px; width: 100%; min-height: 7rem; }
	.surface :global(.tiptap) { min-height: 6rem; padding: 0.6rem 0.75rem; outline: none; overflow-wrap: anywhere; }
	.compact .surface :global(.tiptap) { min-height: 2.6rem; }
	.surface :global(.tiptap p) { margin: 0 0 0.5em; }
	.surface :global(.tiptap > :last-child) { margin-bottom: 0; }
	.surface :global(.tiptap h2) { font-size: 1.25em; margin: 0.5em 0 0.3em; }
	.surface :global(.tiptap h3) { font-size: 1.12em; margin: 0.5em 0 0.3em; }
	.surface :global(.tiptap h4) { font-size: 1em; margin: 0.5em 0 0.3em; }
	.surface :global(.tiptap ul), .surface :global(.tiptap ol) { padding-left: 1.4em; margin: 0 0 0.5em; }
	.surface :global(.tiptap blockquote) { margin: 0 0 0.5em; padding: 0.2em 0.9em; border-left: 3px solid var(--color-base-300); color: var(--muted); }
	.surface :global(.tiptap code) { font-family: ui-monospace, Consolas, monospace; font-size: 0.92em; background: var(--bg); border: 1px solid var(--color-base-300); border-radius: 4px; padding: 0.05em 0.3em; }
	.surface :global(.tiptap pre) { background: var(--bg); border: 1px solid var(--color-base-300); border-radius: 8px; padding: 0.6em; overflow-x: auto; }
	.surface :global(.tiptap pre code) { border: 0; padding: 0; background: none; }
	.surface :global(.tiptap table) { border-collapse: collapse; margin: 0 0 0.5em; }
	.surface :global(.tiptap th), .surface :global(.tiptap td) { border: 1px solid var(--color-base-300); padding: 0.3em 0.5em; min-width: 3em; vertical-align: top; position: relative; }
	.surface :global(.tiptap th) { background: var(--bg); }
	.surface :global(.tiptap .selectedCell) { background: color-mix(in srgb, var(--accent) 18%, transparent); }
	.surface :global(.tiptap .tableWrapper) { overflow-x: auto; }
	.surface :global(.tiptap hr) { border: 0; border-top: 1px solid var(--color-base-300); }
	.surface :global(.tiptap a) { color: var(--accent); }
	.surface :global(.blank-chip) { font-family: ui-monospace, Consolas, monospace; font-size: 0.85em; padding: 0.05em 0.35em; border-radius: 4px; background: var(--warn-bg); color: var(--warn); border: 1px solid currentColor; cursor: default; }
	.surface :global(.ProseMirror-selectednode) { outline: 2px solid var(--accent); }
	.surface :global(img.rt-img) { max-width: 100%; max-height: 22rem; display: inline-block; vertical-align: middle; }
	.surface :global(img.rt-img.ProseMirror-selectednode) { outline: 3px solid var(--color-primary); }
	.surface :global(.tiptap-mathematics-render--editable) { cursor: pointer; border-radius: 4px; }
	.surface :global(.tiptap-mathematics-render--editable:hover) { background: var(--bg); }
	.surface :global(.block-math-error), .surface :global(.inline-math-error) { color: var(--danger); }
	.surface :global(p.is-editor-empty:first-child::before) { content: attr(data-placeholder); color: var(--muted); float: left; height: 0; pointer-events: none; }
</style>
