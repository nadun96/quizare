<script lang="ts">
	// Real-time whiteboard (V2-09, D-47, D-52). Pen lines are sent in pieces
	// every 200 ms while drawing, so viewers see them grow; shapes and text are
	// sent when finished. Pieces go out one request at a time, in order, and
	// are drawn on an overlay until saved so nothing flickers. New strokes are
	// drawn on top of the board; only removals and resizes redraw it all.
	import { onDestroy } from 'svelte';
	import Icon, { type IconName } from '../ui/Icon.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { confirmDialog } from '../ui/dialog.svelte';
	import { toast } from '../ui/toast.svelte';
	import { BOARD_H, BOARD_W, type BoardState, drawStroke, hits, INKS, type NewStroke, newGesture, render, round, SIZES, type Stroke, StrokeQueue, thin, toPNG, type Tool } from './strokes.svelte';

	type Client = {
		add: (strokes: NewStroke[]) => Promise<Stroke[]>;
		erase: (ids: number[], gesture?: string) => Promise<number[]>;
		clear?: () => Promise<void>;
	};
	let { board, client, teacher = false, title = 'whiteboard' }: { board: BoardState; client: Client; teacher?: boolean; title?: string } = $props();

	let tool = $state<Tool | 'eraser'>('pen');
	let color = $state(INKS[0][0]);
	let size = $state(SIZES[1][0]);
	let wrap = $state<HTMLDivElement>();
	let main = $state<HTMLCanvasElement>();
	let over = $state<HTMLCanvasElement>();
	let px = $state({ w: 0, h: 0 });
	let hidden = $state.raw(new Set<number>()); // erased locally, waiting for the server
	let draft = $state.raw<NewStroke | null>(null); // being drawn; its points array grows in place
	let textAt = $state<{ x: number; y: number; value: string } | null>(null);
	let textInput = $state<HTMLInputElement>();
	// The press on the canvas keeps focus there; move it to the text box once it exists.
	$effect(() => {
		if (textInput) setTimeout(() => textInput?.focus(), 0);
	});
	const mine: string[] = []; // own gestures this session, for undo

	const TOOLS: [Tool | 'eraser', IconName, string][] = [
		['pen', 'pencil', 'Pen'], ['highlighter', 'brush', 'Highlighter'], ['line', 'minus', 'Line'], ['arrow', 'arrow-right', 'Arrow'],
		['rect', 'square', 'Rectangle'], ['ellipse', 'circle', 'Ellipse'], ['text', 'type', 'Text'], ['eraser', 'eraser', 'Eraser']
	];

	// Size the canvases to their box at the device's pixel density.
	$effect(() => {
		if (!wrap) return;
		const measure = () => {
			toolbarH = toolbarEl?.offsetHeight ?? 0;
			const r = wrap!.getBoundingClientRect();
			const dpr = Math.min(window.devicePixelRatio || 1, 2);
			px = { w: Math.round(r.width * dpr), h: Math.round(r.width * dpr * (BOARD_H / BOARD_W)) };
		};
		if (typeof ResizeObserver === 'undefined') {
			measure();
			window.addEventListener('resize', measure);
			return () => window.removeEventListener('resize', measure);
		}
		const ro = new ResizeObserver(measure);
		ro.observe(wrap);
		return () => ro.disconnect();
	});
	// The board: strokes that were only appended are drawn on top; anything
	// else (a removal, a clear, a reload, a resize) redraws it all.
	let shown: { count: number; rev: number; hidden: Set<number> | null; w: number; h: number } = { count: 0, rev: -1, hidden: null, w: 0, h: 0 };
	$effect(() => {
		const ctx = main?.getContext('2d');
		const list = board.strokes;
		const h = hidden;
		const { w, h: ht } = px;
		if (!ctx || !w) return;
		if (shown.rev === board.rev && shown.hidden === h && shown.w === w && shown.h === ht && list.length >= shown.count) {
			ctx.setTransform(w / BOARD_W, 0, 0, ht / BOARD_H, 0, 0);
			for (let i = shown.count; i < list.length; i++) if (!h.has(list[i].id)) drawStroke(ctx, list[i]);
		} else render(ctx, h.size ? list.filter((s) => !h.has(s.id)) : list, w, ht);
		shown = { count: list.length, rev: board.rev, hidden: h, w, h: ht };
	});
	// The overlay (unsaved pieces and the stroke being drawn): at most once a frame.
	const frame = typeof requestAnimationFrame === 'function' ? requestAnimationFrame : (f: () => void) => setTimeout(f, 16);
	let painting = false;
	function paintOver() {
		painting = false;
		const ctx = over?.getContext('2d');
		if (!ctx || !px.w) return;
		ctx.setTransform(1, 0, 0, 1, 0, 0);
		ctx.clearRect(0, 0, px.w, px.h);
		ctx.setTransform(px.w / BOARD_W, 0, 0, px.h / BOARD_H, 0, 0);
		for (const s of queue.pending) drawStroke(ctx, s);
		if (draft) drawStroke(ctx, draft);
	}
	$effect(() => {
		void queue.pending;
		void draft;
		void px.w;
		if (!painting) {
			painting = true;
			frame(paintOver);
		}
	});

	function at(e: PointerEvent): [number, number] {
		const r = over!.getBoundingClientRect();
		return [((e.clientX - r.left) / r.width) * BOARD_W, ((e.clientY - r.top) / r.height) * BOARD_H];
	}

	const queue = new StrokeQueue(
		(s) => client.add(s),
		(saved) => board.apply({ type: 'board', op: 'add', strokes: saved }),
		(e) => toast(e instanceof Error ? e.message : 'Could not draw', 'error')
	);
	const send = (s: NewStroke) => queue.push(s);

	// ---- pen and highlighter: pieces every 200 ms ----
	let sentUpTo = 0;
	let flushTimer: ReturnType<typeof setInterval> | null = null;
	function flushPiece(final = false) {
		if (!draft) return;
		const pts = draft.points;
		const from = Math.max(0, sentUpTo - 2); // repeat the last point so pieces join
		if (pts.length - from < (final ? 2 : 4)) return;
		const piece: NewStroke = { ...draft, points: round(thin(pts.slice(from))) };
		sentUpTo = pts.length;
		send(piece);
	}

	let start: [number, number] = [0, 0];
	let erasing = new Set<number>();
	function down(e: PointerEvent) {
		if (!board.canDraw || e.button > 0) return;
		e.preventDefault();
		over!.setPointerCapture(e.pointerId);
		const [x, y] = at(e);
		start = [x, y];
		if (tool === 'text') {
			textAt = { x, y, value: '' };
			return;
		}
		if (tool === 'eraser') {
			erasing = new Set();
			erase(x, y);
			return;
		}
		const gesture = newGesture();
		mine.push(gesture);
		const line = tool === 'pen' || tool === 'highlighter';
		draft = { gesture, tool, color, size, points: line ? [x, y] : [x, y, x, y] };
		if (line) {
			sentUpTo = 0;
			flushTimer = setInterval(() => flushPiece(), 200);
		}
	}
	function move(e: PointerEvent) {
		if (!board.canDraw) return;
		if (tool === 'eraser' && e.buttons) return erase(...at(e));
		if (!draft) return;
		const [x, y] = at(e);
		if (draft.tool === 'pen' || draft.tool === 'highlighter') {
			// Coalesced events keep fast strokes smooth. Points are appended in
			// place; a new draft object tells the overlay to repaint.
			const evs = (e.getCoalescedEvents?.() ?? []).length ? e.getCoalescedEvents() : [e];
			for (const ev of evs) draft.points.push(...at(ev));
			draft = { ...draft };
		} else draft = { ...draft, points: [start[0], start[1], x, y] };
	}
	function up() {
		if (tool === 'eraser') {
			const ids = [...erasing];
			erasing = new Set();
			if (ids.length) eraseIds(ids);
			return;
		}
		if (!draft) return;
		if (flushTimer) clearInterval(flushTimer);
		flushTimer = null;
		if (draft.tool === 'pen' || draft.tool === 'highlighter') flushPiece(true);
		else {
			const [x1, y1, x2, y2] = draft.points;
			if (Math.hypot(x2 - x1, y2 - y1) > 4) send({ ...draft, points: round(draft.points) });
			else mine.pop();
		}
		draft = null;
	}

	// ---- eraser: whole lines (every piece of a gesture); participants only their own ----
	function erase(x: number, y: number) {
		const hit: Stroke[] = [];
		for (const s of board.strokes) {
			if (erasing.has(s.id) || (!teacher && !board.mine(s))) continue;
			if (hits(s, x, y, 10)) hit.push(s);
		}
		if (!hit.length) return;
		for (const s of board.gestures(hit)) erasing.add(s.id);
		hidden = new Set([...hidden, ...erasing]);
	}
	async function eraseIds(ids: number[]) {
		try {
			const gone = await client.erase(ids);
			board.apply({ type: 'board', op: 'remove', ids: gone });
		} catch {
			toast('Could not erase', 'error');
		} finally {
			hidden = new Set([...hidden].filter((id) => !ids.includes(id)));
		}
	}

	async function undo() {
		const g = mine.pop();
		if (!g) return toast('Nothing of yours to undo', 'info');
		try {
			// Pieces of the line may still be on their way: undo them too.
			await queue.idle();
			const gone = await client.erase([], g);
			board.apply({ type: 'board', op: 'remove', ids: gone });
		} catch {
			toast('Could not undo', 'error');
		}
	}
	async function clearAll() {
		if (!client.clear) return;
		if (!(await confirmDialog({ title: 'Clear the whole board?', body: 'Every mark disappears for everyone. This cannot be undone.', confirm: 'Clear board', danger: true }))) return;
		await client.clear();
		board.apply({ type: 'board', op: 'clear' });
	}
	function commitText() {
		const t = textAt;
		textAt = null;
		if (!t || !t.value.trim()) return;
		const gesture = newGesture();
		mine.push(gesture);
		send({ gesture, tool: 'text', color, size, points: round([t.x, t.y]), text: t.value.trim().slice(0, 200) });
	}
	// ---- full screen, for the teacher and participants alike (D-54) ----
	// The board fills the window at the largest 16:9 size that fits. Where the
	// browser allows it, the whole page also goes full screen (hiding the
	// browser's bars) rather than just the board, so dialogs and messages stay
	// visible over it. iPhones only allow the in-page view.
	let full = $state(false);
	let toolbarH = $state(0);
	let toolbarEl = $state<HTMLDivElement>();
	let ownNative = false; // we put the page in full screen, so we take it out again
	async function toggleFull() {
		if (full) return exitFull();
		full = true;
		const el = document.documentElement;
		if (el.requestFullscreen && !document.fullscreenElement) {
			try {
				await el.requestFullscreen({ navigationUI: 'hide' });
				ownNative = true;
			} catch {
				/* the in-page view still works */
			}
		}
	}
	function exitFull() {
		full = false;
		if (ownNative && document.fullscreenElement) document.exitFullscreen().catch(() => {});
		ownNative = false;
	}
	// Esc in the browser's own full screen leaves it: leave ours too.
	function fullscreenChange() {
		if (ownNative && !document.fullscreenElement) {
			ownNative = false;
			full = false;
		}
	}
	// The page behind mustn't scroll while the board covers it.
	$effect(() => {
		if (!full) return;
		const el = document.documentElement;
		const before = el.style.overflow;
		el.style.overflow = 'hidden';
		return () => {
			el.style.overflow = before;
		};
	});

	onDestroy(() => {
		if (flushTimer) clearInterval(flushTimer);
		if (full) exitFull();
	});
	async function exportPNG() {
		const blob = await toPNG(board.strokes);
		if (!blob) return toast('Export is not supported in this browser', 'warning');
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = `${title.replace(/[^\w-]+/g, '-').slice(0, 60) || 'whiteboard'}.png`;
		a.click();
		setTimeout(() => URL.revokeObjectURL(a.href), 5000);
	}
	let root = $state<HTMLDivElement>();
	// Ctrl+Z undoes while the focus is on the board's tools (or nowhere in particular).
	function keys(e: KeyboardEvent) {
		if (full && e.key === 'Escape' && !textAt) {
			e.preventDefault();
			return exitFull();
		}
		const a = document.activeElement;
		const here = !a || a === document.body || !!root?.contains(a);
		if (here && (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z' && board.canDraw && !textAt) {
			e.preventDefault();
			undo();
		}
	}
	const label = $derived(`Whiteboard with ${board.strokes.length} ${board.strokes.length === 1 ? 'mark' : 'marks'}${board.canDraw ? '. Draw with a mouse, pen or finger.' : ''}`);
</script>

<svelte:window onkeydown={keys} />
<svelte:document onfullscreenchange={fullscreenChange} />

<div class="wb" class:full role="group" aria-label="Whiteboard" bind:this={root} style:--tb-h="{toolbarH}px">
	<div class="toolbar" role="toolbar" aria-label="Whiteboard tools" bind:this={toolbarEl}>
		{#if board.canDraw}
			<div class="grp">
				{#each TOOLS as [t, icon, name] (t)}
					<IconBtn {icon} label={name} class="btn-sm {tool === t ? 'btn-primary' : ''}" aria-pressed={tool === t} tip="bottom" onclick={() => (tool = t)} />
				{/each}
			</div>
			<div class="grp" aria-label="Colour">
				{#each INKS as [c, name] (c)}
					<span class="tooltip tooltip-bottom" data-tip={name}><button type="button" class="swatch" class:on={color === c} style:background={c} aria-label={name} aria-pressed={color === c} onclick={() => (color = c)}></button></span>
				{/each}
			</div>
			<div class="grp" aria-label="Size">
				{#each SIZES as [n, name] (n)}
					<span class="tooltip tooltip-bottom" data-tip="{name} line"><button type="button" class="btn btn-sm btn-square" class:btn-primary={size === n} aria-pressed={size === n} aria-label="{name} line" onclick={() => (size = n)}><span class="dot" style:width="{4 + n}px" style:height="{4 + n}px"></span></button></span>
				{/each}
			</div>
			<span class="tooltip tooltip-bottom" data-tip="Undo your last mark (Ctrl+Z)"><button type="button" class="btn btn-sm" onclick={undo}><Icon name="undo" size={16} />Undo</button></span>
			{#if teacher && client.clear}<button type="button" class="btn btn-sm btn-ghost text-error" onclick={clearAll}><Icon name="x" size={16} />Clear</button>{/if}
		{:else}
			<span class="small muted flex items-center gap-1"><Icon name="eye" size={14} />View only</span>
		{/if}
		<span class="spacer"></span>
		<IconBtn icon={full ? 'minimize' : 'maximize'} label={full ? 'Exit full screen' : 'Full screen'} hint={full ? 'Exit full screen (Esc)' : 'Full screen'} class="btn-sm btn-ghost" tip="left" aria-pressed={full} onclick={toggleFull} />
		<span class="tooltip tooltip-left" data-tip="Download the board as a PNG image"><button type="button" class="btn btn-sm btn-ghost" onclick={exportPNG}><Icon name="download" size={16} />PNG</button></span>
	</div>
	<div class="surface" bind:this={wrap} role="img" aria-label={label}>
		<canvas bind:this={main} width={px.w} height={px.h} aria-hidden="true"></canvas>
		<canvas bind:this={over} width={px.w} height={px.h} class="over" class:draw={board.canDraw} class:erase={tool === 'eraser'}
			aria-hidden="true" onpointerdown={down} onpointermove={move} onpointerup={up} onpointercancel={up}></canvas>
		{#if textAt}
			<input class="text-in" style:left="{(textAt.x / BOARD_W) * 100}%" style:top="{(textAt.y / BOARD_H) * 100}%" style:color={color}
				maxlength="200" aria-label="Text to add" bind:this={textInput} bind:value={textAt.value}
				onkeydown={(e) => { if (e.key === 'Enter') commitText(); else if (e.key === 'Escape') textAt = null; }} onblur={commitText} />
		{/if}
	</div>
</div>

<style>
	.wb { display: grid; gap: 0.5rem; }
	/* Full screen: above the page and its menus (z-40), below messages (z-50). */
	.wb.full {
		position: fixed; inset: 0; z-index: 45; align-content: start; overflow: auto;
		background: var(--color-base-200);
		padding: max(0.5rem, env(safe-area-inset-top)) max(0.5rem, env(safe-area-inset-right)) max(0.5rem, env(safe-area-inset-bottom)) max(0.5rem, env(safe-area-inset-left));
	}
	.wb.full .surface { justify-self: center; width: min(100%, calc((100dvh - var(--tb-h) - 1.5rem) * 16 / 9)); }
	.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; }
	.grp { display: flex; flex-wrap: wrap; gap: 0.25rem; padding-right: 0.5rem; border-right: 1px solid var(--color-base-300); }
	.swatch { width: 1.75rem; height: 1.75rem; border-radius: 999px; border: 2px solid var(--color-base-100); box-shadow: 0 0 0 1px var(--color-base-300); cursor: pointer; }
	.swatch.on { box-shadow: 0 0 0 2.5px var(--color-base-content); }
	.swatch:focus-visible { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	.dot { display: block; border-radius: 999px; background: currentColor; }
	.surface { position: relative; width: 100%; aspect-ratio: 16 / 9; border-radius: var(--radius-box); overflow: hidden; background: #fff; border: 1px solid var(--color-base-300); }
	.surface:focus-visible { outline: 3px solid var(--color-primary); outline-offset: 2px; }
	canvas { position: absolute; inset: 0; width: 100%; height: 100%; }
	.over.draw { cursor: crosshair; touch-action: none; }
	.over.erase { cursor: cell; }
	.text-in { position: absolute; min-width: 8rem; padding: 0.15rem 0.3rem; border: 1px dashed currentColor; background: rgb(255 255 255 / 90%); font: 600 1rem system-ui, sans-serif; }
</style>
