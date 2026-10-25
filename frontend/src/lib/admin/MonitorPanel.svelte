<script lang="ts">
	// Monitoring tutoring, for the admin (D-82, TS-NFR-41): upload, download,
	// CPU, tracks, participants and reconnects, in total and per session, now
	// and over the last day or week. The media server's own figures are
	// measured when LiveKit's metrics are configured; each session's are
	// estimated (LiveKit reports nothing per room).
	import { ApiError } from '../api';
	import { TutorClient, tutoringConfig } from '../tutoring/client';
	import { fillGaps, mbps, type MonitorView } from '../tutoring/types';
	import { toast } from '../ui/toast.svelte';
	import TimeChart from './TimeChart.svelte';

	const client = new TutorClient();
	let enabled = $state<boolean | null>(null);
	let range = $state<'24h' | '7d'>('24h');
	let v = $state<MonitorView | null>(null);

	async function load() {
		v = await client.get<MonitorView>(`/api/admin/monitor?range=${range}`);
	}
	$effect(() => {
		range; // reload when the range changes
		let t: ReturnType<typeof setInterval> | undefined;
		tutoringConfig().then((c) => {
			enabled = c.enabled;
			if (!c.enabled) return;
			load().catch((e) => toast(e instanceof ApiError ? e.message : 'Tutoring didn’t answer', 'error'));
			t = setInterval(() => load().catch(() => {}), 10_000);
		});
		return () => clearInterval(t);
	});

	type P = MonitorView['points'][number];
	const zero = (at: string): P => ({ at, up_mbps: 0, down_mbps: 0, cpu: undefined, participants: 0, reconnects: 0, measured: false });
	const filled = $derived(v ? fillGaps(v.points, v.step_seconds, zero) : []);
	const bandwidth = $derived(filled.map((p) => ({ at: p.at, values: { up: p.up_mbps, down: p.down_mbps } })));
	const cpu = $derived(filled.map((p) => ({ at: p.at, values: { cpu: p.cpu } })));
	const people = $derived(filled.map((p) => ({ at: p.at, values: { people: p.participants } })));
	const hasCpu = $derived(filled.some((p) => p.cpu != null));
	const cores = (n: number) => `${n < 1 ? n.toFixed(2) : n.toFixed(1)} cores`;
	const whole = (n: number) => String(Math.round(n));
	const time = (s: string) => new Date(s).toLocaleString(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit' });
</script>

{#if enabled === true && v}
	<div class="card card-border bg-base-100 shadow-sm p-4 sm:p-6 vstack gap-4">
		<div class="row gap-2 flex-wrap">
			<h2 class="m-0 text-lg">Monitoring</h2>
			<span class="spacer"></span>
			<div class="join" role="group" aria-label="Range">
				<button class="btn btn-sm join-item" class:btn-active={range === '24h'} aria-pressed={range === '24h'} onclick={() => (range = '24h')}>Last 24 hours</button>
				<button class="btn btn-sm join-item" class:btn-active={range === '7d'} aria-pressed={range === '7d'} onclick={() => (range = '7d')}>Last 7 days</button>
			</div>
		</div>

		<div class="tiles">
			<div class="tile"><span class="small muted">Upload {v.total.measured ? '(measured)' : '(estimated)'}</span><strong class="tabular">{mbps(v.total.up_mbps)}</strong></div>
			<div class="tile"><span class="small muted">Download {v.total.measured ? '(measured)' : '(estimated)'}</span><strong class="tabular">{mbps(v.total.down_mbps)}</strong></div>
			<div class="tile"><span class="small muted">Media server CPU</span><strong class="tabular">{v.total.cpu != null ? cores(v.total.cpu) : '–'}</strong></div>
			<div class="tile"><span class="small muted">Participants</span><strong class="tabular">{v.total.participants}</strong></div>
			<div class="tile"><span class="small muted">Tracks</span><strong class="tabular">{v.total.tracks}</strong></div>
			<div class="tile"><span class="small muted">Reconnects</span><strong class="tabular">{v.total.reconnects}</strong></div>
		</div>
		{#if !v.measuring}
			<p class="small muted m-0">Figures are estimates. Set <code>LIVEKIT_METRICS_URL</code> for the tutoring service (and <code>prometheus: port:</code> in LiveKit’s config) to measure bandwidth and CPU.</p>
		{:else if !v.total.measured}
			<p class="small text-warning m-0">The media server’s metrics aren’t answering: these are estimates.</p>
		{/if}

		{#if v.sessions.length}
			<div class="overflow-x-auto">
				<table class="table table-sm">
					<caption class="text-left small muted">Live now (each session estimated{v.total.measured ? '; CPU shared out by upload' : ''})</caption>
					<thead><tr><th>Session</th><th>Teacher</th><th class="text-right">Upload</th><th class="text-right">Download</th><th class="text-right">CPU</th><th class="text-right">People</th><th class="text-right">Tracks</th><th class="text-right">Reconnects</th></tr></thead>
					<tbody>
						{#each v.sessions as s (s.id)}
							<tr><td>{s.title}</td><td>{s.teacher_name}</td><td class="tabular text-right">{mbps(s.mbps)}</td><td class="tabular text-right">{mbps(s.down_mbps)}</td><td class="tabular text-right">{s.cpu != null ? cores(s.cpu) : '–'}</td><td class="tabular text-right">{s.participants}</td><td class="tabular text-right">{s.tracks}</td><td class="tabular text-right">{s.reconnects}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="small muted m-0">No session is live.</p>
		{/if}

		{#if v.points.length}
			<section class="vstack gap-1" aria-labelledby="bw-h">
				<h3 id="bw-h" class="m-0 text-base">Bandwidth</h3>
				<TimeChart label="Tutoring's upload and download, {range === '7d' ? 'last 7 days' : 'last 24 hours'}" points={bandwidth} fmt={mbps} ref={{ value: v.budget_mbps, label: `Budget (${mbps(v.budget_mbps)})` }}
					series={[{ key: 'up', label: 'Upload', color: 'var(--cat-1)' }, { key: 'down', label: 'Download', color: 'var(--cat-2)' }]} />
			</section>
			{#if hasCpu}
				<section class="vstack gap-1" aria-labelledby="cpu-h">
					<h3 id="cpu-h" class="m-0 text-base">Media server CPU</h3>
					<TimeChart label="The media server's CPU in cores" points={cpu} fmt={cores} series={[{ key: 'cpu', label: 'CPU', color: 'var(--cat-1)' }]} />
				</section>
			{/if}
			<section class="vstack gap-1" aria-labelledby="ppl-h">
				<h3 id="ppl-h" class="m-0 text-base">Participants</h3>
				<TimeChart label="People connected to tutoring" points={people} fmt={whole} series={[{ key: 'people', label: 'Participants', color: 'var(--cat-1)' }]} />
			</section>
		{:else}
			<p class="small muted m-0">Nothing ran in this time. A point is saved each minute while sessions are live, and kept 7 days.</p>
		{/if}

		{#if v.history.length}
			<div class="overflow-x-auto">
				<table class="table table-sm">
					<caption class="text-left small muted">Sessions {range === '7d' ? 'in the last 7 days' : 'in the last 24 hours'} (estimated)</caption>
					<thead><tr><th>Session</th><th>Teacher</th><th>From</th><th>To</th><th class="text-right">Peak upload</th><th class="text-right">Average</th><th class="text-right">Peak download</th><th class="text-right">Most people</th><th class="text-right">Most tracks</th><th class="text-right">Reconnects</th></tr></thead>
					<tbody>
						{#each v.history as h (h.id)}
							<tr><td>{h.title}</td><td>{h.teacher_name}</td><td>{time(h.from)}</td><td>{time(h.to)}</td><td class="tabular text-right">{mbps(h.peak_up_mbps)}</td><td class="tabular text-right">{mbps(h.avg_up_mbps)}</td><td class="tabular text-right">{mbps(h.peak_down_mbps)}</td><td class="tabular text-right">{h.peak_participants}</td><td class="tabular text-right">{h.peak_tracks}</td><td class="tabular text-right">{h.reconnects}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>
{/if}

<style>
	.tiles { display: grid; gap: 0.75rem; grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr)); }
	.tile { display: flex; flex-direction: column; gap: 0.15rem; padding: 0.6rem 0.75rem; border: 1px solid var(--color-base-300); border-radius: 0.75rem; }
	.tile strong { font-size: 1.35rem; }
</style>
