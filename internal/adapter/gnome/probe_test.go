// The probe and what it reads of the extension, against a recorder in place
// of wctl. No GNOME session and no wctl process: the wctl on PATH is an empty
// file, there only for the lookup, so the default layer runs it.
package gnome_test

import (
	"context"
	"slices"
	"testing"

	"github.com/hk9890/revier/pkg/revier"
)

// The host is usable when wctl is recent enough and its document names the
// extension the shell has loaded. A document that says the two do not match
// still names one, and that stack works: the exit status decides nothing.
func TestProbeNeedsARecentWctlAndALoadedExtension(t *testing.T) {
	cases := []struct {
		name   string
		doc    string
		code   int
		usable bool
	}{
		{
			name:   "matching",
			doc:    `{"wctl":"0.16.0","expects_extension":"16","extension":"16","compatible":true,"capabilities":[]}`,
			usable: true,
		},
		{
			name:   "an extension outside the range wctl was built for",
			doc:    `{"wctl":"0.17.0","expects_extension":"17","extension":"16","compatible":false,"capabilities":[],"message":"wctl 0.17.0 works with extension versions 16 to 17."}`,
			code:   5,
			usable: true,
		},
		{
			name:   "a pre-release",
			doc:    `{"wctl":"0.17.0-rc.1","extension":"17","compatible":true}`,
			usable: true,
		},
		{
			name: "no extension running",
			doc:  `{"wctl":"0.16.0","expects_extension":"16","extension":null,"compatible":false,"message":"Window Control extension is not running."}`,
			code: 5,
		},
		{
			name: "a wctl older than the oldest revier drives",
			doc:  `{"wctl":"0.15.2","expects_extension":"15","extension":"15","compatible":true}`,
		},
		{name: "a wctl that prints no document", doc: "wctl 0.9.0"},
		{name: "a wctl that fails with no document", code: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			onGNOME(t)
			w := &wctl{reply: versionReply(tc.doc, tc.code)}
			err := w.host().Probe(context.Background())
			if (err == nil) != tc.usable {
				t.Errorf("Probe = %v, want usable %v", err, tc.usable)
			}
		})
	}
}

// Hide minimizes and Focus activates with --no-animation where the loaded
// extension reports it can, which keeps the popup in place on Esc and brings
// it back in place. Each capability is its own: an extension can have one
// and not the other. A host that was never probed asks for the animation.
func TestHideAndFocusSkipTheAnimationWhereTheExtensionCan(t *testing.T) {
	cases := []struct {
		name         string
		capabilities string // empty: the host is not probed
		want         []string
	}{
		{
			name:         "both",
			capabilities: `["rules","no-animation","activate-no-animation"]`,
			want:         []string{"minimize 4181121382 --no-animation", "activate 4181121382 --no-animation"},
		},
		{
			name:         "minimize only",
			capabilities: `["no-animation"]`,
			want:         []string{"minimize 4181121382 --no-animation", "activate 4181121382"},
		},
		{
			name:         "neither",
			capabilities: `[]`,
			want:         []string{"minimize 4181121382", "activate 4181121382"},
		},
		{
			name: "not probed",
			want: []string{"minimize 4181121382", "activate 4181121382"},
		},
	}
	ref := revier.TargetRef{Host: "gnome", ID: "4181121382"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			onGNOME(t)
			w := &wctl{reply: versionReply(`{"wctl":"0.16.0","extension":"16","compatible":true,"capabilities":`+tc.capabilities+`}`, 0)}
			h := w.host()
			if tc.capabilities != "" {
				if err := h.Probe(context.Background()); err != nil {
					t.Fatalf("Probe: %v", err)
				}
				w.calls = nil
			}
			if err := h.Hide(context.Background(), ref); err != nil {
				t.Fatalf("Hide: %v", err)
			}
			if err := h.Focus(context.Background(), ref); err != nil {
				t.Fatalf("Focus: %v", err)
			}
			if !slices.Equal(w.calls, tc.want) {
				t.Errorf("calls = %q, want %q", w.calls, tc.want)
			}
		})
	}
}

// A probe that refuses the stack reads none of its capabilities, so the host
// asks for the animation.
func TestARefusedProbeLeavesTheAnimation(t *testing.T) {
	onGNOME(t)
	w := &wctl{reply: versionReply(`{"wctl":"0.15.0","extension":"15","capabilities":["no-animation"]}`, 0)}
	h := w.host()
	if err := h.Probe(context.Background()); err == nil {
		t.Fatal("Probe accepted wctl 0.15.0")
	}
	w.calls = nil
	if err := h.Hide(context.Background(), revier.TargetRef{Host: "gnome", ID: "1"}); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	if want := []string{"minimize 1"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
}
