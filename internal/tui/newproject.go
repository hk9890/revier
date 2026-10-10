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

	"github.com/hk9890/revier/internal/app"
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

// createScreen is the new-project screen while it is up.
type createScreen struct {
	step newStep
	path textinput.Model // the directory field
	rows []string        // what the screen lists under the field
	row  int             // the chosen one of rows, -1 for none
	dir  string          // the folder the screen asks to create
}

func newCreateScreen(th theme.Theme) createScreen {
	return createScreen{path: newPathInput(th), row: -1}
}

// created is a project the screen wrote.
type created struct {
	project core.Project
	exists  bool // its directory is there
	clone   bool // its directory is to be cloned from the project's URL
}

// createResult is what a press on the screen leaves for the surface.
type createResult struct {
	err     error    // the footer's
	closed  bool     // Esc on the field: back to the surface
	created *created // the project the press wrote
}

// openCreate is the "new" button and alt+n. The cursor comes back to the
// list first, so the surface the screen stands over is the one it is left on.
func (m Model) openCreate() (tea.Model, tea.Cmd) {
	m.err = nil
	cmd := m.create.open()
	m.toList()
	m.dialog = dialogNew
	return m, cmd
}

// createKey is every press on the new-project screen, and what it left for
// the surface: the screen closed, or a project that is a row the moment it
// lands, and is cloned when its folder is to come from a repository.
func (m Model) createKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	res, cmd := m.create.key(m.surface(), msg)
	m.err = res.err
	if res.closed {
		m.dialog = dialogNone
	}
	if c := res.created; c != nil {
		// Provisional, until the survey answers: nothing of it is running.
		m.addProject(c.project, revier.ProjectView{Project: c.project.Project, PathExists: c.exists})
		if c.clone {
			home, _ := c.project.Home()
			cmd = m.clone(c.project, home.Name)
		}
	}
	return m, cmd
}

// addProject puts a project just written in as the selected row, at once, as the
// next survey will show it, rather than a refresh later. The screen that
// wrote it closes.
func (m *Model) addProject(p core.Project, view revier.ProjectView) {
	m.projects = append(m.projects, p)
	m.setKeys()
	m.views = m.sorted(append(m.views, view))
	m.dialog = dialogNone
	m.reload()
	m.selectName(p.Name)
}

// open clears the field for a new visit.
func (s *createScreen) open() tea.Cmd {
	s.path.SetValue("")
	s.step = newField
	s.syncRows()
	return s.path.Focus()
}

// key is every press on the screen. Esc goes back a step, and from the field
// closes the screen.
func (s *createScreen) key(sf surface, msg tea.KeyMsg) (createResult, tea.Cmd) {
	res := createResult{err: sf.err}
	if key.Matches(msg, sf.keys.Quit) {
		return res, tea.Quit
	}
	switch s.step {
	case newRoot:
		return s.rootKey(sf, msg)
	case newMkdir:
		return s.mkdirKey(sf, msg)
	}
	switch {
	case key.Matches(msg, sf.keys.Back):
		res.closed = true
		s.path.Blur()
		return res, nil
	case key.Matches(msg, sf.keys.Enter):
		return s.submitField(sf), nil
	case key.Matches(msg, sf.keys.Next):
		s.completePath()
		return res, nil
	case key.Matches(msg, sf.keys.Down):
		s.row = min(s.row+1, len(s.rows)-1)
		return res, nil
	case key.Matches(msg, sf.keys.Up):
		if s.row > 0 {
			s.row--
		}
		return res, nil
	}
	if altRune(msg) {
		return res, nil
	}
	res.err = nil
	in, cmd := s.path.Update(msg)
	s.path = in
	s.syncRows()
	return res, cmd
}

// rootKey is the step that chooses the folder a name or a clone goes in.
func (s *createScreen) rootKey(sf surface, msg tea.KeyMsg) (createResult, tea.Cmd) {
	res := createResult{err: sf.err}
	switch {
	case key.Matches(msg, sf.keys.Back):
		res.err = nil
		s.step = newField
		s.syncRows()
		return res, s.path.Focus()
	case key.Matches(msg, sf.keys.Enter):
		return s.addOrAsk(sf, s.rootTarget(s.row)), nil
	case key.Matches(msg, sf.keys.Down):
		s.row = min(s.row+1, len(s.rows)-1)
	case key.Matches(msg, sf.keys.Up):
		s.row = max(s.row-1, 0)
	}
	return res, nil
}

// mkdirKey is the question whether to create the folder. Esc goes back to
// where the folder was given.
func (s *createScreen) mkdirKey(sf surface, msg tea.KeyMsg) (createResult, tea.Cmd) {
	switch {
	case key.Matches(msg, sf.keys.Back):
		if isFullPath(s.typed()) {
			s.step = newField
			return createResult{}, s.path.Focus()
		}
		s.step = newRoot
		return createResult{}, nil
	case key.Matches(msg, sf.keys.Enter):
		// A folder that appeared since the question is added as one that was
		// there, so a clone URL it does not come from is named, not dropped.
		if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
			return s.addOrAsk(sf, s.dir), nil
		}
		if url := s.typed(); isCloneURL(url) {
			return s.cloneInto(sf, s.dir, url), nil
		}
		return s.mkdirProject(sf, s.dir), nil
	}
	return createResult{err: sf.err}, nil
}

