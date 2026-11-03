package gnome

import "testing"

// A value too short to be a quoted string, a lone quote among them, comes
// back as it is: there are no quotes around it to take off.
func TestAValueTooShortToBeQuotedComesBackAsItIs(t *testing.T) {
	for in, want := range map[string]string{
		"":       "",
		"'":      "'",
		`"`:      `"`,
		" ' ":    "'",
		"''":     "",
		"'a'":    "a",
		`'a\'b'`: "a'b",
	} {
		if got := gvariantString(in); got != want {
			t.Errorf("gvariantString(%q) = %q, want %q", in, got, want)
		}
	}
}
