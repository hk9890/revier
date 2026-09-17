package tui

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hk9890/revier/internal/checkout"
	"github.com/hk9890/revier/internal/config"
	"github.com/hk9890/revier/internal/core"
	"github.com/hk9890/revier/internal/theme"
	"github.com/hk9890/revier/pkg/revier"
)

// The new-project screen: one field, which takes a directory or a clone URL.
// The name is the directory's base name, as `revier new` derives it, so there
// is nothing else to ask. It writes the same file that command writes, and the
// project is a row the moment it lands.
//
// Under the field is a list. An empty field lists the folders the projects
// already live in; a directory lists the subdirectories that complete it; a
// clone URL lists those folders again, as where the clone goes.

// newPathInput is the directory field.
func newPathInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.Placeholder = "~/dev/example, or a git clone URL"
	in.CharLimit = 512
	return in
}

// openNew is the "new" button and alt+n. The cursor comes back to the list
// first, so the surface the screen stands over is the one it is left on.
func (m Model) openNew() (tea.Model, tea.Cmd) {
	m.err = nil
	m.path.SetValue("")
	m.syncNewRows()
	m.toList()
	m.dialog = dialogNew
	return m, m.path.Focus()
}

// newKey is every press on the new-project screen. Enter writes the project,
// Tab completes, up and down choose a row, Esc goes back, and everything else
// is the field's: it is the one place on the surface where a printable rune is
// text rather than a filter.
func (m Model) newKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		m.dialog = dialogNone
		m.path.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if url := strings.TrimSpace(m.path.Value()); isCloneURL(url) {
			return m.cloneProject(url)
		}
		return m.createProject()
	case key.Matches(msg, m.keys.Next):
		return m.completePath(), nil
	case key.Matches(msg, m.keys.Down):
		m.nrow = min(m.nrow+1, len(m.nrows)-1)
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.nrow > 0 {
			m.nrow--
		}
		return m, nil
	}
	if altRune(msg) {
		return m, nil
	}
	m.err = nil
	in, cmd := m.path.Update(msg)
	m.path = in
	m.syncNewRows()
	return m, cmd
}

// syncNewRows lists what the field's value offers. A clone URL always has a
// folder chosen, as Enter clones into it; a directory has none until one is
// picked, so Tab completes the common part first.
func (m *Model) syncNewRows() {
	typed := strings.TrimSpace(m.path.Value())
	m.nrow = -1
	switch {
	case typed == "":
		m.nrows = projectRoots(m.projects)
	case isCloneURL(typed):
		m.nrows = projectRoots(m.projects)
		m.nrow = 0
	default:
		m.nrows = subdirs(typed)
	}
}

// completePath is Tab: the chosen row, the only row, or the part every row
// shares. A row is a directory, so it goes in with the separator that lists
// what is inside it.
func (m Model) completePath() Model {
	typed := strings.TrimSpace(m.path.Value())
	if isCloneURL(typed) {
		return m
	}
	next := typed
	switch {
	case m.nrow >= 0:
		next = m.nrows[m.nrow] + "/"
	case len(m.nrows) == 1:
		next = m.nrows[0] + "/"
	case len(m.nrows) > 1:
		next = commonPrefix(m.nrows)
	}
	if next == typed || len(next) < len(typed) {
		return m
	}
	m.path.SetValue(next)
	m.path.CursorEnd()
	m.syncNewRows()
	return m
}

// createProject writes the project file for the directory in the field. A
// directory that is not there is refused: that is what a clone URL is for.
// The origin is recorded, as `revier new` records it, so the project can be
// cloned on the next machine.
func (m Model) createProject() (tea.Model, tea.Cmd) {
	m.err = nil
	dir := strings.TrimSpace(m.path.Value())
	if dir == "" {
		m.err = fmt.Errorf("give the directory of the project to add, or a clone URL")
		return m, nil
	}
	dir = config.ExpandHome(dir)
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			m.err = err
			return m, nil
		}
		dir = abs
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		m.err = fmt.Errorf("%s is not a directory here", contractHome(dir))
		return m, nil
	}
	if _, ok := m.addProject(dir, checkout.Origin(dir), true); ok {
		checkout.Trust(dir, io.Discard)
	}
	return m, nil
}

// cloneProject writes the project for a clone URL, into the chosen folder
// under the repository's own name, and clones it as Enter on a project with a
// missing directory does. A clone that fails leaves the project behind, and
// Enter on its row tries again.
func (m Model) cloneProject(url string) (tea.Model, tea.Cmd) {
	m.err = nil
	if err := config.ValidateGitURL(url); err != nil {
		m.err = fmt.Errorf("clone URL: %w", err)
		return m, nil
	}
	name := repoName(url)
	if err := config.ValidateName(revier.ProjectName(name)); err != nil {
		m.err = fmt.Errorf("clone URL %s: %w", url, err)
		return m, nil
	}
	dir := filepath.Join(config.ExpandHome(m.nrows[m.nrow]), name)
	if _, err := os.Stat(dir); err == nil {
		m.err = fmt.Errorf("%s is already there: add it by its directory", contractHome(dir))
		return m, nil
	}
	p, ok := m.addProject(dir, url, false)
	if !ok {
		return m, nil
	}
	home, _ := p.Home()
	return m, m.clone(p, home.Name)
}

