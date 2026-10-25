import { describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api';
import { insistable, mbps, meter } from './types';

// The bandwidth budget in the browser (D-81).

const confirmDialog = vi.fn(async (_o: unknown) => true);
vi.mock('../ui/dialog.svelte', () => ({ confirmDialog: (o: unknown) => confirmDialog(o) }));
const { sentence, withBudget } = await import('./budget');

describe('figures', () => {
	it('writes Mbit/s with sensible precision', () => {
		expect(mbps(0.12)).toBe('0.12 Mbit/s');
		expect(mbps(5.92)).toBe('5.9 Mbit/s');
		expect(mbps(55.76)).toBe('56 Mbit/s');
	});

	it('warns from 80 % and alarms over the limit', () => {
		expect(meter(10, 40)).toEqual({ pct: 25, level: 'ok' });
		expect(meter(32, 40)).toEqual({ pct: 80, level: 'warn' });
		expect(meter(50, 40)).toEqual({ pct: 100, level: 'over' });
		expect(meter(0, 0)).toEqual({ pct: 0, level: 'ok' });
		expect(meter(1, 0)).toEqual({ pct: 100, level: 'over' });
	});

	it('knows which refusals a teacher may override', () => {
		expect(insistable('it would use about 1.2 Mbit/s more, and 0.4 are left: stop another screen or camera, or send it anyway')).toBe(true);
		expect(insistable('that would take the server above its hard limit (270 Mbit/s): stop another screen or camera')).toBe(false);
		expect(sentence('its plan needs about 255 Mbit/s')).toBe('Its plan needs about 255 Mbit/s.');
	});
});

describe('withBudget', () => {
	const over = (msg: string) => new ApiError(409, 'over_budget', msg, {});

	it('goes ahead when it fits, without asking', async () => {
		const f = vi.fn(async () => {});
		expect(await withBudget(f)).toBe(true);
		expect(f).toHaveBeenCalledWith(false);
		expect(confirmDialog).not.toHaveBeenCalled();
	});

	it('asks, then insists with force', async () => {
		const f = vi.fn(async (force: boolean) => {
			if (!force) throw over('it needs 30 and 20 are free: start anyway');
		});
		expect(await withBudget(f, 'Start anyway')).toBe(true);
		expect(f.mock.calls).toEqual([[false], [true]]);
		expect(confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ confirm: 'Start anyway', body: expect.stringContaining('It needs 30 and 20 are free') }));
	});

	it('stops when the teacher declines', async () => {
		confirmDialog.mockResolvedValueOnce(false);
		const f = vi.fn(async (force: boolean) => {
			if (!force) throw over('…, or turn it on anyway');
		});
		expect(await withBudget(f)).toBe(false);
		expect(f).toHaveBeenCalledTimes(1);
	});

	it('passes other refusals through: the hard limit, and other errors', async () => {
		await expect(withBudget(async () => Promise.reject(over('above its hard limit (270 Mbit/s): keep groups to voice and chat')))).rejects.toThrow('hard limit');
		await expect(withBudget(async () => Promise.reject(new ApiError(403, 'forbidden', 'not allowed', {})))).rejects.toThrow('not allowed');
	});
});
