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
	"unicode/utf8"

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

// The new-project screen. The field takes one of three things:
//
//   - a full path, starting with / or ~, which Tab completes: Enter adds that
//     folder;
//   - a name: Enter asks which of the folders the projects live in it goes
//     in, and adds it there;
//   - a git clone URL: as a name, with the repository's name, and the
//     project is cloned into its folder.
//
// A folder that is not there is created after asking; a clone creates its
// own. The project name is the folder's base name, as `revier new` derives
// it. It writes the same file that command writes, and the project is a row
// the moment it lands.

type newStep int

const (
	newField newStep = iota // typing
	newRoot                 // choosing the folder a name goes in
	newMkdir                // asking to create a folder that is not there
)

// newPathInput is the directory field.
func newPathInput(th theme.Theme) textinput.Model {
	in := textinput.New()
	in.Prompt = promptMark
	styleField(&in, th)
	in.Placeholder = "~/dev/example, a name, or a git clone URL"
	in.CharLimit = 512
	return in
}

// openNew is the "new" button and alt+n. The cursor comes back to the list
// first, so the surface the screen stands over is the one it is left on.
func (m Model) openNew() (tea.Model, tea.Cmd) {
	m.err = nil
	m.path.SetValue("")
	m.nstep = newField
	m.syncNewRows()
	m.toList()
	m.dialog = dialogNew
	return m, m.path.Focus()
}

// newKey is every press on the new-project screen. Esc goes back a step, and
// from the field closes the screen.
func (m Model) newKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}
	switch m.nstep {
	case newRoot:
		return m.rootKey(msg)
	case newMkdir:
		return m.mkdirKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		m.dialog = dialogNone
		m.path.Blur()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		return m.submitField()
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

// rootKey is the step that chooses the folder a name or a clone goes in.
func (m Model) rootKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		m.nstep = newField
		m.syncNewRows()
		return m, m.path.Focus()
	case key.Matches(msg, m.keys.Enter):
		return m.submitRoot()
	case key.Matches(msg, m.keys.Down):
		m.nrow = min(m.nrow+1, len(m.nrows)-1)
	case key.Matches(msg, m.keys.Up):
		m.nrow = max(m.nrow-1, 0)
	}
	return m, nil
}

// mkdirKey is the question whether to create the folder. Esc goes back to
// where the folder was given.
func (m Model) mkdirKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.err = nil
		if isFullPath(m.typed()) {
			m.nstep = newField
			return m, m.path.Focus()
		}
		m.nstep = newRoot
	case key.Matches(msg, m.keys.Enter):
		m.err = nil
		if url := m.typed(); isCloneURL(url) {
			return m.cloneInto(m.ndir, url)
		}
		m.mkdirProject(m.ndir)
	}
	return m, nil
}

// mkdirProject writes the project for dir, then creates dir. The file comes
// first, so a file that cannot be written leaves no folder behind.
func (m *Model) mkdirProject(dir string) {
	p, ok := m.addProject(dir, "", false)
	if !ok {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.err = fmt.Errorf("project %q is written, but its folder is not: %w", p.Name, err)
		return
	}
	for i := range m.views {
		if m.views[i].Project.Name == p.Name {
			m.views[i].PathExists = true
		}
	}
	m.reload()
}

// cloneInto writes the project for url at dir, which is not there yet, and
// clones it; git creates the folder.
func (m Model) cloneInto(dir, url string) (tea.Model, tea.Cmd) {
	p, ok := m.addProject(dir, url, false)
	if !ok {
		return m, nil
	}
	home, _ := p.Home()
	return m, m.clone(p, home.Name)
}

func (m Model) typed() string { return strings.TrimSpace(m.path.Value()) }

// fieldPath is the full path Enter adds: the row chosen under the field, or
// the field itself.
func (m Model) fieldPath() string {
	if m.nrow >= 0 {
		return m.nrows[m.nrow]
	}
	return m.typed()
}

// syncNewRows lists the subdirectories that complete a full path. A name
// lists nothing until Enter asks where it goes.
func (m *Model) syncNewRows() {
	m.nrow = -1
	m.nrows = nil
	if typed := m.typed(); isFullPath(typed) {
		m.nrows = subdirs(typed)
	}
}

