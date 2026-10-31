<script lang="ts">
	import { goto } from '$app/navigation';
	import { auth } from '$lib/session.svelte';
	import { flyIn } from '$lib/ui/motion';
	import { t } from '$lib/i18n/i18n.svelte';

	$effect(() => {
		if (auth.loaded && auth.user) goto(auth.home(), { replaceState: true });
	});

	// The hero shows the moment the product is about: a join code on the
	// projector and a phone answering. Drawn with the theme's own colours, so
	// it suits every theme, light or dark, and costs no image request (NFR-09).
	// A fixed pattern that reads as a QR code; it isn't a scannable one.
	const N = 21;
	const finder = (x: number, y: number) => {
		for (const [ox, oy] of [[0, 0], [N - 7, 0], [0, N - 7]]) {
			const dx = x - ox, dy = y - oy;
			if (dx >= 0 && dx < 7 && dy >= 0 && dy < 7) return dx === 0 || dx === 6 || dy === 0 || dy === 6 || (dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4) ? 1 : 0;
		}
		return -1;
	};
	const cells: [number, number][] = [];
	for (let y = 0; y < N; y++)
		for (let x = 0; x < N; x++) {
			const f = finder(x, y);
			const near = (x < 8 && y < 8) || (x > N - 9 && y < 8) || (x < 8 && y > N - 9);
			if (f === 1 || (f === -1 && !near && ((x * 7 + y * 13 + x * y) % 5 < 2))) cells.push([x, y]);
		}

	const joined = ['AS', 'KF', 'NJ', 'RP', 'TD', 'MW'];

	const steps = $derived<[string, string][]>([
		[t('home.step1'), t('home.step1Body')],
		[t('home.step2'), t('home.step2Body')],
		[t('home.step3'), t('home.step3Body')]
	]);
</script>

