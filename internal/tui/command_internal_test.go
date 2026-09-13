package tui

import (
	"slices"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	for line, want := range map[string][]string{
		"git pull":                    {"git", "pull"},
		"  git   pull  ":              {"git", "pull"},
		`git commit -m "wip: a fix"`:  {"git", "commit", "-m", "wip: a fix"},
		`sh -c 'echo "$1"'`:           {"sh", "-c", `echo "$1"`},
		`echo a\ b`:                   {"echo", "a b"},
		`echo ""`:                     {"echo", ""},
		"wl-copy {{ .Path }}":         {"wl-copy", "{{ .Path }}"},
		"code {{ .Path }}/src --wait": {"code", "{{ .Path }}/src", "--wait"},
		"":                            nil,
	} {
		got, err := splitCommand(line)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("splitCommand(%q) = %q, %v; want %q", line, got, err, want)
		}
	}
	for _, line := range []string{`echo "open`, "echo 'open", "echo {{ .Path"} {
		if _, err := splitCommand(line); err == nil {
			t.Errorf("splitCommand(%q) accepted", line)
		}
	}
}

// What joinCommand writes, splitCommand reads back as the same argv.
func TestJoinCommandRoundTrips(t *testing.T) {
	for _, argv := range [][]string{
		{"git", "pull"},
		{"git", "commit", "-m", "wip: a fix"},
		{"sh", "-c", `echo "it's"`},
		{"echo", ""},
		{"wl-copy", "{{ .Path }}"},
		{"echo", `back\slash`},
	} {
		got, err := splitCommand(joinCommand(argv))
		if err != nil || !slices.Equal(got, argv) {
			t.Errorf("splitCommand(joinCommand(%q)) = %q, %v", argv, got, err)
		}
	}
	if got := joinCommand([]string{"git", "commit", "-m", "wip: a fix"}); got != "git commit -m 'wip: a fix'" {
		t.Errorf("joinCommand = %q, want quotes only where a word needs them", got)
	}
}
