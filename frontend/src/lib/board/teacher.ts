// The teacher's whiteboard calls (D-47).
import { api } from '../api';
import type { BoardAccess, BoardView, NewStroke, Stroke } from './strokes.svelte';

export function teacherBoard(pollId: string) {
	const base = '/api/teacher/polls/' + pollId + '/board';
	return {
		view: () => api.get<BoardView>(base),
		add: async (strokes: NewStroke[]) => (await api.post<{ strokes: Stroke[] }>(base + '/strokes', { strokes })).strokes,
		erase: async (ids: number[], gesture = '') => (await api.post<{ removed: number[] }>(base + '/erase', { ids, gesture })).removed ?? [],
		clear: async () => {
			await api.post(base + '/clear');
		},
		access: (a: BoardAccess) => api.put<BoardAccess>(base + '/access', a)
	};
}
