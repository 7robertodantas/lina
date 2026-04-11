package internal

import "strings"

// PebbleStorePath returns a directory path for a Pebble store.
// If p already ends with "-pebble" (case-insensitive), or ends with "/pebble" (a directory
// literally named pebble), p is returned unchanged. Otherwise "-pebble" is appended.
func PebbleStorePath(p string) string {
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, "-pebble") {
		return p
	}
	if strings.HasSuffix(lower, "/pebble") {
		return p
	}
	return p + "-pebble"
}
