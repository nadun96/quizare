// Promise-based confirmation dialog replacing window.confirm (D-39): a real
// modal <dialog> with a clear title, the consequence, and labelled buttons.
export type ConfirmOptions = { title: string; body?: string; confirm?: string; cancel?: string; danger?: boolean };
type Pending = ConfirmOptions & { resolve: (ok: boolean) => void };

class Dialogs {
	current = $state<Pending | null>(null);
	confirm(o: ConfirmOptions): Promise<boolean> {
		this.current?.resolve(false);
		return new Promise((resolve) => (this.current = { ...o, resolve }));
	}
	close(ok: boolean) {
		const c = this.current;
		this.current = null;
		c?.resolve(ok);
	}
}

export const dialogs = new Dialogs();
export const confirmDialog = (o: ConfirmOptions) => dialogs.confirm(o);
