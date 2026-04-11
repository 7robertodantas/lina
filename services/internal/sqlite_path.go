package internal

import "strings"

// SQLiteDBPath returns a path for a single SQLite database file. If p does not already
// end with ".db" (case-insensitive), ".db" is appended.
func SQLiteDBPath(p string) string {
	if strings.HasSuffix(strings.ToLower(p), ".db") {
		return p
	}
	return p + ".db"
}
