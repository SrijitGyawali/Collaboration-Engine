// Keeping the local caret and selection in place while remote edits change
// the text around them.
//
// When another user types before your cursor, the text you are looking at
// moves right, so your cursor must move with it, or your next keystroke lands
// in the wrong place. Positions here are code points.

import { cpLength } from "./text";
import type { Change } from "./change";

export interface Selection {
  start: number;
  end: number;
}

/**
 * Where position i ends up after change.
 *
 * - A deletion before i pulls it left; a deletion around i moves it to where
 *   the deleted text was.
 * - An insertion strictly before i pushes it right. An insertion exactly at
 *   i leaves it where it is, so the remote text appears after the caret and
 *   the local user keeps typing in front of it. Each user's own words then
 *   stay together when two people type at the same spot.
 */
export function transformPosition(i: number, change: Change): number {
  const { pos, deleteCount } = change;
  if (deleteCount > 0 && i > pos) {
    i = i >= pos + deleteCount ? i - deleteCount : pos;
  }
  if (change.insert !== "" && pos < i) {
    i += cpLength(change.insert);
  }
  return i;
}

/** Applies transformPosition to both ends of a selection. */
export function transformSelection(sel: Selection, change: Change): Selection {
  return { start: transformPosition(sel.start, change), end: transformPosition(sel.end, change) };
}