// addProject writes the project file for dir and makes it the selected row.
// It reports false, with the reason in m.err, when nothing was written.
func (m *Model) addProject(dir, gitURL string, exists bool) (core.Project, bool) {
	name := config.NameFor(dir)
	if p, ok := m.project(name); ok {
		m.err = fmt.Errorf("project %q already exists: %s", name, contractHome(p.File))
		return core.Project{}, false
	}
	root, err := config.Root()
	if err != nil {
		m.err = err
		return core.Project{}, false
	}
	p, err := config.Create(root, name, dir, gitURL)
	if err != nil {
		m.err = err
		return core.Project{}, false
	}
	m.projects = append(m.projects, p)
	m.tkeys = targetKeys(m.projects, m.keys)
	// Provisional, until the survey answers: nothing of it is running, and
	// its directory is the one just checked.
	m.views = sorted(append(m.views, revier.ProjectView{Project: p.Project, PathExists: exists}))
	m.dialog = dialogNone
	m.path.Blur()
	m.reload()
	m.selectName(p.Name)
	return p, true
}

// projectRoots is the folders the local projects live in, the one holding
// the most first, with the home directory always among them. They are
// written as the field takes them, with the home directory as ~.
func projectRoots(projects []core.Project) []string {
	home, _ := os.UserHomeDir()
	count := map[string]int{"~": 0}
	for _, p := range projects {
		if p.Remote != nil || p.Path == "" {
			continue
		}
		parent := filepath.Dir(config.ExpandHome(p.Path))
		if parent == home {
			parent = "~"
		}
		count[contractHome(parent)]++
	}
	roots := make([]string, 0, len(count))
	for r := range count {
		roots = append(roots, r)
	}
	slices.SortFunc(roots, func(a, b string) int {
		return cmp.Or(cmp.Compare(count[b], count[a]), cmp.Compare(a, b))
	})
	return roots
}

// subdirs is the directories that complete typed: those in the directory it
// names up to its last separator, whose names start with the rest. A hidden
// directory is listed only once a dot is typed. They are written as typed,
// so ~ stays ~.
func subdirs(typed string) []string {
	if typed == "~" {
		return []string{"~"}
	}
	cut := strings.LastIndex(typed, "/") + 1
	dir, prefix := typed[:cut], typed[cut:]
	read := config.ExpandHome(dir)
	if read == "" {
		read = "."
	}
	entries, err := os.ReadDir(read)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		// os.DirEntry does not follow a link; a linked directory is one to
		// complete into all the same.
		if info, err := os.Stat(filepath.Join(read, name)); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, dir+name)
	}
	return out
}

func commonPrefix(rows []string) string {
	prefix := rows[0]
	for _, r := range rows[1:] {
		for !strings.HasPrefix(r, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// scpURL is git's scp-like form, user@host:path, as in
// git@github.com:owner/repo.git.
var scpURL = regexp.MustCompile(`^[\w.-]+@[\w.-]+:\S`)

// isCloneURL reports whether the field holds a URL to clone rather than a
// directory.
func isCloneURL(s string) bool {
	for _, scheme := range []string{"https://", "http://", "ssh://", "git://"} {
		if strings.HasPrefix(s, scheme) {
			return true
		}
	}
	return scpURL.MatchString(s)
}

// repoName is the directory git clone would make of url: its last path
// element, without .git.
func repoName(url string) string {
	s := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// newScreen is what stands in the list's place while the screen is up: what
// the field is for, what pressing Enter will write, and the rows the field
// offers. at is the line of the chosen row, for the body to keep in view.
func (m Model) newScreen() (text string, at int) {
	th := m.theme
	w := m.listWidth()
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	typed := strings.TrimSpace(m.path.Value())
	rows := m.nrows
	var lines []string
	switch {
	case typed == "":
		lines = []string{
			say(th.NameDim, "A directory on this machine, or a git clone URL."),
			say(th.Meta, "The project is named after the directory."),
		}
	case isCloneURL(typed):
		name := revier.ProjectName(repoName(typed))
		lines = []string{
			say(th.NameDim, "Enter clones into the folder chosen below, and writes"),
			m.newFileLine(say, name),
			say(th.Meta, m.newTargets(name)),
		}
		rows = make([]string, len(m.nrows))
		for i, r := range m.nrows {
			rows[i] = filepath.Join(r, string(name))
		}
	default:
		name := config.NameFor(config.ExpandHome(typed))
		lines = []string{
			say(th.NameDim, "Enter writes"),
			m.newFileLine(say, name),
			say(th.Meta, m.newTargets(name)),
		}
	}
	lines = append(lines, "")
	return strings.Join(append(lines, choiceRows(th, rows, m.nrow, w)), "\n"), len(lines) + max(m.nrow, 0)
}

// newFileLine is the project file Enter writes for name.
func (m Model) newFileLine(say func(lipgloss.Style, string) string, name revier.ProjectName) string {
	root, err := config.Root()
	if err != nil {
		return say(m.theme.Attention, err.Error())
	}
	return say(m.theme.Path, contractHome(config.ProjectFile(root, name)))
}

// newTargets says what the written project will run. With shared targets in
// config.toml the file declares no targets of its own, and the project gets
// the shared ones.
func (m Model) newTargets(name revier.ProjectName) string {
	if len(m.targets) == 0 {
		return fmt.Sprintf("an agent, a shell and an editor for %q", name)
	}
	names := make([]string, len(m.targets))
	for i, t := range m.targets {
		names[i] = string(t.Name)
	}
	return fmt.Sprintf("%q with the shared targets: %s", name, strings.Join(names, ", "))
}

// choiceRows draws rows to choose from, with the chosen one marked as the
// list marks its selection.
func choiceRows(th theme.Theme, rows []string, chosen, w int) string {
	var b strings.Builder
	for i, r := range rows {
		sel := i == chosen
		style := th.ProjectName
		if sel {
			style = th.OnSelection(style)
		}
		b.WriteString(fill(cursor(th, sel)+style.Render(clipTo(r, w-2)), w, func(st lipgloss.Style) lipgloss.Style {
			if sel {
				return th.OnSelection(st)
			}
			return st
		}) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}
