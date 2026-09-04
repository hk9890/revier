// Command migrate-sessions converts the shell .session files of the sessions
// system into revier project TOML.
//
// One-shot and throwaway. It exists to be run once against
// ~/setup/dotfiles/kitty/.config/kitty/sessions and is not part of the product:
// revier reads TOML and nothing else, and there is no backward compatibility
// with the KEY=value form (docs/design/decisions.md D5). The transports it
// refuses to translate are D6's.
//
// Usage:
//
//	go run ./scripts/migrate-sessions -out /tmp/projects        # convert and check
//	go run ./scripts/migrate-sessions -out ~/.config/revier/projects
//
// Every written file is loaded with config.LoadProjects before the program
// reports success, so a project that would be rejected at a keypress is caught
// here instead.
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hk9890/revier/internal/config"
)

// sessionPlace is where a launched workspace lands, in the window host's
// vocabulary: the right three quarters of the workarea, full height.
const sessionPlace = "right top 75% 100%"

const defaultIn = "~/setup/dotfiles/kitty/.config/kitty/sessions"

// translated lists the keys this converter maps. Every other key present in
// the input is reported as untranslated rather than dropped in silence: a
// half-converted project is worse than a named omission.
var translated = map[string]bool{
	"KT_SESSION_PATH": true,
	"KT_WEB_URL":      true,
	"KT_IDEA_NAME":    true,
	"KT_EDITOR":       true,
}

type session struct {
	name string
	file string
	vars map[string]string
}

func main() {
	in := flag.String("in", defaultIn, "directory of .session files")
	out := flag.String("out", "", "directory to write project TOML into (required)")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "migrate-sessions: -out is required")
		os.Exit(2)
	}
	if err := convert(expandHome(*in), expandHome(*out)); err != nil {
		fmt.Fprintln(os.Stderr, "migrate-sessions:", err)
		os.Exit(1)
	}
}

func convert(in, out string) error {
	sessions, err := read(in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	written, skipped := 0, 0
	untranslated := map[string]int{}
	var missing []string

	for _, s := range sessions {
		if reason := skipReason(s); reason != "" {
			fmt.Fprintf(os.Stderr, "skip %-44s %s\n", s.name, reason)
			skipped++
			continue
		}
		// Only a converted session's keys count as untranslated. A skipped
		// session's extra keys are the reason it was skipped, and reporting
		// them twice would read as two different omissions.
		for k := range s.vars {
			if !translated[k] {
				untranslated[k]++
			}
		}
		body, err := render(s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.file, err)
		}
		if err := os.WriteFile(filepath.Join(out, s.name+".toml"), []byte(body), 0o644); err != nil {
			return err
		}
		written++
		if dir := expandHome(s.vars["KT_SESSION_PATH"]); dir != "" {
			if _, err := os.Stat(dir); err != nil {
				missing = append(missing, s.name)
			}
		}
	}

	// The real gate: every file revier will read has to load, validate and
	// prepare. A converter that writes ninety files revier rejects has done
	// nothing.
	projects, err := config.LoadProjects(out)
	if err != nil {
		return fmt.Errorf("the written projects do not load: %w", err)
	}

	fmt.Printf("input    %d\nwritten  %d\nskipped  %d\nloaded   %d\n",
		len(sessions), written, skipped, len(projects))
	if len(untranslated) > 0 {
		fmt.Printf("not translated (no field in a revier project): %s\n", counts(untranslated))
	}
	if len(missing) > 0 {
		fmt.Printf("%d projects point at a directory that does not exist here: %s\n",
			len(missing), strings.Join(missing, " "))
	}
	if written+skipped != len(sessions) {
		return fmt.Errorf("written+skipped = %d, input = %d", written+skipped, len(sessions))
	}
	return nil
}

