package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseQuery(t *testing.T) {
	cases := []struct {
		in       string
		wantText string
		wantTags []string
	}{
		{"", "", nil},
		{"   ", "", nil},
		{"verify", "verify", nil},
		{"tag:auth", "", []string{"auth"}},
		{"verify tag:auth", "verify", []string{"auth"}},
		{"tag:auth verify", "verify", []string{"auth"}},
		{"x tag:a tag:b y", "x y", []string{"a", "b"}},
		{`tag:"two words"`, "", []string{"two words"}},
		{`hello tag:"two words" world`, "hello world", []string{"two words"}},
		{`tag:"unclosed`, "", []string{"unclosed"}}, // tolerant of unbalanced quotes
		{"tag:", "", nil}, // empty value dropped
		{"  tag:a   tag:b  ", "", []string{"a", "b"}},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseQuery(tc.in)
			assert.Equal(t, tc.wantText, got.Text, "Text for %q", tc.in)
			assert.Equal(t, tc.wantTags, got.Tags, "Tags for %q", tc.in)
		})
	}
}

func TestBuildQuery(t *testing.T) {
	assert.Empty(t, BuildQuery("", nil))
	assert.Equal(t, "verify", BuildQuery("verify", nil))
	assert.Equal(t, "tag:a", BuildQuery("", []string{"a"}))
	assert.Equal(t, "verify tag:a tag:b", BuildQuery("verify", []string{"a", "b"}))
	assert.Equal(t, `tag:"two words"`, BuildQuery("", []string{"two words"}))
	assert.Equal(t, "tag:a", BuildQuery("", []string{"", "a", ""}), "empty tags dropped")
}

func TestQueryHasTag(t *testing.T) {
	q := ParseQuery("verify tag:auth tag:high")
	assert.True(t, q.HasTag("auth"))
	assert.True(t, q.HasTag("high"))
	assert.False(t, q.HasTag("low"))
	assert.False(t, q.HasTag(""))
}

func TestQuerySelectTag(t *testing.T) {
	// No active tag → click adds it
	q := ParseQuery("verify")
	assert.Equal(t, "verify tag:auth", q.SelectTag("auth"))

	// Different tag is active → click replaces it (single-select)
	q = ParseQuery("verify tag:auth")
	assert.Equal(t, "verify tag:high", q.SelectTag("high"))

	// Multiple active tags → click collapses to just the clicked one
	q = ParseQuery("tag:auth tag:high tag:critical")
	assert.Equal(t, "tag:high", q.SelectTag("high"))

	// Clicking the only active tag clears the filter (toggle-off)
	q = ParseQuery("tag:auth")
	assert.Empty(t, q.SelectTag("auth"))

	// Toggle-off preserves free-text
	q = ParseQuery("verify tag:auth")
	assert.Equal(t, "verify", q.SelectTag("auth"))

	// Tag with spaces gets quoted
	q = ParseQuery("")
	assert.Equal(t, `tag:"two words"`, q.SelectTag("two words"))
}
