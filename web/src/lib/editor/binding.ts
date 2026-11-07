// Binds a <textarea> to a CollabSession, in both directions.
//
// Local edits: on every input event the new value is diffed against what the
// textarea showed before (diffText, using the caret to place the edit), and
// the resulting Change goes to the session, which sends the ops.
//
// Remote edits: the session reports the new text and the changes it took.
// The caret and selection are moved through those changes
// (transformSelection) before the new text is shown, so they stay next to
// the same characters.
//
// This is plain DOM code, not React: a React-controlled textarea would reset
// the caret every time it re-rendered with a new value.

import type { CollabSession, RemoteUpdate } from "@/lib/sync/session";
import { diffText } from "./change";
import { transformSelection, type Selection } from "./selection";
import { cpLength, cpToUtf16, utf16ToCp } from "./text";

export interface BindingOptions {
  /** Called with the text's length (in characters) whenever it changes. */
  onLengthChange?: (length: number) => void;
}

export class TextareaBinding {
  #el: HTMLTextAreaElement;
  #session: CollabSession;
  #options: BindingOptions;
  /** The text the textarea showed after the last edit processed. */
  #shown: string;
  #unsubscribe: () => void;

  constructor(el: HTMLTextAreaElement, session: CollabSession, options: BindingOptions = {}) {
    this.#el = el;
    this.#session = session;
    this.#options = options;

    this.#shown = session.text;
    el.value = this.#shown;
    options.onLengthChange?.(cpLength(this.#shown));

    el.addEventListener("input", this.#onInput);
    this.#unsubscribe = session.onRemoteUpdate(this.#onRemote);
  }

  /** Stops listening; the textarea keeps its current text. */
  dispose(): void {
    this.#el.removeEventListener("input", this.#onInput);
    this.#unsubscribe();
  }

  /** Sends the edit the user just made. */
  #onInput = (): void => {
    const value = this.#el.value;
    const change = diffText(this.#shown, value, utf16ToCp(value, this.#el.selectionEnd));
    if (change) {
      this.#session.applyLocal(change);
    }
    this.#shown = value;
    this.#options.onLengthChange?.(cpLength(value));
  };

  /** Shows a remote update, keeping the local caret and selection in place. */
  #onRemote = (update: RemoteUpdate): void => {
    const el = this.#el;
    const before = this.#shown;
    let sel: Selection = { start: utf16ToCp(before, el.selectionStart), end: utf16ToCp(before, el.selectionEnd) };

    // Exact changes when the session has them; otherwise compare the texts.
    const fallback = diffText(before, update.text);
    const changes = update.changes ?? (fallback ? [fallback] : []);
    for (const change of changes) {
      sel = transformSelection(sel, change);
    }

    // Setting the selection of an unfocused textarea can steal focus in
    // some browsers, so only restore it for the focused one.
    const focused = el.ownerDocument.activeElement === el;
    const direction = el.selectionDirection;
    el.value = update.text;
    this.#shown = update.text;
    if (focused) {
      el.setSelectionRange(cpToUtf16(update.text, sel.start), cpToUtf16(update.text, sel.end), direction);
    }
    this.#options.onLengthChange?.(cpLength(update.text));
  };
}
