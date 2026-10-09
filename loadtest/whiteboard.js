// k6 load test: the real-time whiteboard (V2-09, D-47).
//
// One poll board, many people watching, some drawing at once:
//   - viewers join anonymously, load the board and keep a live socket open;
//   - drawers join too and draw pen lines the way the browser does: a piece
//     of ~12 points every 200 ms, in 3 s gestures with a pause between;
//   - the teacher clears the board every minute (as in class, and so long
//     runs stay under the 20,000-stroke board limit).
//
// Each piece's gesture id carries the time it was sent ("lt<ms>-…"), so
// viewers can measure draw → on-screen latency with k6's own clock. (The
// browser gives all pieces of a line one gesture id for undo; the server
// treats every piece the same whatever its gesture, so the load is equal.)
//
//   k6 run -e BASE=https://quiz.example.edu \
//          -e TEACHER_EMAIL=... -e TEACHER_PASSWORD=... \
//          -e VIEWERS=200 -e DRAWERS=30 -e DURATION=3m loadtest/whiteboard.js
//
// Polls allow 150 joins at once per network address, then four a second
// (D-51); more viewers than that from one address join gradually, which is
// measured as join_waits. To spread joins over many addresses instead, run k6 on the
// server itself (or behind its local proxy) with -e SPREAD_IPS=1: the server
// trusts X-Forwarded-For only from loopback (ADR-12).
//
// Thresholds: draw → viewer p95 < 1 s (V2-09), stroke save p95 < 150 ms,
// board load p95 < 1 s (on joining, and for latecomers joining the busy
// board), no unexpected socket closes, < 1 % failed checks.
import http from 'k6/http';
import ws from 'k6/experimental/websockets'; // k6 ≤ 1.3; newer releases also offer k6/websockets
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE = __ENV.BASE || 'http://localhost:8080';
const WSBASE = BASE.replace(/^http/, 'ws');
const VIEWERS = Number(__ENV.VIEWERS || 200);
const DRAWERS = Number(__ENV.DRAWERS || 30);
const DURATION = __ENV.DURATION || '3m';
const RAMP = __ENV.RAMP || '60s';
const SPREAD = !!__ENV.SPREAD_IPS;

const e2e = new Trend('stroke_draw_to_viewer_ms', true);
const savePiece = new Trend('stroke_save_ms', true);
const boardLoad = new Trend('board_load_ms', true);
const lateLoad = new Trend('late_board_load_ms', true); // joining a board that's already busy
const lateSize = new Trend('late_board_strokes');
const joinWaits = new Counter('join_waits');
const received = new Counter('board_events_received');
const sent = new Counter('stroke_pieces_sent');
const rejected = new Counter('stroke_pieces_rejected');
const closes = new Counter('ws_unexpected_closes');
const dropAfter = new Trend('ws_drop_after_s'); // how long a dropped socket had been open
const dropBehind = new Trend('ws_drop_events_seen'); // events it had received when dropped
const ok = new Rate('checks_ok');

export const options = {
	setupTimeout: '60s',
	scenarios: {
		viewers: { executor: 'ramping-vus', exec: 'viewer', startVUs: 0, stages: [{ duration: RAMP, target: VIEWERS }, { duration: DURATION, target: VIEWERS }], gracefulRampDown: '5s' },
		drawers: { executor: 'constant-vus', exec: 'drawer', vus: DRAWERS, duration: DURATION, startTime: RAMP },
		teacher: { executor: 'constant-vus', exec: 'teacher', vus: 1, duration: DURATION, startTime: RAMP },
		// Latecomers: one every 2 s joins the busy board and loads all of it.
		latecomers: { executor: 'constant-arrival-rate', exec: 'latecomer', rate: 1, timeUnit: '2s', duration: DURATION, startTime: RAMP, preAllocatedVUs: 5 }
	},
	thresholds: {
		stroke_draw_to_viewer_ms: ['p(95)<1000', 'p(99)<2000'],
		stroke_save_ms: ['p(95)<150', 'p(99)<400'],
		board_load_ms: ['p(95)<1000'],
		late_board_load_ms: ['p(95)<1000'],
		ws_unexpected_closes: ['count==0'],
		checks_ok: ['rate>0.99']
	}
};

