// Load test: the tutoring media server, LiveKit (ADR-19, D-58), on this machine.
//
// Starts a LiveKit server bound to loopback, simulates a broadcaster and
// viewers with LiveKit's own load tester (`lk load-test`), samples the
// server's CPU and memory, and checks the capacity requirements:
//
//   200      1 video (simulcast) + audio to 200 viewers   TS-NFR-01
//   75x4     4 videos + audio to 75 viewers                TS-NFR-02
//
//   node loadtest/tutoring-media.mjs                 # both scenarios, 60 s each
//   SCENARIO=200 DURATION=120s node loadtest/tutoring-media.mjs
//   LIVEKIT_PORT=7980 node loadtest/tutoring-media.mjs   # if 7880 is taken
//
// The pinned LiveKit builds are downloaded once into loadtest/.bin and
// checked against their published SHA-256 checksums (or set LIVEKIT_BIN to
// a folder holding livekit-server and lk). Everything runs on 127.0.0.1;
// nothing is reachable from the network.
//
// Thresholds (environment variables to change them): every viewer receives
// every track; packet loss < MAX_LOSS % (2); no tester errors; the server's
// CPU p95 < CPU_MAX cores (2.5: the load generator shares this machine's
// cores, so the 2-core target of TS-NFR-07 gets some slack); total sent
// bitrate within BUDGET_MBPS (240, the default tutoring budget, TS-FR-31).
//
// Requires Node 18+ and `tar` (built into Windows 10+, Linux and macOS).
// On macOS, install livekit-server with Homebrew and set LIVEKIT_BIN.
import { spawn, execFile } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const run = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));
const win = process.platform === 'win32';
const exe = (n) => (win ? n + '.exe' : n);

const SERVER = '1.13.7';
const CLI = '2.18.7';
// From the releases' checksums.txt.
const SUMS = {
	[`livekit_${SERVER}_windows_amd64.zip`]: 'e539e7d2f75807b9c9202cd2a0bf2cb3d52fc4c52978a6953e0f47bc339fe77f',
	[`livekit_${SERVER}_linux_amd64.tar.gz`]: '6634aeeb2fb1366b6723708ae4320b9d5408106a4c63457c5e845ae3979c90e2',
	[`livekit_${SERVER}_linux_arm64.tar.gz`]: '5d167fdf52cf43c0c72972f25325364479f41f854bfef651056eab2504da5de9',
	[`lk_${CLI}_windows_amd64.zip`]: '44f56794b0d3d0ae8cb56e56b2a768fc0bc14eec41bbdc77ba57fee31070e33d',
	[`lk_${CLI}_linux_amd64.tar.gz`]: '7f55697d855f13a091491fcbd7b8de9fc40971524b9eedfa50f58efd6b88e13e',
	[`lk_${CLI}_linux_arm64.tar.gz`]: '258a6502f640378fb5ce8db91ee2b8cd149f689dda8cc36b9d44a56db1e8a587',
	[`lk_${CLI}_darwin_amd64.tar.gz`]: '59a8a7159695606e593624114e138d0b8e254cb674265bea300c2e67d38c5070',
	[`lk_${CLI}_darwin_arm64.tar.gz`]: '9c9a76746663b47d86676e63afa1d00730b024070abe2fe4aaf4cd38c950ecaf'
};

const env = process.env;
const DURATION = env.DURATION || '60s';
const MAX_LOSS = Number(env.MAX_LOSS || 2);
const CPU_MAX = Number(env.CPU_MAX || 2.5);
const BUDGET_MBPS = Number(env.BUDGET_MBPS || 240);
// LiveKit's ports: PORT for HTTP, PORT+1 TCP and PORT+2 UDP for media. Windows
// sometimes reserves 7880's range (netsh interface ipv4 show excludedportrange).
const PORT = Number(env.LIVEKIT_PORT || 7880);
const SCENARIOS = {
	200: { requirement: 'TS-NFR-01', video: 1, audio: 1, viewers: 200 },
	'75x4': { requirement: 'TS-NFR-02', video: 4, audio: 1, viewers: 75 }
};
const chosen = (env.SCENARIO || 'all') === 'all' ? Object.keys(SCENARIOS) : [env.SCENARIO];
for (const s of chosen) if (!SCENARIOS[s]) throw new Error(`unknown SCENARIO ${s}; use ${Object.keys(SCENARIOS).join(', ')} or all`);

// ---------- binaries ----------

async function fetchVerified(name, url, dir) {
	const file = join(dir, name);
	if (!existsSync(file)) {
		console.log(`downloading ${name}`);
		const res = await fetch(url);
		if (!res.ok) throw new Error(`${url}: ${res.status}`);
		writeFileSync(file, Buffer.from(await res.arrayBuffer()));
	}
	const sum = createHash('sha256').update(readFileSync(file)).digest('hex');
	if (sum !== SUMS[name]) throw new Error(`${name}: checksum mismatch (${sum}); delete it and retry`);
	// A relative name from inside the folder: GNU tar reads "D:\…" as a remote host.
	// On Windows, its own tar (bsdtar) reads zip files; Git Bash's GNU tar doesn't.
	const tar = win ? join(env.SystemRoot || String.raw`C:\Windows`, 'System32', 'tar.exe') : 'tar';
	await run(tar, ['-xf', name], { cwd: dir });
}