// mkdirProject writes the project for dir, then creates dir. The file comes
// first, so a file that cannot be written leaves no folder behind.
func (s *createScreen) mkdirProject(sf surface, dir string) createResult {
	p, err := s.write(sf, dir, "")
	if err != nil {
		return createResult{err: err}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return createResult{created: &created{project: p}, err: fmt.Errorf("project %q is written, but its folder is not: %w", p.Name, err)}
	}
	return createResult{created: &created{project: p, exists: true}}
}

// cloneInto writes the project for url at dir, which is not there yet, to be
// cloned; git creates the folder.
func (s *createScreen) cloneInto(sf surface, dir, url string) createResult {
	p, err := s.write(sf, dir, url)
	if err != nil {
		return createResult{err: err}
	}
	return createResult{created: &created{project: p, clone: true}}
}

func (s *createScreen) typed() string { return strings.TrimSpace(s.path.Value()) }

// fieldPath is the full path Enter adds: the row chosen under the field, or
// the field itself.
func (s *createScreen) fieldPath() string {
	if s.row >= 0 {
		return s.rows[s.row]
	}
	return s.typed()
}

// syncRows lists the subdirectories that complete a full path. A name lists
// nothing until Enter asks where it goes.
func (s *createScreen) syncRows() {
	s.row = -1
	s.rows = nil
	if typed := s.typed(); isFullPath(typed) {
		s.rows = subdirs(typed)
	}
}

// completePath is Tab on a full path: the chosen row, the only row, or the
// part every row shares. A row is a directory, so it goes in with the
// separator that lists what is inside it.
func (s *createScreen) completePath() {
	typed := s.typed()
	next := typed
	switch {
	case s.row >= 0:
		next = s.rows[s.row] + "/"
	case len(s.rows) == 1:
		next = s.rows[0] + "/"
	case len(s.rows) > 1:
		next = commonPrefix(s.rows)
	}
	if len(next) <= len(typed) {
		return
	}
	s.path.SetValue(next)
	s.path.CursorEnd()
	s.syncRows()
}

// submitField is Enter in the field: a full path is added, or asked about;
// a name or a clone URL goes on to choose its folder.
func (s *createScreen) submitField(sf surface) createResult {
	typed := s.typed()
	switch {
	case typed == "":
		return createResult{err: fmt.Errorf("give a full path, a name, or a clone URL")}
	case isFullPath(typed):
		typed = s.fieldPath()
		dir := config.ExpandHome(typed)
		if !filepath.IsAbs(dir) {
			return createResult{err: fmt.Errorf("%s is not a full path", typed)}
		}
		return s.addOrAsk(sf, filepath.Clean(dir))
	case isCloneURL(typed):
		if err := config.ValidateGitURL(typed); err != nil {
			return createResult{err: fmt.Errorf("clone URL: %w", err)}
		}
	}
	name := s.newName()
	if err := config.ValidateName(revier.ProjectName(name)); err != nil {
		return createResult{err: err}
	}
	if err := nameFree(sf.projects, config.NameFor(name)); err != nil {
		return createResult{err: err}
	}
	s.step = newRoot
	s.rows = projectRoots(sf.projects)
	s.row = 0
	s.path.Blur()
	return createResult{}
}

// addOrAsk adds dir when it is a folder, and asks to create it, or to clone
// into it, when it is not there.
func (s *createScreen) addOrAsk(sf surface, dir string) createResult {
	if err := config.CanCreate(sf.projects, config.NameFor(dir), dir); err != nil {
		return createResult{err: err}
	}
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		s.dir = dir
		s.step = newMkdir
		s.path.Blur()
	case err != nil:
		return createResult{err: err}
	case !info.IsDir():
		return createResult{err: fmt.Errorf("%s is not a folder", config.ContractHome(dir))}
	default:
		return s.addFolder(sf, dir)
	}
	return createResult{}
}

// addFolder writes the project for a folder that is there. Its origin is
// recorded, as `revier new` records it, so the project can be cloned on the
// next machine. A clone URL that is not that origin is not recorded, and the
// footer says so.
func (s *createScreen) addFolder(sf surface, dir string) createResult {
	p, err := s.write(sf, dir, "")
	if err != nil {
		return createResult{err: err}
	}
	res := createResult{created: &created{project: p, exists: true}}
	if url := s.typed(); isCloneURL(url) && sameRepo(url) != sameRepo(p.GitURL) {
		res.err = fmt.Errorf("%s was already there and its origin is not %s: the URL is ignored", config.ContractHome(dir), url)
	}
	return res
}

// sameRepo is url without what two spellings of one repository differ by: a
// trailing separator and the .git suffix.
func sameRepo(url string) string {
	return strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
}

