package crdt

import "slices"

// The document's characters, tombstones included, are stored in d.order in
// the order they appear in the text. This file holds the two operations on
// that list: finding a character, and finding where a new one goes.
//
// Conceptually the text is still the tree of "inserted after" links
// described on Document. Keeping the tree flattened in reading order makes
// rendering a plain loop and lets positions be looked up without walking a
// tree.

// indexOf returns the index of character id in d.order, or -1 for Root (the
// document start comes before index 0).
//
// It scans the list, so it costs O(n) in the worst case.
func (d *Document) indexOf(id ID) int {
	if id.IsRoot() {
		return -1
	}
	return slices.IndexFunc(d.order, func(e *element) bool { return e.id == id })
}

// insertPosition returns the index in d.order where the character inserted
// by op belongs.
//
// The rule (RGA's): start right after the parent, skip every character whose
// ID is higher than the new one, and stop at the first lower one.
//
// Why this gives the same order as the tree, with siblings sorted highest ID
// first:
//
//   - Right after the parent come the parent's existing children, each
//     followed by everything inserted after it. A child with a higher ID
//     than the new character must stay in front of it, and so must
//     everything inserted after that child. All of those have even higher
//     IDs, because a character is always newer than its parent (Validate
//     enforces it). So skipping "higher IDs" skips exactly those subtrees.
//   - The first lower ID is either a sibling with a lower ID (the new
//     character goes before it) or the first character after the parent's
//     whole subtree. That character follows one of the parent's ancestors,
//     so its ID is lower than that ancestor's, and therefore lower than the
//     new character's. Either way the scan stops in the right place.
func (d *Document) insertPosition(op Op) int {
	i := d.indexOf(op.After) + 1
	for i < len(d.order) && d.order[i].id.Compare(op.ID) > 0 {
		i++
	}
	return i
}
