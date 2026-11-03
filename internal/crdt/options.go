package crdt

// DefaultMaxPending is how many ops a document holds back, waiting for their
// dependency, before it starts rejecting new ones.
//
// An honest client rarely gets far ahead: a reconnect replays at most an
// offline session's worth of edits, and those arrive mostly in order. A
// client that keeps sending ops whose parents never arrive is broken or
// hostile, and without a limit it could grow the server's memory forever.
const DefaultMaxPending = 100_000

// Option configures a Document.
type Option func(*Document)

// WithMaxPending sets how many ops may wait for a missing dependency at once.
// When the limit is reached, Apply returns ErrTooManyPending for further ops
// that would have to wait; ops that can be applied right away are unaffected.
func WithMaxPending(n int) Option {
	return func(d *Document) {
		d.maxPending = n
	}
}
