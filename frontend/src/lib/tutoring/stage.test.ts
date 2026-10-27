import { describe, expect, it } from 'vitest';
import { ordered, pageOf, pages, wanted, type StageEntry } from './stage';

const people = (n: number): StageEntry[] =>
	Array.from({ length: n }, (_, i) => ({ key: 'stage:' + i, participant: 'p' + i, name: 'Student ' + String(i).padStart(3, '0'), label: 'Camera', source: 'camera' as const }));

describe("the teacher's grid of what students share", () => {
	it('receives only the visible page and the enlarged tile, even with 200 sharing', () => {
		const list = people(200);
		expect(pages(list.length)).toBe(8);
		expect(wanted(list, 0, null).size).toBe(25);
		const w = wanted(list, 3, 'stage:199');
		expect(w.size).toBe(26);
		expect(w.has('stage:75') && w.has('stage:99') && !w.has('stage:100') && w.has('stage:199')).toBe(true);
	});

	it('keeps the page within what there is, and forgets an enlarged tile that left', () => {
		const list = people(30);
		expect(pageOf(list, 5).length).toBe(5); // the last page
		expect(pageOf([], 2)).toEqual([]);
		expect(pages(0)).toBe(1);
		expect(wanted(list, 0, 'stage:gone').has('stage:gone')).toBe(false);
	});

	it('orders by name, screens first', () => {
		const list: StageEntry[] = [
			{ key: 'b', participant: 'x', name: 'Bob', label: 'Camera', source: 'camera' },
			{ key: 'a2', participant: 'y', name: 'Ann', label: 'Camera', source: 'camera' },
			{ key: 'a1', participant: 'y', name: 'Ann', label: 'Screen', source: 'screen_share' }
		];
		expect(ordered(list).map((e) => e.key)).toEqual(['a1', 'a2', 'b']);
	});
});
