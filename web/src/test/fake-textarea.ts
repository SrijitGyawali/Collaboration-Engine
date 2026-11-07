// A minimal stand-in for HTMLTextAreaElement: just what TextareaBinding uses
// (value, selection, input and composition events, focus). It lets the
// binding be tested in Node without a browser.

type Listener = (event: Event) => void;

export class FakeTextarea {
  value = "";
  selectionStart = 0;
  selectionEnd = 0;
  selectionDirection: "forward" | "backward" | "none" = "none";
  readonly ownerDocument: { activeElement: unknown } = { activeElement: null };
  #listeners = new Map<string, Set<Listener>>();

  addEventListener(type: string, listener: Listener): void {
    if (!this.#listeners.has(type)) this.#listeners.set(type, new Set());
    this.#listeners.get(type)!.add(listener);
  }

  removeEventListener(type: string, listener: Listener): void {
    this.#listeners.get(type)?.delete(listener);
  }

  setSelectionRange(start: number, end: number, direction?: "forward" | "backward" | "none"): void {
    this.selectionStart = start;
    this.selectionEnd = end;
    if (direction) this.selectionDirection = direction;
  }

  focus(): void {
    this.ownerDocument.activeElement = this;
  }

  /** Simulates the user typing text at the caret (replacing any selection). */
  type(text: string): void {
    this.value = this.value.slice(0, this.selectionStart) + text + this.value.slice(this.selectionEnd);
    this.selectionStart = this.selectionEnd = this.selectionStart + text.length;
    this.dispatch("input");
  }

  /** Simulates Backspace at the caret. */
  backspace(): void {
    if (this.selectionStart === this.selectionEnd) {
      // A surrogate pair (emoji) is removed as one character.
      const before = this.value.slice(0, this.selectionStart);
      const last = [...before].pop() ?? "";
      this.selectionStart -= last.length;
    }
    this.type("");
  }

  dispatch(type: string): void {
    for (const listener of this.#listeners.get(type) ?? []) {
      listener(new Event(type));
    }
  }

  /** Casts to the DOM type TextareaBinding expects. */
  asElement(): HTMLTextAreaElement {
    return this as unknown as HTMLTextAreaElement;
  }
}
