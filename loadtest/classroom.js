// k6 load test: "class of 100 × 3 safety factor" (architecture §2.3).
//
//   1. Create a Ready quiz and a session as a teacher; note the session id and join code.
//   2. Pre-create student accounts student1..student300@load.test (password "password123"),
//      or run once with SIGNUP=1 to register them.
//   3. k6 run -e BASE=https://quiz.example.edu -e CODE=ABC123 -e SESSION=<uuid> \
//            -e TEACHER_EMAIL=... -e TEACHER_PASSWORD=... loadtest/classroom.js
//
// Thresholds: p95 answer save < 150 ms, p99 < 400 ms; admit→countdown p95 < 500 ms;
// login p95 < 3 s during the burst; no unexpected WebSocket closes.
import http from 'k6/http';
import ws from 'k6/websockets';
import { check, sleep } from 'k6';
import { Trend, Counter } from 'k6/metrics';

const BASE = __ENV.BASE || 'http://localhost:8080';
const CODE = __ENV.CODE;
const SESSION = __ENV.SESSION;
const WSBASE = BASE.replace(/^http/, 'ws');

const saveLatency = new Trend('answer_save_ms', true);
const admitLatency = new Trend('admit_to_countdown_ms', true);
const loginLatency = new Trend('login_ms', true);
const unexpectedCloses = new Counter('ws_unexpected_closes');

export const options = {
	scenarios: {
		students: { executor: 'ramping-vus', exec: 'student', startVUs: 0, stages: [{ duration: '60s', target: 300 }, { duration: '10m', target: 300 }] },
		teacher: { executor: 'per-vu-iterations', exec: 'teacher', vus: 1, iterations: 1, startTime: '70s' }
	},
	thresholds: {
		answer_save_ms: ['p(95)<150', 'p(99)<400'],
		admit_to_countdown_ms: ['p(95)<500'],
		login_ms: ['p(95)<3000'],
		ws_unexpected_closes: ['count==0']
	}
};

const headers = (cookie) => ({ 'Content-Type': 'application/json', 'X-Requested-With': 'fetch', Origin: BASE, Cookie: cookie || '' });

function login(email, password) {
	if (__ENV.SIGNUP) http.post(`${BASE}/api/auth/register`, JSON.stringify({ email, password, name: email, role: 'student' }), { headers: headers() });
	const t0 = Date.now();
	let res;
	for (let i = 0; i < 5; i++) {
		res = http.post(`${BASE}/api/auth/login`, JSON.stringify({ email, password }), { headers: headers() });
		if (res.status !== 503) break; // Argon2 queue full: back off and retry (R1)
		sleep(1 + Math.random());
	}
	loginLatency.add(Date.now() - t0);
	check(res, { 'login ok': (r) => r.status === 200 });
	const c = res.cookies['__Host-sid'];
	return c ? `__Host-sid=${c[0].value}` : '';
}

export function student() {
	const cookie = login(`student${__VU}@load.test`, 'password123');
	const join = http.post(`${BASE}/api/join/sessions/${CODE}`, JSON.stringify({ student_number: `LT-${__VU}` }), { headers: headers(cookie) });
	if (!check(join, { 'joined': (r) => r.status === 200 })) return;
	const attempt = join.json('attempt_id');
	let waitingSince = Date.now();
	let state = null;
	let done = false;

	const socket = new ws.WebSocket(`${WSBASE}/ws/attempts/${attempt}`, null, { headers: { Cookie: cookie, Origin: BASE } });
	socket.onmessage = (e) => {
		const m = JSON.parse(e.data);
		if (m.type !== 'state') return;
		if (state === 'waiting' && m.state === 'admitted') admitLatency.add(Date.now() - waitingSince);
		state = m.state;
		if (m.state === 'admitted') socket.send(JSON.stringify({ type: 'start' }));
		if (m.state === 'in_progress' && m.question && !m.answer) answer(m);
		if (m.state === 'submitted' || m.state === 'invalidated') {
			done = true;
			socket.close();
		}
	};
	socket.onclose = () => {
		if (!done) unexpectedCloses.add(1);
	};
	const ping = setInterval(() => socket.send(JSON.stringify({ type: 'ping', t: Date.now() })), 10000);

	function answer(m) {
		const q = m.question;
		const response = q.type === 'ESSAY' ? { text: 'load test answer text' } : q.body.options ? { selected: [q.body.options[0].id] } : {};
		setTimeout(() => {
			if (Math.random() < 0.02) socket.send(JSON.stringify({ type: 'violation', kind: 'tab_hidden', client_ts: Date.now() })); // 2% violate
			const t0 = Date.now();
			const r = http.put(`${BASE}/api/attempts/${attempt}/answers/${q.id}`, JSON.stringify({ response, seq: Date.now() }), { headers: headers(cookie) });
			saveLatency.add(Date.now() - t0);
			check(r, { 'saved': (x) => x.status === 200 || x.status === 409 });
			http.post(`${BASE}/api/attempts/${attempt}/advance`, JSON.stringify({ question_id: q.id }), { headers: headers(cookie) });
		}, 5000 + Math.random() * 25000); // 5-30 s think time
	}
	socket.onopen = () => (waitingSince = Date.now());
	setTimeout(() => clearInterval(ping), 15 * 60 * 1000);
}

export function teacher() {
	const cookie = login(__ENV.TEACHER_EMAIL, __ENV.TEACHER_PASSWORD);
	const post = (path, body) => http.post(`${BASE}/api/teacher/sessions/${SESSION}${path}`, JSON.stringify(body), { headers: headers(cookie) });
	check(post('/admit', { all: true }), { 'admit all': (r) => r.status === 200 });
	sleep(120);
	post('/pause', { all: true });
	sleep(20);
	post('/resume', { all: true });
	const dash = http.get(`${BASE}/api/teacher/sessions/${SESSION}`, { headers: headers(cookie) });
	const ids = (dash.json('rows') || []).slice(0, 10).map((r) => r.attempt_id);
	post('/extend', { attempt_ids: ids, seconds: 300 });
}
