// Short confirmations for actions ("Question saved", "Student admitted").
// Announced politely to screen readers by Toaster.svelte.
export type ToastKind = 'success' | 'info' | 'warning' | 'error';
export type Toast = { id: number; kind: ToastKind; text: string };

let next = 1;
class Toasts {
	list = $state<Toast[]>([]);
	show(text: string, kind: ToastKind = 'success', ms = kind === 'error' ? 6000 : 3500) {
		const id = next++;
		this.list = [...this.list.slice(-3), { id, kind, text }];
		setTimeout(() => this.dismiss(id), ms);
		return id;
	}
	dismiss(id: number) {
		this.list = this.list.filter((t) => t.id !== id);
	}
}

export const toasts = new Toasts();
export const toast = (text: string, kind?: ToastKind) => toasts.show(text, kind);
