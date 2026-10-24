<script lang="ts">
	// A page's content blocks in the tutorial editor (D-74): add, edit,
	// reorder and remove. Pictures, videos and documents can come from Google
	// Drive when the teacher connected it (D-71).
	import { ApiError } from '../api';
	import { driveStatus, pickFiles, shareByLink, type PickKind } from '../drive';
	import RichTextEditor from '../richtext/RichTextEditor.svelte';
	import Icon from '../ui/Icon.svelte';
	import IconBtn from '../ui/IconBtn.svelte';
	import { toast } from '../ui/toast.svelte';
	import { BLOCKS, emptyBlock, move, type Block, type BlockType } from './tutorial';

	let { blocks = $bindable([]), errors = {} }: { blocks: Block[]; errors?: Record<string, string> } = $props();
	let drive = $state(false);
	$effect(() => {
		driveStatus().then((s) => (drive = s.connected && !s.reconnect));
	});

	const meta = (t: BlockType) => BLOCKS.find((b) => b.type === t)!;
	function add(t: BlockType) {
		blocks = [...blocks, emptyBlock(t)];
	}
	async function fromDrive(i: number) {
		const b = blocks[i];
		const kind: PickKind = b.type === 'image' ? 'images' : 'any';
		try {
			const [f] = await pickFiles({ kind, title: b.type === 'image' ? 'Choose a picture' : b.type === 'video' ? 'Choose a video' : 'Choose a document' });
			if (!f) return;
			const shared = await shareByLink(f.id);
			blocks[i] = { ...b, url: shared.image_url ?? shared.web_link, alt: b.alt || (b.type === 'image' ? shared.name.replace(/\.[a-z0-9]+$/i, '') : b.alt), text: b.text || (b.type === 'image' ? b.text : shared.name) };
			toast('Shared by link, so students can open it without a Google account', 'info');
		} catch (e) {
			toast(e instanceof ApiError ? e.message : e instanceof Error ? e.message : 'Google Drive didn’t answer', 'error');
		}
	}
</script>

<div class="vstack gap-3">
	{#each blocks as b, i (i)}
		{@const m = meta(b.type)}
		<div class="block-card" class:bad={errors['blocks.' + i]}>
			<div class="flex items-center gap-2">
				<Icon name={m.icon} size={16} />
				<strong class="small">{m.label}</strong>
				<span class="spacer"></span>
				<IconBtn icon="arrow-up" label="Up" hint="Move this {m.label.toLowerCase()} up" class="btn-xs btn-ghost" disabled={i === 0} onclick={() => (blocks = move(blocks, i, -1))} />
				<IconBtn icon="arrow-down" label="Down" hint="Move this {m.label.toLowerCase()} down" class="btn-xs btn-ghost" disabled={i === blocks.length - 1} onclick={() => (blocks = move(blocks, i, 1))} />
				<IconBtn icon="trash" label="Remove" hint="Remove this {m.label.toLowerCase()}" class="btn-xs btn-ghost" onclick={() => (blocks = blocks.filter((_, j) => j !== i))} />
			</div>
			{#if b.type === 'text'}
				<RichTextEditor id={'blk-' + i} label="Text" bind:value={b.text} placeholder="Write the reading…" />
			{:else}
				<div class="flex flex-wrap gap-2">
					<input class="input input-sm flex-1" style="min-width:14rem" placeholder={b.type === 'link' ? 'https://…' : b.type === 'video' ? 'YouTube or Google Drive link' : b.type === 'document' ? 'Google Drive, Docs, Slides or Sheets link' : 'Picture link (Google Drive links work)'}
						aria-label="{m.label} link" bind:value={b.url} />
					{#if drive && b.type !== 'link'}<button type="button" class="btn btn-sm" onclick={() => fromDrive(i)}><Icon name="cloud" size={14} />From Drive</button>{/if}
				</div>
				{#if b.type === 'image'}<input class="input input-sm w-full" placeholder="Describe the picture, for people who can’t see it" aria-label="Picture description" bind:value={b.alt} />{/if}
				<input class="input input-sm w-full" placeholder={b.type === 'link' ? 'Link text' : 'Caption (optional)'} aria-label={b.type === 'link' ? 'Link text' : 'Caption'} bind:value={b.text} />
			{/if}
			{#if errors['blocks.' + i]}<p class="field-error m-0">{errors['blocks.' + i]}</p>{:else}<p class="small muted m-0">{m.hint}</p>{/if}
		</div>
	{/each}
	<div class="flex flex-wrap items-center gap-2">
		<span class="small muted">Add</span>
		{#each BLOCKS as b (b.type)}<button type="button" class="btn btn-sm" onclick={() => add(b.type)}><Icon name={b.icon} size={14} />{b.label}</button>{/each}
	</div>
</div>

<style>
	.block-card { display: grid; gap: 0.5rem; padding: 0.75rem; border: 1px solid var(--color-base-300); border-radius: 0.75rem; background: var(--color-base-100); }
	.block-card.bad { border-color: var(--color-error); }
	.block-card :global(.icon) { color: var(--color-primary); }
</style>
