// One edit to a text, in code point positions: delete deleteCount characters
// at pos, then insert text there. Typing, pasting, deleting a selection and
// typing over a selection are all one Change. Remote ops are reported as
// Changes too, so the editor handles local and remote edits the same way.

import { cpLength } from "./text";

export interface Change {
  /** Where the edit happens, in code points. */
  pos: number;
  /** How many characters are removed at pos. */
  deleteCount: number;
  /** What is inserted at pos (after the removal). */
  insert: string;
}

/**
 * Finds the single edit that turns before into after.
 *
 * A textarea only reports its new value, so the edit is recovered by
 * stripping the longest common prefix and suffix. That is ambiguous when the
 * edit repeats a neighbouring character: typing "a" into "a|a" leaves "aaa"
 * either way. cursor, the caret position (in code points) right after the
 * edit, resolves it: the edit cannot extend past the caret, so the common
 * suffix may not reach before it. Without a cursor the suffix is matched
 * first, so an ambiguous edit is placed as early as possible.
 *
 * Returns null if the texts are equal.
 */
export function diffText(before: string, after: string, cursor?: number): Change | null {
  if (before === after) {
    return null;
  }
  const a = [...before];
  const b = [...after];

  let suffix = 0;
  const maxSuffix = Math.min(a.length, b.length, cursor === undefined ? b.length : Math.max(0, b.length - cursor));
  while (suffix < maxSuffix && a[a.length - 1 - suffix] === b[b.length - 1 - suffix]) {
    suffix++;
  }

  let prefix = 0;
  const maxPrefix = Math.min(a.length, b.length) - suffix;
  while (prefix < maxPrefix && a[prefix] === b[prefix]) {
    prefix++;
  }

  return {
    pos: prefix,
    deleteCount: a.length - suffix - prefix,
    insert: b.slice(prefix, b.length - suffix).join(""),
  };
}

/** Applies change to text; used by tests and as the editor's reference. */
export function applyChange(text: string, change: Change): string {
  const chars = [...text];
  chars.splice(change.pos, change.deleteCount, ...change.insert);
  return chars.join("");
}

/** How much a change changes the text's length, in code points. */
export function lengthDelta(change: Change): number {
  return cpLength(change.insert) - change.deleteCount;
}
