// Promise-based dialogs replacing window.confirm and window.prompt (D-39,
// D-48): a real modal <dialog> with a clear title, the consequence, a
// labelled field when an answer is needed, and labelled buttons.
export type ConfirmOptions = { title: string; body?: string; confirm?: string; cancel?: string; danger?: boolean };
export type PromptOptions = ConfirmOptions & {
	/** Field label (the title is used when missing). */
	label?: string;
	value?: string;
	placeholder?: string;
	/** Allow an empty answer (e.g. "clear the nickname"). */
	allowEmpty?: boolean;
	maxlength?: number;
	inputmode?: 'text' | 'decimal' | 'numeric';
	/** Several lines (feedback, reasons). */
	multiline?: boolean;
};
type Pending = PromptOptions & { prompt: boolean; resolve: (v: string | null) => void };

class Dialogs {
	current = $state<Pending | null>(null);
	/** Text in the prompt field. */
	value = $state('');
	confirm(o: ConfirmOptions): Promise<boolean> {
		return this.open({ ...o, prompt: false }).then((v) => v !== null);
	}
	prompt(o: PromptOptions): Promise<string | null> {
		this.value = o.value ?? '';
		return this.open({ confirm: 'Save', ...o, prompt: true });
	}
	private open(o: Omit<Pending, 'resolve'>): Promise<string | null> {
		this.current?.resolve(null);
		return new Promise((resolve) => (this.current = { ...o, resolve }));
	}
	/** ok=false cancels; a prompt resolves to the field's text. */
	close(ok: boolean) {
		const c = this.current;
		if (ok && c?.prompt && !c.allowEmpty && !this.value.trim()) return; // nothing to save yet
		this.current = null;
		c?.resolve(ok ? (c.prompt ? this.value : '') : null);
	}
}

export const dialogs = new Dialogs();
export const confirmDialog = (o: ConfirmOptions) => dialogs.confirm(o);
/** Asks for a line of text; resolves to null when cancelled. */
export const promptDialog = (o: PromptOptions) => dialogs.prompt(o);
