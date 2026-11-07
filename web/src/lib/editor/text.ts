// Converting between the two ways of counting positions in text.
//
// The CRDT counts code points: "👋" is one character. The browser's textarea
// (selectionStart, selectionEnd, string indexes) counts UTF-16 units, where
// "👋" is two. Every position crossing between the editor and the CRDT goes
// through these functions; mixing the two up would put edits next to the
// wrong character as soon as anyone types an emoji.

/** Number of code points in s. */
export function cpLength(s: string): number {
  // Spreading a string iterates it by code point.
  return [...s].length;
}

/**
 * Converts a UTF-16 index into s to a code point index. An index that falls
 * between the two halves of a surrogate pair counts as after the character.
 */
export function utf16ToCp(s: string, index: number): number {
  let cp = 0;
  let u = 0;
  for (const ch of s) {
    if (u >= index) break;
    u += ch.length;
    cp++;
  }
  return cp;
}

/** Converts a code point index into s to a UTF-16 index. */
export function cpToUtf16(s: string, index: number): number {
  let u = 0;
  let cp = 0;
  for (const ch of s) {
    if (cp >= index) break;
    u += ch.length;
    cp++;
  }
  return u;
}
