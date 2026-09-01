package revier_test

import (
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

func TestMatchConstrainsEveryNonEmptyField(t *testing.T) {
	inst := revier.Instance{Title: "session:revier", Class: "kitty", PID: 42}

	cases := []struct {
		name string
		m    revier.Match
		want bool
	}{
		{"title only", revier.Match{Title: "^session:revier$"}, true},
		{"title mismatch", revier.Match{Title: "^session:other$"}, false},
		{"class and title", revier.Match{Class: "^kitty$", Title: "revier"}, true},
		{"class mismatch fails the whole match", revier.Match{Class: "^code$", Title: "revier"}, false},
		{"pid", revier.Match{PID: 42}, true},
		{"pid mismatch", revier.Match{PID: 43}, false},
		{"empty field does not constrain", revier.Match{Class: "", Title: "revier"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := tc.m.Compile()
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if got := c.Matches(inst); got != tc.want {
				t.Errorf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestZeroMatchIsReported(t *testing.T) {
	if !(revier.Match{}).IsZero() {
		t.Error("empty match should report IsZero")
	}
	if (revier.Match{Title: "x"}).IsZero() {
		t.Error("a constrained match should not report IsZero")
	}
}

func TestCompileRejectsBadPattern(t *testing.T) {
	if _, err := (revier.Match{Title: "("}).Compile(); err == nil {
		t.Fatal("want an error for an unparsable pattern")
	}
}

func TestStatusJSONRoundTrip(t *testing.T) {
	for _, s := range []revier.Status{
		revier.StatusUnknown, revier.StatusIdle, revier.StatusRunning, revier.StatusAttention,
	} {
		b, err := s.MarshalJSON()
		if err != nil {
			t.Fatalf("marshal %v: %v", s, err)
		}
		var got revier.Status
		if err := got.UnmarshalJSON(b); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != s {
			t.Errorf("round trip: got %v want %v", got, s)
		}
	}
}

func TestProjectHomeAndTargetLookup(t *testing.T) {
	p := revier.Project{
		Name: "revier",
		Targets: []revier.Target{
			{Name: "editor"},
			{Name: "home", Home: true},
		},
	}
	h, ok := p.Home()
	if !ok || h.Name != "home" {
		t.Fatalf("Home() = %v, %v", h.Name, ok)
	}
	if _, ok := p.Target("editor"); !ok {
		t.Error("Target(editor) not found")
	}
	if _, ok := p.Target("absent"); ok {
		t.Error("Target(absent) should not be found")
	}
	if _, ok := (revier.Project{}).Home(); ok {
		t.Error("a project with no targets has no home")
	}
}