func nameFree(projects []core.Project, name revier.ProjectName) error {
	if p, ok := projectNamed(projects, name); ok {
		return fmt.Errorf("project %q already exists: %s", name, config.ContractHome(p.File))
	}
	return nil
}

// write writes the project file for dir, as app.CreateProject writes it.
// gitURL is the repository a folder that is not there is cloned from.
func (s *createScreen) write(sf surface, dir, gitURL string) (core.Project, error) {
	name := config.NameFor(dir)
	if err := nameFree(sf.projects, name); err != nil {
		return core.Project{}, err
	}
	root, err := config.Root()
	if err != nil {
		return core.Project{}, err
	}
	p, err := app.CreateProject(root, sf.projects, name, dir, gitURL, io.Discard)
	if err != nil {
		return core.Project{}, err
	}
	s.path.Blur()
	return p, nil
}

// newName is the folder name a name or a clone URL puts in the chosen folder.
func (s *createScreen) newName() string {
	if typed := s.typed(); isCloneURL(typed) {
		return repoName(typed)
	}
	return s.typed()
}

// rootTarget is the folder the name goes to in the i-th listed folder.
func (s *createScreen) rootTarget(i int) string {
	return filepath.Join(config.ExpandHome(s.rows[i]), s.newName())
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
		count[config.ContractHome(parent)]++
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

// help is the footer of the step the screen is at.
func (s *createScreen) help(k keyMap) []key.Binding {
	back := helpKey("esc", "back")
	switch {
	case s.step == newMkdir && isCloneURL(s.typed()):
		return []key.Binding{helpKey("enter", "clone"), back, k.Quit}
	case s.step == newMkdir:
		return []key.Binding{helpKey("enter", "create the folder"), back, k.Quit}
	case s.step == newRoot:
		return []key.Binding{helpKey("enter", "add the project"), helpKey("↑↓", "choose"), back, k.Quit}
	case isFullPath(s.typed()):
		return []key.Binding{helpKey("enter", "add the project"), helpKey("tab", "complete"), helpKey("↑↓", "choose"), back, k.Quit}
	}
	return []key.Binding{helpKey("enter", "choose its folder"), back, k.Quit}
}

// screen is what stands in the list's place while the screen is up: what the
// step is for, what Enter will write, and the rows to choose from. at is the
// line of the chosen row, for the body to keep in view.
func (s *createScreen) screen(sf surface) (text string, at int) {
	th, w := sf.theme, sf.list
	say := func(st lipgloss.Style, text string) string {
		return st.PaddingLeft(2).Width(w).Render(clipTo(text, w-2))
	}
	file := func(name revier.ProjectName) string {
		root, err := config.Root()
		if err != nil {
			return say(th.Attention, err.Error())
		}
		return say(th.Path, config.ContractHome(config.ProjectFile(root, name)))
	}
	typed := s.typed()
	rows := s.rows
	var lines []string
	switch {
	case s.step == newMkdir:
		rows = nil
		verb := "Enter creates it and writes"
		if isCloneURL(typed) {
			verb = "Enter clones " + typed + " into it and writes"
		}
		lines = []string{
			say(th.Attention, "This folder is not there:"),
			say(th.Path, config.ContractHome(s.dir)),
			say(th.NameDim, verb),
			file(config.NameFor(s.dir)),
		}
	case s.step == newRoot:
		verb := "Enter puts " + typed + " in the folder chosen below, and writes"
		if isCloneURL(typed) {
			verb = "Enter clones " + typed + " into the folder chosen below, and writes"
		}
		name := config.NameFor(s.newName())
		lines = []string{say(th.NameDim, verb), file(name), say(th.Meta, newTargets(sf, name))}
		rows = make([]string, len(s.rows))
		for i := range s.rows {
			dir := s.rootTarget(i)
			rows[i] = config.ContractHome(dir)
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
		name := config.NameFor(config.ExpandHome(s.fieldPath()))
		lines = []string{say(th.NameDim, "Enter writes"), file(name), say(th.Meta, newTargets(sf, name))}
	default:
		verb := "Enter chooses the folder for " + s.newName()
		if isCloneURL(typed) {
			verb = "Enter chooses the folder to clone " + s.newName() + " into"
		}
		name := config.NameFor(s.newName())
		lines = []string{say(th.NameDim, verb), file(name), say(th.Meta, newTargets(sf, name))}
	}
	lines = append(lines, "")
	return strings.Join(append(lines, choiceRows(th, rows, s.row, w)), "\n"), len(lines) + max(s.row, 0)
}

// newTargets says what the written project will run. With shared targets in
// config.toml the project gets the shared ones, and the file declares only
// a home of its own, when no shared target is home.
func newTargets(sf surface, name revier.ProjectName) string {
	if len(sf.targets) == 0 {
		return fmt.Sprintf("an agent, a shell and an editor for %q", name)
	}
	names := make([]string, len(sf.targets))
	for i, t := range sf.targets {
		names[i] = string(t.Name)
	}
	with := "the shared targets"
	if !config.SharedHome(sf.usable) {
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
