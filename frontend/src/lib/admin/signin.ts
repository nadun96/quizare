// Sign-in limits (D-126).
export type SignInLimits = { network_burst: number; network_per_sec: number; password_workers: number | null; workers: number; default_workers: number; cpus: number };

/**
 * The longest any of n students on one network waits to sign in when they
 * arrive evenly over `over` seconds (0: all at once). The address's bucket
 * holds at most `burst` sign-ins and refills at `perSec`, as the server's does.
 */
export function classSignInSeconds(n: number, burst: number, perSec: number, over: number): number {
	if (!(burst > 0) || !(perSec > 0) || n < 1) return n < 1 ? 0 : Infinity;
	const dt = 0.05;
	let tokens = burst;
	let arrived = 0;
	let served = 0;
	let longest = 0;
	const arrival = (k: number) => (n > 1 ? (over * k) / (n - 1) : 0); // when student k arrives
	for (let t = 0; served < n && t < 3600; t += dt) {
		while (arrived < n && arrival(arrived) <= t + 1e-9) arrived++;
		while (served < arrived && tokens >= 1) {
			tokens--;
			longest = Math.max(longest, t - arrival(served));
			served++;
		}
		tokens = Math.min(burst, tokens + perSec * dt);
	}
	return served < n ? Infinity : Math.round(longest * 10) / 10;
}