async function binaries() {
	if (env.LIVEKIT_BIN) return env.LIVEKIT_BIN;
	const dir = join(here, '.bin');
	if (existsSync(join(dir, exe('livekit-server'))) && existsSync(join(dir, exe('lk')))) return dir;
	mkdirSync(dir, { recursive: true });
	const os = { win32: 'windows', linux: 'linux', darwin: 'darwin' }[process.platform];
	const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
	const ext = win ? 'zip' : 'tar.gz';
	const server = `livekit_${SERVER}_${os}_${arch}.${ext}`;
	const cli = `lk_${CLI}_${os}_${arch}.${ext}`;
	if (!SUMS[server] || !SUMS[cli]) throw new Error(`no pinned LiveKit build for ${os}/${arch}; install livekit-server and lk and set LIVEKIT_BIN`);
	await fetchVerified(server, `https://github.com/livekit/livekit/releases/download/v${SERVER}/${server}`, dir);
	await fetchVerified(cli, `https://github.com/livekit/livekit-cli/releases/download/v${CLI}/${cli}`, dir);
	return dir;
}

// ---------- CPU sampling ----------

// Cumulative CPU seconds and resident memory (MB) of a process (Linux, macOS).
async function usage(pid) {
	if (process.platform === 'linux') {
		// Clock ticks (100 a second) are finer than ps's whole seconds.
		const f = readFileSync(`/proc/${pid}/stat`, 'utf8').split(') ')[1].split(' ');
		return { cpu: (Number(f[11]) + Number(f[12])) / 100, rss: (Number(f[21]) * 4096) / 2 ** 20 };
	}
	const { stdout } = await run('ps', ['-o', 'time=,rss=', '-p', String(pid)]);
	const [time, rss] = stdout.trim().split(/\s+/);
	const parts = time.split(/[-:]/).map(Number); // [[dd-]hh:]mm:ss
	let cpu = 0;
	for (const p of parts) cpu = cpu * 60 + p;
	if (parts.length === 4) cpu = parts[0] * 86400 + parts[1] * 3600 + parts[2] * 60 + parts[3];
	return { cpu, rss: Number(rss) / 1024 };
}

// On Windows one PowerShell stays open and prints the figures every 2 s:
// starting a new one per sample took enough CPU to disturb the test.
function windowsSampler(pid) {
	const samples = [];
	let prev = null;
	const ps = spawn('powershell', ['-NoProfile', '-Command',
		`while ($true) { $p = Get-Process -Id ${pid} -ErrorAction SilentlyContinue; if (-not $p) { break }; "{0} {1} {2}" -f [DateTimeOffset]::Now.ToUnixTimeMilliseconds(), $p.TotalProcessorTime.TotalSeconds, $p.WorkingSet64; Start-Sleep -Seconds 2 }`],
		{ stdio: ['ignore', 'pipe', 'ignore'] });
	let buf = '';
	ps.stdout.on('data', (d) => {
		buf += d;
		let i;
		while ((i = buf.indexOf('\n')) >= 0) {
			const [t, cpu, rss] = buf.slice(0, i).trim().split(/\s+/).map(Number);
			buf = buf.slice(i + 1);
			if (!Number.isFinite(cpu)) continue;
			if (prev) samples.push({ cores: (cpu - prev.cpu) / ((t - prev.t) / 1000), rss: rss / 2 ** 20 });
			prev = { t, cpu };
		}
	});
	return async () => {
		ps.kill();
		return samples;
	};
}

function sampler(pid) {
	if (win) return windowsSampler(pid);
	const samples = [];
	let prev = null;
	let stop = false;
	const loop = (async () => {
		while (!stop) {
			const t = Date.now();
			try {
				const u = await usage(pid);
				if (prev) samples.push({ cores: (u.cpu - prev.cpu) / ((t - prev.t) / 1000), rss: u.rss });
				prev = { ...u, t };
			} catch {
				/* the process is gone */
			}
			await new Promise((r) => setTimeout(r, 2000));
		}
	})();
	return async () => {
		stop = true;
		await loop;
		return samples;
	};
}

const pct = (xs, p) => {
	const s = [...xs].sort((a, b) => a - b);
	return s.length ? s[Math.min(s.length - 1, Math.floor(s.length * p))] : 0;
};

// ---------- the test ----------

