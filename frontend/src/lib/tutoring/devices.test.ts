import { describe, expect, it } from 'vitest';
import { deviceName, level, loadChoice, pick, saveChoice } from './devices';

describe('device check', () => {
	it('remembers the choice, and survives storage that is broken or blocked', () => {
		const m = new Map<string, string>();
		const store = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
		saveChoice({ speaker: 's1', mic: 'm2' }, store);
		expect(loadChoice(store)).toEqual({ speaker: 's1', mic: 'm2', camera: undefined });
		m.set('tutoring.devices', '{not json');
		expect(loadChoice(store)).toEqual({});
		expect(loadChoice(null)).toEqual({});
		const blocked = { setItem: () => { throw new Error('quota'); } };
		expect(() => saveChoice({ mic: 'x' }, blocked)).not.toThrow();
	});

	it('uses the saved device while it is plugged in, else the default', () => {
		const list = [{ deviceId: 'default' }, { deviceId: 'usb' }];
		expect(pick(list, 'usb')).toBe('usb');
		expect(pick(list, 'gone')).toBe('default');
		expect(pick([], 'usb')).toBe('');
	});

	it('names devices before the browser shows their names', () => {
		expect(deviceName({ label: '', kind: 'audiooutput' }, 0)).toBe('Speaker 1');
		expect(deviceName({ label: '', kind: 'videoinput' }, 1)).toBe('Camera 2');
		expect(deviceName({ label: 'USB Mic', kind: 'audioinput' }, 0)).toBe('USB Mic');
	});

	it('measures how loud the microphone is', () => {
		expect(level(new Uint8Array([128, 128, 128]))).toBe(0);
		expect(level(new Uint8Array([128, 178, 100]))).toBe(0.5);
		expect(level(new Uint8Array([0, 255]))).toBe(1);
	});
});