// One network address per VU with SPREAD_IPS (10.x.y.z), else the k6 host's.
function headers(extra = {}) {
	const h = { 'Content-Type': 'application/json', 'X-Requested-With': 'fetch', Origin: BASE, ...extra };
	if (SPREAD) h['X-Forwarded-For'] = `10.${(__VU >> 16) & 255}.${(__VU >> 8) & 255}.${__VU & 255}`;
	return h;
}

// "1m30s", "90s", "3m" → milliseconds.
function ms(d) {
	let t = 0;
	for (const [, n, u] of d.matchAll(/(\d+)(ms|s|m|h)/g)) t += Number(n) * { ms: 1, s: 1000, m: 60000, h: 3600000 }[u];
	return t;
}

export function setup() {
	const res = http.post(`${BASE}/api/auth/login`, JSON.stringify({ email: __ENV.TEACHER_EMAIL, password: __ENV.TEACHER_PASSWORD }), { headers: headers() });
	if (res.status !== 200) throw new Error(`teacher login failed: ${res.status}`);
	const cookie = `__Host-sid=${res.cookies['__Host-sid'][0].value}`;
	const h = headers({ Cookie: cookie });
	const settings = { identity: 'anonymous', audience: 'anyone', pacing: 'self', show_results: 'live', allow_edit: true };
	const poll = http.post(`${BASE}/api/teacher/polls`, JSON.stringify({ title: `Whiteboard load test ${new Date().toISOString()}`, settings }), { headers: h }).json();
	http.post(`${BASE}/api/teacher/polls/${poll.id}/questions`, JSON.stringify({ type: 'WORD_CLOUD', text: 'Load test', body: { max_entries: 1 } }), { headers: h });
	http.post(`${BASE}/api/teacher/polls/${poll.id}/status`, JSON.stringify({ status: 'open' }), { headers: h });
	const access = http.put(`${BASE}/api/teacher/polls/${poll.id}/board/access`, JSON.stringify({ open: true, mode: 'everyone' }), { headers: h });
	if (access.status !== 200) throw new Error(`board access failed: ${access.status} ${access.body}`);
	// Viewers close their own sockets just before the run ends, so only real drops count.
	return { id: poll.id, code: poll.join_code, cookie, endAt: Date.now() + ms(RAMP) + ms(DURATION) - 1500 };
}

// join returns the participant token, waiting out the per-address join limit.
function join(code) {
	for (let i = 0; i < 120; i++) {
		const r = http.post(`${BASE}/api/polls/${code}/join`, JSON.stringify({ nickname: `LT ${__VU}` }), { headers: headers() });
		if (r.status === 200) return r.json('token');
		if (r.status !== 429) {
			ok.add(false);
			return '';
		}
		joinWaits.add(1);
		sleep(2 + Math.random());
	}
	ok.add(false);
	return '';
}