<section class="page-container landing">
	<div class="intro">
		<div class="pitch" in:flyIn>
			<h1 class="hero-title">{t('home.title')}</h1>
			<p class="lead">{t('home.lead')}</p>
			<div class="mt-6 flex flex-wrap gap-3">
				<a class="btn btn-primary btn-lg" href="/register">{t('auth.createAccount')}</a>
				<a class="btn btn-lg" href="/login">{t('auth.login')}</a>
			</div>
			<p class="small muted mt-4 mb-0">{t('home.gotCode')} <a href="/join">{t('home.joinQuiz')}</a></p>
		</div>

		<div class="scene" role="img" aria-label="A projector shows the join code K7Q2XM with a QR code and 24 students joined; a phone shows question 3 of 8 with “Carbon dioxide” chosen.">
			<div class="board" aria-hidden="true">
				<div class="board-bar"><span>Science check-in</span><span class="joined"><i></i>24 joined</span></div>
				<div class="board-body">
					<svg class="qr" viewBox="-1 -1 {N + 2} {N + 2}" shape-rendering="crispEdges">
						<rect x="-1" y="-1" width={N + 2} height={N + 2} fill="var(--color-base-100)" />
						{#each cells as [x, y] (x + ',' + y)}<rect {x} {y} width="1" height="1" />{/each}
					</svg>
					<div class="code-block">
						<span class="code-label">Scan, or enter the code</span>
						<span class="code">K7Q2XM</span>
					</div>
				</div>
				<div class="board-foot">
					{#each joined as j (j)}<span class="face">{j}</span>{/each}<span class="more">+18</span>
				</div>
			</div>
			<div class="phone" aria-hidden="true" in:flyIn={{ delay: 180, y: 24 }}>
				<div class="phone-top"><span>Question 3 of 8</span><span class="timer">0:42</span></div>
				<div class="phone-bar"><i></i></div>
				<p class="phone-q">Which gas do plants take in for photosynthesis?</p>
				<ul class="opts">
					<li><b>A</b>Oxygen</li>
					<li class="picked"><b>B</b>Carbon dioxide</li>
					<li><b>C</b>Nitrogen</li>
				</ul>
				<span class="phone-next">Next</span>
			</div>
		</div>
	</div>

	<h2 class="lesson-title">{t('home.how')}</h2>
	<ol class="lesson">
		{#each steps as [title, body] (title)}
			<li>
				<h3>{title}</h3>
				<p class="small muted m-0">{body}</p>
			</li>
		{/each}
	</ol>
</section>

<style>
	.landing { padding-top: 2.5rem; padding-bottom: 2rem; }
	.intro { display: grid; gap: 2.5rem; align-items: center; grid-template-columns: minmax(0, 1fr) minmax(0, 1.05fr); }
	@media (max-width: 56rem) { .intro { grid-template-columns: minmax(0, 1fr); gap: 2rem; } }
	.hero-title { font-size: clamp(1.9rem, 1.2rem + 2.6vw, 3rem); line-height: 1.1; letter-spacing: -0.02em; margin-bottom: 1rem; max-width: 18ch; }
	.lead { font-size: 1.125rem; line-height: 1.55; color: var(--color-muted); max-width: 34rem; margin: 0; }

	/* The scene: the projector, with the phone in front of it. */
	.scene { display: grid; }
	.board { background: var(--color-base-100); border: 1px solid var(--color-base-300); border-radius: var(--radius-box); overflow: hidden; box-shadow: 0 1px 2px rgb(0 0 0 / 0.04), 0 12px 32px -12px rgb(0 0 0 / 0.18); }
	.board-bar { display: flex; justify-content: space-between; align-items: center; gap: 1rem; padding: 0.6rem 1rem; font-size: 0.85rem; font-weight: 650; border-bottom: 1px solid var(--color-base-300); background: var(--color-base-200); }
	.joined { display: inline-flex; align-items: center; gap: 0.4rem; color: var(--color-success); font-weight: 600; }
	.joined i { width: 0.5rem; height: 0.5rem; border-radius: 999px; background: currentColor; }
	.board-body { display: flex; align-items: center; gap: clamp(1rem, 3vw, 2rem); padding: clamp(1rem, 3vw, 1.75rem); }
	/* Students arriving, on the left of the strip the phone overlaps. */
	.board-foot { display: flex; align-items: center; padding: 0 clamp(1rem, 3vw, 1.75rem) 4.75rem; }
	.face { width: 1.9rem; height: 1.9rem; border-radius: 999px; display: grid; place-items: center; font-size: 0.65rem; font-weight: 700; color: var(--color-base-content); background: var(--color-base-200); border: 2px solid var(--color-base-100); margin-left: -0.45rem; }
	.face:first-child { margin-left: 0; }
	.more { margin-left: 0.5rem; font-size: 0.8rem; color: var(--color-muted); font-weight: 600; }
	.qr { width: clamp(5.5rem, 26%, 9rem); flex: none; fill: var(--color-base-content); }
	.code-block { display: grid; gap: 0.35rem; min-width: 0; }
	.code-label { font-size: 0.85rem; color: var(--color-muted); }
	/* The join code is the handshake between board and phone: set large, wide, in tabular figures. */
	.code { font-size: clamp(1.9rem, 1rem + 3.4vw, 3.4rem); font-weight: 800; letter-spacing: 0.14em; line-height: 1; font-variant-numeric: tabular-nums; color: var(--color-base-content); }

	.phone { position: relative; justify-self: end; margin: -4.25rem 1.25rem 0 0; width: clamp(12rem, 46%, 15rem); padding: 0.85rem 0.85rem 0.95rem; border-radius: 1.6rem; background: var(--color-base-100); border: 1px solid var(--color-base-300); outline: 0.4rem solid var(--color-base-content); outline-offset: -1px; box-shadow: 0 24px 40px -18px rgb(0 0 0 / 0.45); display: grid; gap: 0.55rem; font-size: 0.8rem; }
	.phone-top { display: flex; justify-content: space-between; align-items: center; font-weight: 650; }
	.timer { padding: 0.1rem 0.5rem; border-radius: 999px; border: 1px solid var(--color-base-300); font-variant-numeric: tabular-nums; }
	.phone-bar { height: 0.2rem; border-radius: 999px; background: var(--color-base-300); overflow: hidden; }
	.phone-bar i { display: block; height: 100%; width: 37.5%; background: var(--color-primary); }
	.phone-q { margin: 0.15rem 0 0; font-weight: 600; line-height: 1.35; }
	.opts { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
	.opts li { display: flex; align-items: center; gap: 0.5rem; padding: 0.4rem 0.5rem; border-radius: 0.6rem; border: 1px solid var(--color-base-300); }
	.opts b { display: grid; place-items: center; width: 1.3rem; height: 1.3rem; border-radius: 999px; font-size: 0.7rem; background: var(--color-base-200); }
	.opts .picked { border-color: var(--color-primary); background: color-mix(in oklab, var(--color-primary) 10%, var(--color-base-100)); font-weight: 600; }
	.opts .picked b { background: var(--color-primary); color: var(--color-primary-content); }
	.phone-next { justify-self: end; padding: 0.3rem 0.9rem; border-radius: var(--radius-field); background: var(--color-primary); color: var(--color-primary-content); font-weight: 650; }
	@media (max-width: 30rem) { .board-foot { display: none; } .board-body { padding-bottom: 2.5rem; } .phone { width: 74%; margin: -1.75rem 0.5rem 0 0; } }

	/* A real sequence, so it's numbered; a line ties the steps together. */
	.lesson-title { margin-top: 3.5rem; }
	.lesson { list-style: none; margin: 0; padding: 0; counter-reset: step; display: grid; gap: 1.5rem 2rem; grid-template-columns: repeat(3, minmax(0, 1fr)); }
	@media (max-width: 48rem) { .lesson { grid-template-columns: minmax(0, 1fr); } }
	.lesson li { counter-increment: step; position: relative; padding-top: 2.75rem; }
	.lesson li::before { content: counter(step); position: absolute; top: 0; left: 0; width: 2rem; height: 2rem; border-radius: 999px; display: grid; place-items: center; font-weight: 700; background: var(--color-primary); color: var(--color-primary-content); }
	.lesson li:not(:last-child)::after { content: ''; position: absolute; top: 1rem; left: 2.75rem; right: -1.25rem; height: 2px; background: var(--color-base-300); }
	@media (max-width: 48rem) { .lesson li:not(:last-child)::after { display: none; } }
	.lesson h3 { margin: 0 0 0.35rem; font-size: 1.05rem; }
</style>