// completePath is Tab on a full path: the chosen row, the only row, or the
// part every row shares. A row is a directory, so it goes in with the
// separator that lists what is inside it.
func (m Model) completePath() Model {
	typed := m.typed()
	next := typed
	switch {
	case m.nrow >= 0:
		next = m.nrows[m.nrow] + "/"
	case len(m.nrows) == 1:
		next = m.nrows[0] + "/"
	case len(m.nrows) > 1:
		next = commonPrefix(m.nrows)
	}
	if len(next) <= len(typed) {
		return m
	}
	m.path.SetValue(next)
	m.path.CursorEnd()
	m.syncNewRows()
	return m
}

// submitField is Enter in the field: a full path is added, or asked about;
// a name or a clone URL goes on to choose its folder.
func (m Model) submitField() (tea.Model, tea.Cmd) {
	m.err = nil
	typed := m.typed()
	switch {
	case typed == "":
		m.err = fmt.Errorf("give a full path, a name, or a clone URL")
		return m, nil
	case isFullPath(typed):
		typed = m.fieldPath()
		dir := config.ExpandHome(typed)
		if !filepath.IsAbs(dir) {
			m.err = fmt.Errorf("%s is not a full path", typed)
			return m, nil
		}
		return m.addOrAsk(filepath.Clean(dir))
	case isCloneURL(typed):
		if err := config.ValidateGitURL(typed); err != nil {
			m.err = fmt.Errorf("clone URL: %w", err)
			return m, nil
		}
	}
	name := m.newName()
	if err := config.ValidateName(revier.ProjectName(name)); err != nil {
		m.err = err
		return m, nil
	}
	if err := m.nameFree(config.NameFor(name)); err != nil {
		m.err = err
		return m, nil
	}
	m.nstep = newRoot
	m.nrows = projectRoots(m.projects)
	m.nrow = 0
	m.path.Blur()
	return m, nil
}

// submitRoot is Enter on a folder: the name or the clone goes in it.
func (m Model) submitRoot() (tea.Model, tea.Cmd) {
	m.err = nil
	return m.addOrAsk(m.rootTarget(m.nrow))
}

// addOrAsk adds dir when it is a folder, and asks to create it, or to clone
// into it, when it is not there.
func (m Model) addOrAsk(dir string) (tea.Model, tea.Cmd) {
	if err := config.CanCreate(m.projects, config.NameFor(dir), dir); err != nil {
		m.err = err
		return m, nil
	}
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		m.ndir = dir
		m.nstep = newMkdir
		m.path.Blur()
	case err != nil:
		m.err = err
	case !info.IsDir():
		m.err = fmt.Errorf("%s is not a folder", contractHome(dir))
	default:
		m.addFolder(dir)
	}
	return m, nil
}

// addFolder writes the project for a folder that is there. Its origin is
// recorded, as `revier new` records it, so the project can be cloned on the
// next machine. A clone URL that is not that origin is not recorded, and the
// footer says so.
func (m *Model) addFolder(dir string) {
	origin := checkout.Origin(dir)
	if _, ok := m.addProject(dir, origin, true); !ok {
		return
	}
	checkout.Trust(dir, io.Discard)
	if url := m.typed(); isCloneURL(url) && sameRepo(url) != sameRepo(origin) {
		m.err = fmt.Errorf("%s was already there and its origin is not %s: the URL is ignored", contractHome(dir), url)
	}
}

// sameRepo is url without what two spellings of one repository differ by: a
// trailing separator and the .git suffix.
func sameRepo(url string) string {
	return strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
}

func (m Model) nameFree(name revier.ProjectName) error {
	if p, ok := m.project(name); ok {
		return fmt.Errorf("project %q already exists: %s", name, contractHome(p.File))
	}
	return nil
}

// addProject writes the project file for dir and makes it the selected row.
// It reports false, with the reason in m.err, when nothing was written.
func (m *Model) addProject(dir, gitURL string, exists bool) (core.Project, bool) {
	name := config.NameFor(dir)
	if err := m.nameFree(name); err != nil {
		m.err = err
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

// newName is the folder name a name or a clone URL puts in the chosen folder.
func (m Model) newName() string {
	if typed := m.typed(); isCloneURL(typed) {
		return repoName(typed)
	}
	return m.typed()
}

// rootTarget is the folder the name goes to in the i-th listed folder.
func (m Model) rootTarget(i int) string {
	return filepath.Join(config.ExpandHome(m.nrows[i]), m.newName())
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
		if !filepath.IsAbs(parent) {
			continue
		}
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
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(read, name))
			isDir = err == nil && info.IsDir()
		}
		if !isDir {
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
			_, size := utf8.DecodeLastRuneInString(prefix)
			prefix = prefix[:len(prefix)-size]
		}
	}
	return prefix
}

