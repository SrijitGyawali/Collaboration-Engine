package gateway

import (
	"errors"
	"hash/fnv"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// identity is who a connection edits as. There are no accounts yet: every
// visitor is a guest, known by the replica name its CRDT uses and a display
// name, with a colour for its cursor and edits (presence, week 3).
type identity struct {
	Replica string `json:"replica"`
	Name    string `json:"name"`
	Color   string `json:"color"`
}

// replicaPattern is what a replica name may look like. Replica names end up
// in every op ID the client creates, so they stay short and plain.
var replicaPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

const maxNameLength = 40 // characters

// palette is the set of guest colours; each replica always gets the same one.
var palette = []string{
	"#2f6fde", "#d6336c", "#1e8e4e", "#e8590c",
	"#7048e8", "#0c8599", "#c2255c", "#5c940d",
}

var errBadReplica = errors.New("replica must be 1 to 32 letters, digits, '_' or '-'")

// newIdentity checks the replica name a client asked for and fills in its
// display name and colour.
func newIdentity(replica, name string) (identity, error) {
	if !replicaPattern.MatchString(replica) {
		return identity{}, errBadReplica
	}
	return identity{Replica: replica, Name: displayName(name, replica), Color: colorFor(replica)}, nil
}

// displayName cleans up a requested name: control characters are removed,
// spaces trimmed, and the length capped. With nothing left it falls back to
// "Guest" and the start of the replica name, so names are still told apart.
func displayName(name, replica string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if utf8.RuneCountInString(name) > maxNameLength {
		name = strings.TrimSpace(string([]rune(name)[:maxNameLength]))
	}
	if name == "" {
		name = "Guest " + replica[:min(4, len(replica))]
	}
	return name
}

func colorFor(replica string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(replica))
	return palette[h.Sum32()%uint32(len(palette))]
}
