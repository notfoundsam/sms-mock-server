package httpapi

import "strings"

// parseTagsHeader extracts user-defined tags from the X-Tags HTTP header.
//
// Format: a single header `X-Tags: tag1, tag2` carrying comma-separated
// values. Whitespace around each value is trimmed; empty values and
// duplicates are dropped. Names are lowercased so client "Verification" and
// "verification" collapse to one tag.
//
// Returns nil when the header is absent or yields no tags.
func parseTagsHeader(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