func isFullPath(s string) bool { return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") }

// scpURL is git's scp-like form, user@host:path, as in
// git@github.com:owner/repo.git.
var scpURL = regexp.MustCompile(`^[\w.-]+@[\w.-]+:\S`)

// isCloneURL reports whether the field holds a URL to clone rather than a
// name.
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

// newHelp is the footer of the step the screen is at.
func (m Model) newHelp() []key.Binding {
	back := helpKey("esc", "back")
	switch {
	case m.nstep == newMkdir && isCloneURL(m.typed()):
		return []key.Binding{helpKey("enter", "clone"), back, m.keys.Quit}
	case m.nstep == newMkdir:
		return []key.Binding{helpKey("enter", "create the folder"), back, m.keys.Quit}
	case m.nstep == newRoot:
		return []key.Binding{helpKey("enter", "add the project"), helpKey("↑↓", "choose"), back, m.keys.Quit}
	case isFullPath(m.typed()):
		return []key.Binding{helpKey("enter", "add the project"), helpKey("tab", "complete"), helpKey("↑↓", "choose"), back, m.keys.Quit}
	}
	return []key.Binding{helpKey("enter", "choose its folder"), back, m.keys.Quit}
}

// newScreen is what stands in the list's place while the screen is up: what
// the step is for, what Enter will write, and the rows to choose from. at is
// the line of the chosen row, for the body to keep in view.
func (m Model) newScreen() (text string, at int) {
	th := m.theme
	w := m.listWidth()
	say := func(s lipgloss.Style, text string) string {
		return s.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	typed := m.typed()
	rows := m.nrows
	var lines []string
	switch {
	case m.nstep == newMkdir:
		rows = nil
		verb := "Enter creates it and writes"
		if isCloneURL(typed) {
			verb = "Enter clones " + typed + " into it and writes"
		}
		lines = []string{
			say(th.Attention, "This folder is not there:"),
			say(th.Path, contractHome(m.ndir)),
			say(th.NameDim, verb),
			m.newFileLine(say, config.NameFor(m.ndir)),
		}
	case m.nstep == newRoot:
		verb := "Enter puts " + typed + " in the folder chosen below, and writes"
		if isCloneURL(typed) {
			verb = "Enter clones " + typed + " into the folder chosen below, and writes"
		}
		name := config.NameFor(m.newName())
		lines = []string{say(th.NameDim, verb), m.newFileLine(say, name), say(th.Meta, m.newTargets(name))}
		rows = make([]string, len(m.nrows))
		for i := range m.nrows {
			dir := m.rootTarget(i)
			rows[i] = contractHome(dir)
			if _, err := os.Stat(dir); err == nil {
				rows[i] += "  (already there)"
			}
		}
	case typed == "":
		lines = []string{
			say(th.NameDim, "A full path, starting with / or ~, adds that folder."),
			say(th.NameDim, "A name or a git clone URL: Enter then asks which folder it goes in."),
			say(th.Meta, "A folder that is not there is created after asking."),
		}
	case isFullPath(typed):
		name := config.NameFor(config.ExpandHome(m.fieldPath()))
		lines = []string{say(th.NameDim, "Enter writes"), m.newFileLine(say, name), say(th.Meta, m.newTargets(name))}
	default:
		verb := "Enter chooses the folder for " + m.newName()
		if isCloneURL(typed) {
			verb = "Enter chooses the folder to clone " + m.newName() + " into"
		}
		name := config.NameFor(m.newName())
		lines = []string{say(th.NameDim, verb), m.newFileLine(say, name), say(th.Meta, m.newTargets(name))}
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
// config.toml the project gets the shared ones, and the file declares only
// a home of its own, when no shared target is home.
func (m Model) newTargets(name revier.ProjectName) string {
	if len(m.targets) == 0 {
		return fmt.Sprintf("an agent, a shell and an editor for %q", name)
	}
	names := make([]string, len(m.targets))
	for i, t := range m.targets {
		names[i] = string(t.Name)
	}
	with := "the shared targets"
	if !config.SharedHome(config.Usable(m.shared)) {
		with = "its own home and shared targets"
	}
	return fmt.Sprintf("%q with %s: %s", name, with, strings.Join(names, ", "))
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