function parseTotals(out) {
	// The last "Total" row is the subscribers' summary: tracks, bitrate, loss, errors.
	const re = /Total\s*│\s*(\d+)\/(\d+)\s*│\s*([\d.]+)\s*([kmg]?)bps[^│]*│\s*(\d+)\s*\(([\d.]+)%\)\s*│\s*(\d+)/g;
	let m;
	let last = null;
	while ((m = re.exec(out))) last = m;
	if (!last) return null;
	const unit = { '': 1e-6, k: 1e-3, m: 1, g: 1e3 }[last[4]];
	return { tracks: +last[1], expected: +last[2], mbps: +last[3] * unit, loss: +last[6], errors: +last[7] };
}

async function scenario(dir, key, name) {
	const s = SCENARIOS[name];
	const args = ['--url', `ws://127.0.0.1:${PORT}`, '--api-key', 'loadtest', '--api-secret', key, 'load-test', '--room', `lt-${name}-${Date.now()}`,
		'--video-publishers', String(s.video), '--audio-publishers', String(s.audio), '--subscribers', String(s.viewers),
		'--duration', DURATION, '--num-per-second', '5'];
	console.log(`\n${name}: ${s.video} video + ${s.audio} audio to ${s.viewers} viewers for ${DURATION} (${s.requirement})`);
	const lk = spawn(join(dir, exe('lk')), args, { stdio: ['ignore', 'pipe', 'pipe'] });
	let out = '';
	lk.stdout.on('data', (d) => (out += d));
	lk.stderr.on('data', (d) => (out += d));
	const code = await new Promise((r) => lk.on('close', r));
	const failedJoins = (out.match(/could not connect/g) || []).length;
	return { name, ...s, code, failedJoins, totals: parseTotals(out) };
}

async function main() {
	const dir = await binaries();
	const key = randomBytes(24).toString('hex'); // this run only
	const cfg = join(here, '.bin', 'livekit-loadtest.yaml');
	mkdirSync(dirname(cfg), { recursive: true });
	// Loopback only. node_ip must match, or viewers get a candidate address
	// the server doesn't listen on and fail to connect (seen in the spike).
	writeFileSync(cfg, `port: ${PORT}\nbind_addresses: ["127.0.0.1"]\nrtc:\n  udp_port: ${PORT + 2}\n  tcp_port: ${PORT + 1}\n  node_ip: 127.0.0.1\n  use_external_ip: false\nkeys:\n  loadtest: ${key}\nlogging:\n  level: warn\n`);
	const server = spawn(join(dir, exe('livekit-server')), ['--config', cfg], { stdio: 'ignore' });
	const results = [];
	try {
		for (let i = 0; i < 40; i++) {
			if (await fetch(`http://127.0.0.1:${PORT}`).then(() => true, () => false)) break;
			await new Promise((r) => setTimeout(r, 500));
		}
		for (const name of chosen) {
			const stop = sampler(server.pid);
			const r = await scenario(dir, key, name);
			const samples = await stop();
			// Ramp-up (5 viewers a second) and wind-down excluded: the busiest half of the run.
			const cores = samples.map((x) => x.cores);
			const busy = [...cores].sort((a, b) => b - a).slice(0, Math.max(1, Math.ceil(cores.length / 2)));
			r.cpu = { median: pct(busy, 0.5), p95: pct(busy, 0.95), peak: Math.max(0, ...cores) };
			r.rssMB = Math.round(Math.max(0, ...samples.map((x) => x.rss)));
			results.push(r);
		}
	} finally {
		server.kill();
	}

	let ok = true;
	console.log('\nResult');
	for (const r of results) {
		const t = r.totals;
		const checks = [
			['every viewer got every track', t && t.tracks === t.expected && r.failedJoins === 0, t ? `${t.tracks}/${t.expected}, ${r.failedJoins} failed joins` : 'no summary'],
			[`packet loss < ${MAX_LOSS} %`, t && t.loss < MAX_LOSS, t ? `${t.loss} %` : '-'],
			['no tester errors', t && t.errors === 0 && r.code === 0, t ? `${t.errors} errors, exit ${r.code}` : `exit ${r.code}`],
			[`sent within ${BUDGET_MBPS} Mbit/s`, t && t.mbps <= BUDGET_MBPS, t ? `${t.mbps.toFixed(1)} Mbit/s, ${(t.mbps / r.viewers).toFixed(2)} per viewer` : '-'],
			[`server CPU p95 < ${CPU_MAX} cores`, r.cpu.p95 < CPU_MAX, `median ${r.cpu.median.toFixed(2)}, p95 ${r.cpu.p95.toFixed(2)}, peak ${r.cpu.peak.toFixed(2)}; memory ${r.rssMB} MB`]
		];
		console.log(`\n${r.name} (${r.requirement})`);
		for (const [label, pass, detail] of checks) {
			console.log(`  ${pass ? 'PASS' : 'FAIL'}  ${label}: ${detail}`);
			ok &&= !!pass;
		}
	}
	console.log(ok ? '\nALL PASS' : '\nSOME FAILED');
	process.exit(ok ? 0 : 1);
}

main().catch((e) => {
	console.error(e);
	process.exit(1);
});
