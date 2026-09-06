package gnome

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Keys reads GNOME's keyboard shortcuts. It is a separate type from Host, and
// constructed separately, because it needs neither wctl nor the window control
// extension: shortcuts stay readable when the extension is down, which is
// exactly when a user asks what is bound.
//
// Everything here is a read. gsettings and dconf both write as well, so the
// commands this type may run are listed in one place, readCommands, and
// checked before each one runs.
type Keys struct {
	// GSettings and Dconf override the binaries, for tests.
	GSettings string
	Dconf     string

	// run executes a command and returns its standard output. Tests replace
	// it; production leaves it nil and gets execRun.
	run func(ctx context.Context, bin string, args ...string) ([]byte, error)
}

func (k *Keys) Name() string { return "gnome" }

// Where GNOME keeps the shortcuts a user added.
const (
	customSchema = "org.gnome.settings-daemon.plugins.media-keys"
	customPath   = "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/"
	customChild  = "org.gnome.settings-daemon.plugins.media-keys.custom-keybinding"
)

// builtinSchemas are the desktop's own shortcuts. A chord held here cannot be
// claimed without changing a GNOME default, which is why revier reports them
// rather than working around them. A schema this GNOME does not have is
// skipped, so an old or a new release both read.
var builtinSchemas = []string{
	"org.gnome.desktop.wm.keybindings",
	"org.gnome.shell.keybindings",
	"org.gnome.mutter.keybindings",
	"org.gnome.mutter.wayland.keybindings",
	customSchema,
}

func (k *Keys) gsettings() string {
	if k.GSettings != "" {
		return k.GSettings
	}
	return "gsettings"
}

func (k *Keys) dconf() string {
	if k.Dconf != "" {
		return k.Dconf
	}
	return "dconf"
}

// Probe reports GNOME shortcuts readable when gsettings is on PATH and the
// session is GNOME. dconf is checked too: without it the entries that exist in
// the store but are switched off cannot be seen, and reporting a chord as free
// when a disabled entry sits on it is worse than reporting nothing.
func (k *Keys) Probe(ctx context.Context) error {
	for _, bin := range []string{k.gsettings(), k.dconf()} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not on PATH: %w", bin, err)
		}
	}
	if d := os.Getenv("XDG_CURRENT_DESKTOP"); d != "" && !strings.Contains(strings.ToUpper(d), "GNOME") {
		return fmt.Errorf("XDG_CURRENT_DESKTOP is %q, not GNOME", d)
	}
	_, err := k.exec(ctx, k.gsettings(), "get", customSchema, "custom-keybindings")
	return err
}

// List reads every shortcut in a fixed number of commands: one dconf dump for
// the custom entries, one gsettings get for which of them are switched on, and
// one gsettings list-recursively per desktop schema. Nothing here scales with
// the number of projects.
func (k *Keys) List(ctx context.Context) ([]revier.Binding, error) {
	dump, err := k.exec(ctx, k.dconf(), "dump", customPath)
	if err != nil {
		return nil, err
	}
	enabled, err := k.exec(ctx, k.gsettings(), "get", customSchema, "custom-keybindings")
	if err != nil {
		return nil, err
	}
	out := decodeCustom(dump, enabled)

	for _, schema := range builtinSchemas {
		raw, err := k.exec(ctx, k.gsettings(), "list-recursively", schema)
		if err != nil {
			// A schema this GNOME does not ship is not a failure. Every other
			// schema still reports, and a missing one holds no chord.
			continue
		}
		out = append(out, decodeBuiltin(raw)...)
	}
	return out, nil
}

// readCommands is every command this type is allowed to run. gsettings set and
// dconf write exist and are one typo away, so the allowed forms are named here
// and checked, rather than trusted to the call sites.
var readCommands = map[string][]string{
	"gsettings": {"get", "list-recursively"},
	"dconf":     {"dump", "read", "list"},
}

func (k *Keys) exec(ctx context.Context, bin string, args ...string) ([]byte, error) {
	if err := readOnly(bin, args); err != nil {
		return nil, err
	}
	run := k.run
	if run == nil {
		run = execRun
	}
	return run(ctx, bin, args...)
}

// readOnly refuses anything that is not one of the read subcommands. The
// binary is matched on its base name, so a test pointing GSettings at a stub
// is held to the same rule as the real thing.
func readOnly(bin string, args []string) error {
	base := bin
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, "-stub")
	allowed, ok := readCommands[base]
	if !ok {
		return fmt.Errorf("gnome keys: %s is not a command this reads with", bin)
	}
	if len(args) == 0 {
		return fmt.Errorf("gnome keys: %s needs a subcommand", bin)
	}
	for _, a := range allowed {
		if args[0] == a {
			return nil
		}
	}
	return fmt.Errorf("gnome keys: %s %s is not a read", base, args[0])
}

func execRun(ctx context.Context, bin string, args ...string) ([]byte, error) {
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, bin, args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}
