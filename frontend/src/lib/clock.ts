// Server-time display (ADR-07): the server owns every deadline; the browser
// only renders `deadline - (Date.now() + offset)`. The offset is measured
// from WebSocket ping/pong round trips.

export class ServerClock {
	private offset = 0; // serverTime - clientTime, in ms
	private bestRtt = Infinity;

	constructor(private now: () => number = () => Date.now()) {}

	/** Record a round trip: t0 = client send time, serverTime = server stamp, t1 = client receive time. */
	sample(t0: number, serverTime: number, t1: number) {
		const rtt = t1 - t0;
		if (rtt < 0) return;
		// Prefer the fastest round trip: it bounds the error most tightly.
		if (rtt <= this.bestRtt * 1.5 || this.bestRtt === Infinity) {
			this.bestRtt = Math.min(this.bestRtt, rtt);
			this.offset = serverTime - (t0 + rtt / 2);
		}
	}

	/** Coarse correction from a one-way server timestamp (used before any pong arrives). */
	hint(serverTime: number) {
		if (this.bestRtt === Infinity) this.offset = serverTime - this.now();
	}

	serverNow(): number {
		return this.now() + this.offset;
	}

	/** Milliseconds left until a server-time deadline (never negative). */
	remaining(deadline: number | null | undefined): number | null {
		if (deadline == null) return null;
		return Math.max(0, deadline - this.serverNow());
	}
}

export function formatDuration(ms: number | null): string {
	if (ms == null) return '';
	const total = Math.ceil(ms / 1000);
	const h = Math.floor(total / 3600);
	const m = Math.floor((total % 3600) / 60);
	const s = total % 60;
	const pad = (n: number) => String(n).padStart(2, '0');
	return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}
