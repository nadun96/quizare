// The devices a person chose in the device check (TS-FR-05), remembered in
// this browser only, and used when they turn on their microphone or camera
// and for the session's sound.

export type DeviceChoice = { speaker?: string; mic?: string; camera?: string };

const KEY = 'tutoring.devices';

export function loadChoice(store: Pick<Storage, 'getItem'> | null = safeStorage()): DeviceChoice {
	try {
		const v = JSON.parse(store?.getItem(KEY) ?? '{}');
		return typeof v === 'object' && v ? { speaker: str(v.speaker), mic: str(v.mic), camera: str(v.camera) } : {};
	} catch {
		return {};
	}
}

export function saveChoice(c: DeviceChoice, store: Pick<Storage, 'setItem'> | null = safeStorage()) {
	try {
		store?.setItem(KEY, JSON.stringify(c));
	} catch {
		// Private windows and blocked storage: the choice lasts this page only.
	}
}

function safeStorage(): Storage | null {
	try {
		return typeof localStorage === 'undefined' ? null : localStorage;
	} catch {
		return null;
	}
}

const str = (v: unknown) => (typeof v === 'string' && v ? v : undefined);

/** The saved device if it's still there, else the first one (the system's default). */
export function pick(devices: { deviceId: string }[], saved?: string): string {
	if (saved && devices.some((d) => d.deviceId === saved)) return saved;
	return devices[0]?.deviceId ?? '';
}

/** A device's name, or a numbered stand-in until the browser shows names (after a permission). */
export function deviceName(d: { label: string; kind: string }, i: number): string {
	if (d.label) return d.label;
	const what = d.kind === 'audiooutput' ? 'Speaker' : d.kind === 'audioinput' ? 'Microphone' : 'Camera';
	return `${what} ${i + 1}`;
}

/** 0 to 1 from an analyser's time-domain bytes: how loud the microphone is now. */
export function level(bytes: Uint8Array): number {
	let peak = 0;
	for (const b of bytes) peak = Math.max(peak, Math.abs(b - 128));
	return Math.min(1, peak / 100);
}
