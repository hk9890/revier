// Layer L1: parsing the one pane option that carries a panel's variables.
package tmux_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/tmux"
)

func TestParseVars(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"unset", "", nil},
		{"one pair", "CS_SESSION=abc-123", map[string]string{"CS_SESSION": "abc-123"}},
		{
			"several, as a hook that keeps the others writes them",
			"CS_TAB=1 CS_STATE=attn CS_SESSION=abc-123",
			map[string]string{"CS_TAB": "1", "CS_STATE": "attn", "CS_SESSION": "abc-123"},
		},
		{"extra spacing", "  CS_TAB=1   CS_STATE=attn  ", map[string]string{"CS_TAB": "1", "CS_STATE": "attn"}},
		{"an empty value is still set", "CS_STATE=", map[string]string{"CS_STATE": ""}},
		// A pane holding something that is not a pair must not become a
		// variable named after it.
		{"no pairs at all", "some other tool put this here", nil},
		{"a stray word beside a pair", "junk CS_TAB=1", map[string]string{"CS_TAB": "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tmux.ParseVars(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("parseVars(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
