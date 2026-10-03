// Reconnecting WebSocket (ADR-04 consequences: reconnect with exponential
// backoff and resume from server state; the server always sends full state).
import { ServerClock } from './clock';

export type Message = { type: string; server_time?: number; [k: string]: unknown };

export class LiveSocket {
	private ws: WebSocket | null = null;
	private retry = 0;
	private closed = false;
	private pingTimer: ReturnType<typeof setInterval> | null = null;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	connected = false;
	onMessage: (m: Message) => void = () => {};
	onStatus: (connected: boolean) => void = () => {};

	constructor(
		private path: string,
		public clock = new ServerClock(),
		private factory: (url: string) => WebSocket = (u) => new WebSocket(u)
	) {}

	static url(path: string, loc: Location = location) {
		return (loc.protocol === 'https:' ? 'wss://' : 'ws://') + loc.host + path;
	}

	open() {
		this.closed = false;
		const ws = this.factory(LiveSocket.url(this.path));
		this.ws = ws;
		ws.onopen = () => {
			this.retry = 0;
			this.setConnected(true);
			this.ping();
			this.pingTimer = setInterval(() => this.ping(), 10_000);
		};
		ws.onmessage = (ev) => {
			let m: Message;
			try {
				m = JSON.parse(ev.data);
			} catch {
				return;
			}
			if (m.type === 'pong' && typeof m.t === 'number' && typeof m.server_time === 'number') {
				this.clock.sample(m.t, m.server_time, Date.now());
				return;
			}
			if (typeof m.server_time === 'number') this.clock.hint(m.server_time);
			this.onMessage(m);
		};
		ws.onclose = () => {
			this.stopPing();
			this.setConnected(false);
			if (!this.closed) this.scheduleReconnect();
		};
		ws.onerror = () => ws.close();
	}

	private setConnected(v: boolean) {
		this.connected = v;
		this.onStatus(v);
	}

	private ping() {
		this.send({ type: 'ping', t: Date.now() });
	}

	private stopPing() {
		if (this.pingTimer) clearInterval(this.pingTimer);
		this.pingTimer = null;
	}

	/** Backoff: 0.5 s, 1 s, 2 s … capped at 10 s, with jitter. */
	static backoff(attempt: number, rand = Math.random) {
		return Math.min(10_000, 500 * 2 ** attempt) * (0.75 + rand() * 0.5);
	}

	private scheduleReconnect() {
		const delay = LiveSocket.backoff(this.retry++);
		this.reconnectTimer = setTimeout(() => this.open(), delay);
	}

	send(m: Record<string, unknown>): boolean {
		if (this.ws?.readyState === 1) {
			this.ws.send(JSON.stringify(m));
			return true;
		}
		return false;
	}

	close() {
		this.closed = true;
		this.stopPing();
		if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
		this.ws?.close();
	}
}