// read parses every .session file in dir. The format is full-line comments and
// KEY=value, with the rest of the line taken as the value - trailing comments
// are part of the value, which is what the sessions README says the shell
// parser does.
func read(dir string) ([]session, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".session") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		s := session{
			name: strings.TrimSuffix(e.Name(), ".session"),
			file: filepath.Join(dir, e.Name()),
			vars: map[string]string{},
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok || !strings.HasPrefix(key, "KT_") {
				continue
			}
			s.vars[key] = strings.TrimSpace(value)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// skipReason names the transport that keeps a session out of revier, or "" when
// the session converts. Devcontainer and SSH sessions are out of scope (D6), so
// they are reported by name and no file is written for them.
func skipReason(s session) string {
	switch {
	case strings.HasPrefix(s.name, "rs-"):
		return "remote clone; SSH transport is out of scope (D6)"
	case s.vars["KT_SSH_HOST"] != "":
		return "KT_SSH_HOST=" + s.vars["KT_SSH_HOST"] + "; SSH transport is out of scope (D6)"
	case s.vars["KT_REMOTE_SESSION_PATH"] != "":
		return "KT_REMOTE_SESSION_PATH set; SSH transport is out of scope (D6)"
	case s.vars["KT_SESSION_MODE"] != "" && s.vars["KT_SESSION_MODE"] != "native":
		return "KT_SESSION_MODE=" + s.vars["KT_SESSION_MODE"] + "; only native sessions convert (D6)"
	case s.vars["KT_SESSION_IMAGE"] != "":
		return "KT_SESSION_IMAGE=" + s.vars["KT_SESSION_IMAGE"] + "; devcontainer sessions are out of scope (D6)"
	case s.vars["KT_SESSION_PATH"] == "":
		return "no KT_SESSION_PATH"
	}
	return ""
}

// render writes one project. The shape is the two files converted by hand -
// ~/.config/revier/projects/{revier,setup}.toml - because those are the ones
// proven against the real desktop.
func render(s session) (string, error) {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	p("# %s: converted from %s by scripts/migrate-sessions.\n", s.name, contractHome(s.file))
	p("name = %s\n", q(s.name))
	p("path = %s\n\n", q(s.vars["KT_SESSION_PATH"]))

	// The workspace: the agent beside a shell in one kitty OS window titled
	// session:<name>, which is the default kitty template's first tab.
	p("[[target]]\nname = \"home\"\nhome = true\nkey = \"ctrl-shift-u\"\n")
	p("  [target.runtime]\n")
	p("  name = \"session:{{.Name}}\"\n")
	p("  match = { title = %s }\n", q("^session:"+pattern(s.name)+"$"))
	// The geometry the shell tool gives every new session window
	// (os-fzf.sh:460). Only the workspace: an editor and a page keep whatever
	// position they had.
	p("  place = %s\n", q(sessionPlace))
	p("    [[target.runtime.panels]]\n")
	p("    kind = \"agent\"\n    title = \"Claude Code\"\n")
	p("    command = [\"sh\", \"-lc\", %s]\n", q(`"$HOME/setup/scripts/sessions/kt-new-agent.sh"`))
	p("    [[target.runtime.panels]]\n")
	p("    kind = \"shell\"\n    title = \"shell\"\n\n")

	// IntelliJ has no per-project window class, so the title carries the
	// identity. KT_IDEA_NAME overrides it where the project name on disk and
	// the name IntelliJ shows differ.
	//
	// The title is the project name, then whatever IntelliJ appends: " - <file>"
	// usually, " [<path>] - <file>" when it disambiguates. So the pattern ends
	// at a space or at the end of the title, and not at the dash the two
	// hand-converted files assumed - that form missed a real window on this
	// desktop. Project names carry no spaces, so a following space always
	// separates the name from IntelliJ's own text.
	editor := pattern(s.name)
	if idea := s.vars["KT_IDEA_NAME"]; idea != "" {
		editor = regexp.QuoteMeta(idea)
	}
	p("[[target]]\nname = \"editor\"\nkey = \"ctrl-shift-o\"\n")
	p("  [target.window]\n")
	p("  launch = [\"snap\", \"run\", \"intellij-idea\", \"{{.Path}}\"]\n")
	p("  match = { class = \"^jetbrains-idea\", title = %s }\n\n", q("^"+editor+"( |$)"))

	if raw := s.vars["KT_WEB_URL"]; raw != "" {
		class, err := chromeClass(raw)
		if err != nil {
			return "", err
		}
		p("# Chrome derives an app window's class from the address, so the match is\n")
		p("# that derived class rather than a --class Chrome would not honour.\n")
		p("[[target]]\nname = \"web\"\nkey = \"ctrl-shift-i\"\n")
		p("  [target.window]\n")
		p("  launch = [\"google-chrome\", %s]\n", q("--app="+raw))
		p("  match = { class = %s }\n\n", q("^"+regexp.QuoteMeta(class)+"$"))
	}

	// The ticket viewer is the template's second tab; revier gives it its own
	// kitty OS window, because a target is a window and not a tab. No key:
	// reach it from the picker.
	p("[[target]]\nname = \"tickets\"\n")
	p("  [target.runtime]\n")
	p("  name = \"tickets:{{.Name}}\"\n")
	p("  launch = [\"sh\", \"-lc\", %s]\n", q(`"$HOME/setup/scripts/sessions/kt-new-ticketviewer.sh"`))
	p("  match = { title = %s }\n", q("^tickets:"+pattern(s.name)+"$"))
	return b.String(), nil
}

// pattern is the project name as it appears inside a match regexp. Two session
// names contain dots, which a regexp would read as "any character", so those
// are escaped literally instead of going through the {{.Name}} template the
// other files use.
func pattern(name string) string {
	if quoted := regexp.QuoteMeta(name); quoted != name {
		return quoted
	}
	return "{{.Name}}"
}

// chromeClass derives the WM_CLASS Chrome gives an --app window: the host and
// the path with every "/" as "_", prefixed and suffixed by the profile. Only
// one session carries a KT_WEB_URL, and this reproduces the class already
// verified by hand for it; a URL with a query or a fragment is untested.
func chromeClass(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("KT_WEB_URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("KT_WEB_URL %q has no host", raw)
	}
	return "chrome-" + u.Host + "_" + strings.ReplaceAll(u.Path, "/", "_") + "-Default", nil
}

// q renders a Go string as a TOML basic string.
func q(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func counts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s (%d)", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// contractHome is expandHome's inverse, so a generated header names the file
// the way the user does.
func contractHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(p, home+"/") {
		return p
	}
	return "~" + strings.TrimPrefix(p, home)
}

func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
