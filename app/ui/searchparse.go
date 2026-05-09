package ui

import "strings"

// Query is the result of parsing the raw `q` URL parameter from the search box.
// It separates free-text terms from tag operators so handlers can route each
// part to the right place: Text → LIKE %q% in storage, Tags → JOIN against
// the tags table.
//
// Examples:
//
//	"verify"               → Text="verify",       Tags=[]
//	"tag:auth"             → Text="",             Tags=["auth"]
//	"verify tag:auth"      → Text="verify",       Tags=["auth"]
//	"tag:\"two words\""    → Text="",             Tags=["two words"]
//	"x tag:a tag:b y"      → Text="x y",          Tags=["a","b"]
type Query struct {
	Text string
	Tags []string
}

// ParseQuery walks q and pulls out tokens of the form `tag:foo` or
// `tag:"two words"`. Everything else is treated as free-text and joined back
// with single spaces. The parser is whitespace-tolerant and robust against
// unbalanced quotes (an unclosed quote consumes the remainder of the string).
//
// Free-text is single-word-ish — multi-word search terms are joined back
// with spaces but matched via a single LIKE %text% in storage, so phrase
// matching is incidental, not strictly preserved. Good enough for a mock.
func ParseQuery(q string) Query {
	q = strings.TrimSpace(q)
	if q == "" {
		return Query{}
	}

	var (
		out      Query
		textTerms []string
		i         int
	)
	for i < len(q) {
		// skip whitespace
		for i < len(q) && q[i] == ' ' {
			i++
		}
		if i >= len(q) {
			break
		}

		// Try to match "tag:" prefix.
		if hasPrefixAt(q, i, "tag:") {
			j := i + len("tag:")
			tagValue, next := readTokenValue(q, j)
			if tagValue != "" {
				out.Tags = append(out.Tags, tagValue)
			}
			i = next
			continue
		}

		// Otherwise consume a plain whitespace-delimited word (no special
		// quote handling for free-text terms).
		j := i
		for j < len(q) && q[j] != ' ' {
			j++
		}
		textTerms = append(textTerms, q[i:j])
		i = j
	}

	out.Text = strings.Join(textTerms, " ")
	return out
}

// readTokenValue reads the value following a "tag:" prefix starting at index
// i in s. If the value is double-quoted, returns the contents between the
// quotes and the index after the closing quote (or end of string if unclosed).
// Otherwise reads until whitespace.
func readTokenValue(s string, i int) (value string, next int) {
	if i >= len(s) {
		return "", i
	}
	if s[i] == '"' {
		// quoted: find closing quote
		j := i + 1
		for j < len(s) && s[j] != '"' {
			j++
		}
		value = s[i+1 : j]
		if j < len(s) {
			j++ // skip closing quote
		}
		return value, j
	}
	j := i
	for j < len(s) && s[j] != ' ' {
		j++
	}
	return s[i:j], j
}

// hasPrefixAt reports whether s[i:] starts with prefix.
func hasPrefixAt(s string, i int, prefix string) bool {
	return i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix
}

// BuildQuery is the inverse of ParseQuery: assemble a Query back into a string
// suitable for the search box. Free-text first, then tag operators, separated
// by single spaces. Tags with embedded spaces are quoted.
func BuildQuery(text string, tags []string) string {
	parts := []string{}
	if text = strings.TrimSpace(text); text != "" {
		parts = append(parts, text)
	}
	for _, t := range tags {
		if t == "" {
			continue
		}
		if strings.ContainsRune(t, ' ') {
			parts = append(parts, "tag:\""+t+"\"")
		} else {
			parts = append(parts, "tag:"+t)
		}
	}
	return strings.Join(parts, " ")
}

// HasTag reports whether q already contains tag:name (case-sensitive,
// whole-token match). Used by the sidebar to mark a tag entry as active.
func (q Query) HasTag(name string) bool {
	for _, t := range q.Tags {
		if t == name {
			return true
		}
	}
	return false
}

// ToggleTag returns the q string that would result from clicking the given
// tag in the sidebar: removes it if present, appends if absent. Multi-select
// helper, kept for completeness even though SelectTag is what the sidebar
// uses today.
func (q Query) ToggleTag(name string) string {
	tags := make([]string, 0, len(q.Tags)+1)
	found := false
	for _, t := range q.Tags {
		if t == name {
			found = true
			continue
		}
		tags = append(tags, t)
	}
	if !found {
		tags = append(tags, name)
	}
	return BuildQuery(q.Text, tags)
}

// SelectTag is the single-select counterpart of ToggleTag. Clicking a tag
// that isn't currently the only filter replaces the entire tag set with just
// that name. Clicking the already-active tag clears the filter (returns the
// query with no tags). Free-text terms in q are preserved either way.
func (q Query) SelectTag(name string) string {
	if len(q.Tags) == 1 && q.Tags[0] == name {
		return BuildQuery(q.Text, nil) // toggle-off
	}
	return BuildQuery(q.Text, []string{name})
}
