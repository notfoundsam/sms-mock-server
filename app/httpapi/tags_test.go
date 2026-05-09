package httpapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTagsHeader(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"verification", []string{"verification"}},
		{"verification, auth", []string{"verification", "auth"}},
		{" verification , auth ", []string{"verification", "auth"}},
		{"Verification, AUTH", []string{"verification", "auth"}}, // lowercased
		{"a,a,a", []string{"a"}},                                 // dedup
		{"a,,b", []string{"a", "b"}},                             // empty parts skipped
	}
	for _, tc := range cases {
		got := parseTagsHeader(tc.in)
		assert.Equal(t, tc.want, got, "input=%q", tc.in)
	}
}
