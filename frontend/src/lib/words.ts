// Counts in words: "1 mark", "2 marks". One place for the plural rule, so no
// screen says "1 marks".

/** `count(1, 'mark')` → "1 mark"; `count(3, 'question')` → "3 questions"; irregular plurals as the third argument. */
export function count(n: number, one: string, many = one + 's'): string {
	return `${n} ${n === 1 ? one : many}`;
}
