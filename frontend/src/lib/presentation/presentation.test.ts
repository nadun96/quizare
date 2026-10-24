import { describe, expect, it } from 'vitest';
import { canOpen, progressText, slideAlt, slideScale, type MyPresentation, type View } from './presentation';

const view = (allow_jump: boolean, reached: number): Pick<View, 'presentation' | 'reached' | 'slides'> => ({
	presentation: { id: 'p', title: 'T', description: '', allow_jump, allow_download: false, version: 1 },
	reached,
	slides: [1, 2, 3, 4].map((n) => ({ n, width: 1600, height: 900, text: '' }))
});

describe('presentations', () => {
	it('opens any slide, or in order up to the next unseen one', () => {
		expect(canOpen(view(true, 1), 4)).toBe(true);
		expect(canOpen(view(false, 1), 2)).toBe(true);
		expect(canOpen(view(false, 1), 3)).toBe(false);
		expect(canOpen(view(true, 1), 0)).toBe(false);
		expect(canOpen(view(true, 1), 5)).toBe(false);
	});

	it('describes slides for people who can’t see them', () => {
		expect(slideAlt({ n: 2, text: '' }, 9)).toBe('Slide 2 of 9');
		expect(slideAlt({ n: 1, text: '  The cell  ' }, 9)).toBe('Slide 1 of 9: The cell');
		const long = slideAlt({ n: 1, text: 'word '.repeat(100) }, 1);
		expect(long.length).toBeLessThan(270);
		expect(long.endsWith('…')).toBe(true);
	});

	it('says how far a student is', () => {
		const p: MyPresentation = { id: 'p', title: 'T', classroom: 'C', slide_count: 12, started: false, reached: 0, finished: false };
		expect(progressText(p)).toBe('12 slides');
		expect(progressText({ ...p, slide_count: 1 })).toBe('1 slide');
		expect(progressText({ ...p, started: true, reached: 3 })).toBe('Slide 3 of 12');
		expect(progressText({ ...p, started: true, finished: true, reached: 12 })).toBe('Seen to the end');
	});

	it('renders slides 1600 pixels on the longer side', () => {
		expect(slideScale(800, 450)).toBe(2);
		expect(slideScale(595, 842) * 842).toBeCloseTo(1600);
		expect(slideScale(100, 100)).toBe(4); // tiny pages aren't blown up without end
	});
});
