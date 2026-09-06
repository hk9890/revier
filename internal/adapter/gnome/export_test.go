package gnome

import (
	"context"

	"github.com/hk9890/revier/pkg/revier"
)

// SetRunner replaces command execution, so the L3 tests drive List against
// recorded gsettings and dconf output with no GNOME session. The read-only
// check still applies to whatever the runner is asked to run, which is what
// lets a test prove this type cannot write.
func (k *Keys) SetRunner(fn func(ctx context.Context, bin string, args ...string) ([]byte, error)) {
	k.run = fn
}

// ReadOnly exposes the guard that stands between this type and the two tools'
// write subcommands.
func ReadOnly(bin string, args []string) error { return readOnly(bin, args) }

// DecodeCustom and DecodeBuiltin expose the parsers to the L3 tests.
func DecodeCustom(dump, list []byte) []revier.Binding { return decodeCustom(dump, list) }

func DecodeBuiltin(raw []byte) []revier.Binding { return decodeBuiltin(raw) }

// Decode and DecodeFocused expose the parsers to the L3 tests, which run
// against recorded wctl output rather than a live GNOME session.
func (h *Host) Decode(raw []byte) ([]revier.Instance, error) { return h.decode(raw) }

func (h *Host) DecodeFocused(raw []byte) (revier.TargetRef, error) { return h.decodeFocused(raw) }
