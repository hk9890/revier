package gnome

import (
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// decodeCustom turns a `dconf dump` of the custom-keybindings tree, plus the
// `custom-keybindings` list, into bindings.
//
// The two are read separately because they say different things. The tree
// holds every entry that exists; the list holds the ones GNOME acts on. An
// entry in the tree and not in the list is stored, shown nowhere, and dead -
// the state revier reports as inert.
//
// dconf dump is INI: a [group] per entry, then key=value lines whose values
// are GVariant literals.
//
//	[revier-popup]
//	binding='<Alt>space'
//	command='sh -lc "revier-popup"'
//	name='revier: picker'
func decodeCustom(dump, list []byte) []revier.Binding {
	enabled := map[string]bool{}
	for _, p := range gvariantList(string(list)) {
		enabled[strings.TrimSuffix(p, "/")] = true
	}

	var out []revier.Binding
	var cur *revier.Binding
	flush := func() {
		if cur != nil && cur.Chord != "" {
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, line := range strings.Split(string(dump), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			flush()
			group := strings.Trim(line, "[]")
			path := strings.TrimSuffix(customPath, "/") + "/" + group
			cur = &revier.Binding{
				Where:   path + "/",
				Enabled: enabled[path],
				Source:  revier.BindingCustom,
			}
		case cur == nil:
			continue
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			value = gvariantString(strings.TrimSpace(value))
			switch strings.TrimSpace(key) {
			case "binding":
				cur.Chord = value
			case "command":
				cur.Command = value
			case "name":
				cur.Label = value
			}
		}
	}
	flush()
	return out
}

// decodeBuiltin turns `gsettings list-recursively <schema>` into bindings.
// Each line is `schema key value`, and a shortcut's value is an array of
// accelerators. Every other setting in the schema - a boolean, a number - is
// skipped by that shape alone, so no list of key names has to be maintained.
//
//	org.gnome.desktop.wm.keybindings close ['<Alt>F4', '<Super>q']
func decodeBuiltin(raw []byte) []revier.Binding {
	var out []revier.Binding
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(fields) != 3 {
			continue
		}
		schema, key, value := fields[0], fields[1], fields[2]
		for _, chord := range gvariantList(value) {
			if chord == "" {
				continue
			}
			out = append(out, revier.Binding{
				Chord:   chord,
				Label:   key,
				Where:   schema,
				Enabled: true,
				Source:  revier.BindingBuiltin,
			})
		}
	}
	return out
}

// gvariantList reads a GVariant array of strings. An empty array is written
// `@as []`, which carries no elements and needs no special case beyond having
// no quoted strings in it.
//
// A value that is not an array reads as nothing at all. That is what keeps a
// list of shortcut key names out of this adapter: a schema holds shortcuts and
// ordinary settings side by side, and the shape tells them apart. The check is
// on the start of the value, not on a bracket anywhere in it - a string
// setting holding brackets would otherwise yield chords nobody bound.
func gvariantList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "@as") {
		return nil
	}
	open := strings.IndexByte(s, '[')
	closeAt := strings.LastIndexByte(s, ']')
	if open < 0 || closeAt < open {
		return nil
	}
	var out []string
	for _, part := range splitTop(s[open+1 : closeAt]) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, gvariantString(part))
	}
	return out
}

// splitTop splits on commas that are not inside a quoted string. An
// accelerator holds no comma, but a custom shortcut's command can, and the
// same helper reads both.
func splitTop(s string) []string {
	var out []string
	var buf strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == '\\' && i+1 < len(s):
			buf.WriteByte(c)
			i++
			buf.WriteByte(s[i])
		case quote != 0 && c == quote:
			quote = 0
			buf.WriteByte(c)
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
			buf.WriteByte(c)
		case quote == 0 && c == ',':
			out = append(out, buf.String())
			buf.Reset()
		default:
			buf.WriteByte(c)
		}
	}
	out = append(out, buf.String())
	return out
}

// gvariantString unquotes one GVariant string literal. Values reach revier
// with their quoting intact, and a command compared with its quotes still on
// never equals the one revier would install.
func gvariantString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q != '\'' && q != '"') || s[len(s)-1] != q {
		return s
	}
	body := s[1 : len(s)-1]
	var out strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' && i+1 < len(body) {
			i++
			out.WriteByte(body[i])
			continue
		}
		out.WriteByte(body[i])
	}
	return out.String()
}