export function viewer(data) {
	// One session per viewer: k6 restarts an iteration as soon as it ends, so
	// a viewer whose socket just closed waits out the run instead of joining
	// and loading the whole board again at the last moment.
	if (Date.now() > data.endAt - 3000) {
		sleep(Math.max(0, (data.endAt - Date.now()) / 1000 + 2));
		return;
	}
	const token = join(data.code);
	if (!token) return;
	const t0 = Date.now();
	const board = http.get(`${BASE}/api/polls/${data.code}/board`, { headers: headers({ 'X-Poll-Token': token }) });
	boardLoad.add(Date.now() - t0);
	ok.add(check(board, { 'board loads': (r) => r.status === 200 && r.json('open') === true }));

	let closing = false;
	let opened = 0;
	let seen = 0;
	const socket = new ws.WebSocket(`${WSBASE}/ws/polls/${data.code}`, null, { headers: { Origin: BASE } });
	socket.onmessage = (e) => {
		const m = JSON.parse(e.data);
		if (m.type !== 'board') return;
		received.add(1);
		seen++;
		if (m.op !== 'add') return;
		const now = Date.now();
		for (const s of m.strokes || []) {
			const t = /^lt(\d+)-/.exec(s.gesture);
			if (t) e2e.add(now - Number(t[1]));
		}
	};
	// Like the browser (lib/socket.ts): a ping every 10 s once open; the
	// server drops sockets that stay silent for 45 s.
	let ping = null;
	socket.onclose = (e) => {
		if (ping) clearInterval(ping);
		if (closing) return;
		closes.add(1, { code: String(e && e.code) });
		dropAfter.add((Date.now() - opened) / 1000);
		dropBehind.add(seen);
	};
	// Stay connected until just before the run ends (one session per viewer).
	socket.onopen = () => {
		opened = Date.now();
		ping = setInterval(() => socket.send(JSON.stringify({ type: 'ping', t: Date.now() })), 10000);
		setTimeout(() => {
			closing = true;
			socket.close();
		}, Math.max(1000, data.endAt - Date.now()));
	};
}

export function latecomer(data) {
	const token = join(data.code);
	if (!token) return;
	const t0 = Date.now();
	const r = http.get(`${BASE}/api/polls/${data.code}/board`, { headers: headers({ 'X-Poll-Token': token }) });
	lateLoad.add(Date.now() - t0);
	ok.add(check(r, { 'late board loads': (x) => x.status === 200 }));
	if (r.status === 200) lateSize.add((r.json('strokes') || []).length);
}

export function drawer(data) {
	const token = join(data.code);
	if (!token) return;
	const h = headers({ 'X-Poll-Token': token });
	for (;;) {
		// One 3 s line: 15 pieces, each continuing from the last point.
		let x = 100 + Math.random() * 1200;
		let y = 100 + Math.random() * 600;
		for (let piece = 0; piece < 15; piece++) {
			const points = [x, y];
			for (let i = 0; i < 12; i++) {
				x = Math.min(1590, Math.max(10, x + (Math.random() - 0.4) * 12));
				y = Math.min(890, Math.max(10, y + (Math.random() - 0.5) * 12));
				points.push(Math.round(x * 10) / 10, Math.round(y * 10) / 10);
			}
			const stroke = { gesture: `lt${Date.now()}-${__VU}-${piece}`, tool: 'pen', color: '#1d4ed8', size: 4, points };
			const t0 = Date.now();
			const r = http.post(`${BASE}/api/polls/${data.code}/board/strokes`, JSON.stringify({ strokes: [stroke] }), { headers: h });
			savePiece.add(Date.now() - t0);
			sent.add(1);
			if (r.status === 201) ok.add(true);
			else if (r.status === 409) rejected.add(1); // board full until the teacher clears it
			else ok.add(check(r, { 'stroke saved': (x) => x.status === 201 }));
			sleep(Math.max(0, 0.2 - (Date.now() - t0) / 1000));
		}
		sleep(1 + Math.random());
	}
}

export function teacher(data) {
	sleep(60);
	const r = http.post(`${BASE}/api/teacher/polls/${data.id}/board/clear`, null, { headers: headers({ Cookie: data.cookie }) });
	ok.add(check(r, { 'board cleared': (x) => x.status === 204 }));
}

export function teardown(data) {
	// Leave the poll for inspection, but close it so nobody can join later.
	http.post(`${BASE}/api/teacher/polls/${data.id}/status`, JSON.stringify({ status: 'closed' }), { headers: headers({ Cookie: data.cookie }) });
}
