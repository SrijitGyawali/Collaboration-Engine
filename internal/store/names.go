package store

import "regexp"

// documentID is the rule the documents_id_format check enforces (migration
// 000002): 1 to 40 letters, digits, "_" or "-".
var documentID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

// ValidDocumentID reports whether id is a document name the schema accepts.
// The database stays the authority; this lets callers reject a bad name
// before a round trip, and lets stores without PostgreSQL apply the same
// rule.
func ValidDocumentID(id string) bool {
	return documentID.MatchString(id)
}
