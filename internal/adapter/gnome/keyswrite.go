package gnome

import (
	"context"
	"fmt"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Writer changes GNOME's keyboard shortcuts.
//
// It is a type of its own, and not three more methods on Keys, so that the
// read-only guard Keys carries stays true. `revier keys status` is handed a
// Keys and cannot reach a write from it whatever a later change does here.
//
// Writer embeds Keys, so it reads with the same read-only guard and writes
// only through its own.
type Writer struct{ Keys }

// writeCommands is every command Writer may run. dconf reset and gsettings set
// are here, and nothing else is: `dconf load` would rewrite a whole tree from
// one file, which is more than any operation on this type asks for.
var writeCommands = map[string][]string{
	"gsettings": {"get", "list-recursively", "set"},
	"dconf":     {"dump", "read", "list", "write", "reset"},
}

func (w *Writer) write(ctx context.Context, bin string, args ...string) ([]byte, error) {
	if err := allowed(writeCommands, bin, args); err != nil {
		return nil, err
	}
	run := w.run
	if run == nil {
		run = execRun
	}
	return run(ctx, bin, args...)
}

// Bind writes the three keys an entry is made of, then puts its path in the
// list GNOME acts on. The order matters: a path in the list whose entry is not
// yet written is a shortcut with no command, which GNOME reports as broken.
func (w *Writer) Bind(ctx context.Context, b revier.Binding) error {
	if b.ID == "" {
		return fmt.Errorf("gnome keys: a shortcut needs an id")
	}
	if b.Chord == "" || b.Command == "" {
		return fmt.Errorf("gnome keys: %s needs a key and a command", b.ID)
	}
	path := customPath + b.ID + "/"
	for key, value := range map[string]string{
		"name": b.Label, "binding": b.Chord, "command": b.Command,
	} {
		if _, err := w.write(ctx, w.dconf(), "write", path+key, quote(value)); err != nil {
			return err
		}
	}
	return w.setEnabled(ctx, path, true)
}

// Disable stops a shortcut firing and destroys nothing.
//
// A custom entry loses its place in the list GNOME acts on; every word of it
// stays in the store, so whatever wrote it puts it back. A built-in is a
// setting rather than an entry, so the setting is emptied - and the command
// that returns it is what the caller prints, because nothing else will.
func (w *Writer) Disable(ctx context.Context, b revier.Binding) error {
	if b.Source == revier.BindingBuiltin {
		_, err := w.write(ctx, w.gsettings(), "set", b.Where, b.Label, "@as []")
		return err
	}
	if b.Where == "" {
		return fmt.Errorf("gnome keys: a shortcut needs a path to be switched off")
	}
	return w.setEnabled(ctx, b.Where, false)
}

// Remove deletes an entry: out of the list GNOME acts on, then the whole
// subtree. It is refused for a built-in, which has no entry to delete and
// whose setting Disable empties instead.
func (w *Writer) Remove(ctx context.Context, b revier.Binding) error {
	if b.Source == revier.BindingBuiltin {
		return fmt.Errorf("gnome keys: %s is a desktop setting, not an entry to remove", b.Label)
	}
	if b.Where == "" {
		return fmt.Errorf("gnome keys: a shortcut needs a path to be removed")
	}
	if err := w.setEnabled(ctx, b.Where, false); err != nil {
		return err
	}
	_, err := w.write(ctx, w.dconf(), "reset", "-f", b.Where)
	return err
}

// setEnabled rewrites the custom-keybindings list with the path in it or out
// of it. The list is read first and written whole, because that is the shape
// GNOME stores it in; a path that is already where it should be is left alone,
// so a second run writes nothing.
func (w *Writer) setEnabled(ctx context.Context, path string, want bool) error {
	raw, err := w.write(ctx, w.gsettings(), "get", customSchema, "custom-keybindings")
	if err != nil {
		return err
	}
	current := gvariantList(string(raw))

	out := make([]string, 0, len(current)+1)
	found := false
	for _, p := range current {
		// The trailing slash is compared away, as decodeCustom compares it
		// away when it reads the same list. GNOME writes the path with one
		// and accepts it without; a shortcut listed without one would
		// otherwise be readable as switched on and impossible to switch off.
		if samePath(p, path) {
			found = true
			if !want {
				continue
			}
		}
		out = append(out, p)
	}
	switch {
	case want && !found:
		out = append(out, path)
	case want && found, !want && !found:
		return nil // already as it should be
	}

	_, err = w.write(ctx, w.gsettings(), "set", customSchema, "custom-keybindings", gvariantArray(out))
	return err
}

// samePath compares two dconf directory paths, one of which may have been
// written without its trailing slash.
func samePath(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

// quote renders a GVariant string literal. GNOME shortcut commands carry
// double quotes and shell syntax, and a single quote or a backslash in one
// would be read as syntax rather than as text without this.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}

func gvariantArray(in []string) string {
	if len(in) == 0 {
		return "@as []"
	}
	quoted := make([]string, len(in))
	for i, s := range in {
		quoted[i] = quote(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
