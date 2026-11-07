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
import { diffText, type Change } from "./change";
import { transformPosition, transformSelection, type Selection } from "./selection";
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
  /** True while an input method (IME) is composing text. */
  #composing = false;
  /** A remote update arrived during composition and is not shown yet. */
  #deferred = false;
  /**
   * The changes of the remote updates skipped during composition, in order
   * (relative to #shown, since local edits are not sent while composing);
   * null if any of them had no exact changes.
   */
  #deferredChanges: Change[] | null = [];

  constructor(el: HTMLTextAreaElement, session: CollabSession, options: BindingOptions = {}) {
    this.#el = el;
    this.#session = session;
    this.#options = options;

    this.#shown = session.text;
    el.value = this.#shown;
    options.onLengthChange?.(cpLength(this.#shown));

    el.addEventListener("input", this.#onInput);
    el.addEventListener("compositionstart", this.#onCompositionStart);
    el.addEventListener("compositionend", this.#onCompositionEnd);
    this.#unsubscribe = session.onRemoteUpdate(this.#onRemote);
  }

  /** Stops listening; the textarea keeps its current text. */
  dispose(): void {
    this.#el.removeEventListener("input", this.#onInput);
    this.#el.removeEventListener("compositionstart", this.#onCompositionStart);
    this.#el.removeEventListener("compositionend", this.#onCompositionEnd);
    this.#unsubscribe();
  }

  // Input methods for Japanese, Chinese, Korean and others build a character
  // in several steps ("k", "か", "漢") inside the textarea before the user
  // commits it. The intermediate text is not an edit yet, so nothing is sent
  // until composition ends. Replacing the textarea's value mid-composition
  // would also cancel the input method, so remote updates wait too.

  #onCompositionStart = (): void => {
    this.#composing = true;
  };

  #onCompositionEnd = (): void => {
    this.#composing = false;
    if (!this.#deferred) {
      this.#onInput(); // send the committed text as one edit
      return;
    }

    // Remote edits arrived while composing. The session already has them,
    // but the textarea does not, so the committed edit's position refers to
    // the older text. Move it through those edits before sending it, or it
    // would land in the wrong place whenever a remote edit was before it.
    const el = this.#el;
    const value = el.value;
    const local = diffText(this.#shown, value, utf16ToCp(value, el.selectionEnd));
    const remote = this.#deferredChanges ?? [diffText(this.#shown, this.#session.text)].filter((c) => c !== null);
    this.#deferred = false;
    this.#deferredChanges = [];

    if (local) {
      let start = local.pos;
      let end = local.pos + local.deleteCount;
      for (const change of remote) {
        start = transformPosition(start, change);
        end = transformPosition(end, change);
      }
      this.#session.applyLocal({ pos: start, deleteCount: Math.max(0, end - start), insert: local.insert });
    }

    // Show the session's text: the textarea's text plus the remote edits.
    this.#shown = value;
    this.#show(this.#session.text, null);
  };

  /** Sends the edit the user just made. */
  #onInput = (): void => {
    if (this.#composing) return;
    const value = this.#el.value;
    const change = diffText(this.#shown, value, utf16ToCp(value, this.#el.selectionEnd));
    if (change) {
      this.#session.applyLocal(change);
    }
    this.#shown = value;
    this.#options.onLengthChange?.(cpLength(value));
  };

  #onRemote = (update: RemoteUpdate): void => {
    if (this.#composing) {
      this.#deferred = true;
      if (this.#deferredChanges !== null) {
        this.#deferredChanges = update.changes ? [...this.#deferredChanges, ...update.changes] : null;
      }
      return;
    }
    this.#show(update.text, update.changes);
  };

  /**
   * Shows text, keeping the local caret and selection in place: they are
   * moved through changes, or through one diff of the old and new text when
   * the exact changes are not known (null).
   */
  #show(text: string, changes: Change[] | null): void {
    const el = this.#el;
    const before = this.#shown;
    let sel: Selection = { start: utf16ToCp(before, el.selectionStart), end: utf16ToCp(before, el.selectionEnd) };

    if (changes === null) {
      const diff = diffText(before, text);
      changes = diff ? [diff] : [];
    }
    for (const change of changes) {
      sel = transformSelection(sel, change);
    }

    // Setting the selection of an unfocused textarea can steal focus in
    // some browsers, so only restore it for the focused one.
    const focused = el.ownerDocument.activeElement === el;
    const direction = el.selectionDirection;
    el.value = text;
    this.#shown = text;
    if (focused) {
      el.setSelectionRange(cpToUtf16(text, sel.start), cpToUtf16(text, sel.end), direction);
    }
    this.#options.onLengthChange?.(cpLength(text));
  }
}
