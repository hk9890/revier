package herdr

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hk9890/revier/internal/adapter/proc"
	"github.com/hk9890/revier/internal/panelstart"
	"github.com/hk9890/revier/pkg/revier"
)

// minVersion is the oldest herdr this host was tested against. herdr's JSON
// API carries no version of its own, so the release is what is checked.
const minVersion = "0.9.5"

// maxPanes is the most panes one layout.apply builds.
const maxPanes = 24

// metadataSource is the source revier reports its tokens under.
const metadataSource = "revier"

// openVar is the variable Open starts a new workspace's first shell with,
// set to a value no other Open uses, so that a workspace whose create was
// not answered can still be told apart.
const openVar = "REVIER_HERDR_OPEN"

// Open creates a workspace for the realization, starting the server when none
// runs yet. The workspace is labelled r.Name and carries it as its name
// token, which Instances reads as the title. Its one tab runs the panels side
// by side, or r.Launch alone, each with no shell around it; r.Vars go on
// every pane as tokens, which Instances reports back as the panel's Vars.
// The workspace opens without the focus; the core focuses what it wants.
//
// A workspace that fails is closed whole, as tmux kills a session that fails:
// left open, it would be matched, or hold its name against the next Open.
func (h *Host) Open(ctx context.Context, r revier.Realization) (revier.TargetRef, error) {
	if r.Name == "" {
		return revier.TargetRef{}, fmt.Errorf("herdr: realization has no name to give the workspace")
	}
	panels := r.PanelSpecs()
	if err := h.buildable(ctx, panels); err != nil {
		return revier.TargetRef{}, err
	}
	if err := h.running(ctx); err != nil {
		return revier.TargetRef{}, err
	}

	var created struct {
		Workspace struct {
			ID string `json:"workspace_id"`
		} `json:"workspace"`
		Tab struct {
			ID string `json:"tab_id"`
		} `json:"tab"`
	}
	mark := rand.Text()
	params := map[string]any{"label": r.Name, "focus": false, "env": map[string]string{openVar: mark}}
	if r.Dir != "" {
		params["cwd"] = r.Dir
	}
	pid, err := h.request(ctx, "workspace.create", params, &created)
	if err != nil {
		// herdr may have made the workspace and its answer been lost, the end
		// of ctx while it was read: the next press would raise it with
		// herdr's default shell alone in it.
		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		h.closeMarked(closing, mark)
		return revier.TargetRef{}, err
	}
	workspace := created.Workspace.ID
	if err := h.fill(ctx, workspace, created.Tab.ID, r, panels); err != nil {
		// Best effort: the error that failed the workspace is the one worth
		// reporting, and a workspace whose panes all exited is gone already.
		// The close has a bound of its own: the end of ctx may be what failed
		// the workspace.
		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = h.request(closing, "workspace.close", map[string]string{"workspace_id": workspace}, nil)
		return revier.TargetRef{}, err
	}
	return revier.TargetRef{Host: h.Name(), ID: refID(pid, workspace), Title: r.Name}, nil
}

// closeMarked closes the workspace whose first shell carries mark in openVar:
// the one a workspace.create made whose answer was lost. The label does not
// tell it apart: another Open, such as a keypress beside the TUI, can make a
// workspace with the same label meanwhile, and the user can have made one.
// The mark is this Open's alone. A shell not yet started, or ended already,
// leaves the workspace open: a stray workspace costs less than closing
// another process's live one.
func (h *Host) closeMarked(ctx context.Context, mark string) {
	scanned, err := proc.Scan(h.root(), openVar, "HERDR_PANE_ID")
	if err != nil {
		return
	}
	var marked string
	for _, vars := range scanned {
		if vars[openVar] == mark {
			marked = vars["HERDR_PANE_ID"]
		}
	}
	if marked == "" {
		return
	}
	_, snap, err := h.snapshot(ctx)
	if err != nil {
		return
	}
	for _, p := range snap.Panes {
		if p.ID == marked {
			_, _ = h.request(ctx, "workspace.close", map[string]string{"workspace_id": p.WorkspaceID}, nil)
			return
		}
	}
}

// fill names a new workspace and replaces its first tab, the shell herdr
// starts it with, by the realization's panels, then checks that every one of
// them still runs.
func (h *Host) fill(ctx context.Context, workspace, tab string, r revier.Realization, panels []revier.PanelSpec) error {
	if survives(r.Name) {
		if _, err := h.request(ctx, "workspace.report_metadata", map[string]any{
			"workspace_id": workspace, "source": metadataSource, "tokens": map[string]string{nameToken: r.Name},
		}, nil); err != nil {
			return err
		}
	}

	_, panes, err := h.apply(ctx, "tab_id", tab, panels)
	if err != nil {
		return err
	}

	tokens := h.keepable(r.Name, r.Vars)
	for _, id := range panes {
		if err := h.mark(ctx, id, tokens); err != nil {
			// The mark is best effort, as it is on tmux: an unmarked panel
			// falls back to the guess it replaces.
			slog.Warn("panel mark", "host", h.Name(), "name", r.Name, "panel", id, "err", err)
		}
	}
	return nil
}

// keepable is the vars herdr keeps as written, as tokens. herdr trims a
// token, takes out its control characters and cuts it at 80 characters, and a
// var changed on the way would never match again, so one that would not
// survive is left off.
func (h *Host) keepable(name string, vars map[string]string) map[string]string {
	tokens := map[string]string{}
	for k, v := range vars {
		if !validKey(k) || !survives(v) {
			slog.Warn("panel var herdr cannot keep", "host", h.Name(), "name", name, "var", k)
			continue
		}
		tokens[k] = v
	}
	return tokens
}

// mark leaves tokens on a pane.
func (h *Host) mark(ctx context.Context, pane string, tokens map[string]string) error {
	if len(tokens) == 0 {
		return nil
	}
	_, err := h.request(ctx, "pane.report_metadata", map[string]any{"pane_id": pane, "source": metadataSource, "tokens": tokens}, nil)
	return err
}

// survives reports whether herdr keeps a token value as written.
func survives(v string) bool {
	return v != "" && v == strings.TrimSpace(v) && utf8.RuneCountInString(v) <= 80 &&
		!strings.ContainsFunc(v, unicode.IsControl)
}

// validKey reports whether herdr accepts a token name.
func validKey(k string) bool {
	return k != "" && len(k) <= 32 && !strings.ContainsFunc(k, func(r rune) bool {
		return r >= 128 || !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})
}

// layoutNode is a layout as herdr describes and takes it: a pane, or a split
// of two nodes.
type layoutNode struct {
	Type      string      `json:"type"`
	PaneID    string      `json:"pane_id,omitempty"`
	Label     string      `json:"label,omitempty"`
	Cwd       string      `json:"cwd,omitempty"`
	Command   []string    `json:"command,omitempty"`
	Direction string      `json:"direction,omitempty"`
	Ratio     float64     `json:"ratio,omitempty"`
	First     *layoutNode `json:"first,omitempty"`
	Second    *layoutNode `json:"second,omitempty"`
}

// layout puts the panels side by side in equal widths: each split halves the
// panels and gives its first half its share of the width. Halves, not one
// panel per split: herdr refuses a layout more than 16 levels deep, and a
// split per panel would be 24 levels deep for 24 panels. A panel with no
// command runs the shell herdr starts.
func layout(panels []revier.PanelSpec) layoutNode {
	if len(panels) == 1 {
		p := panels[0]
		return layoutNode{Type: "pane", Label: p.Title, Cwd: p.Dir, Command: p.Command}
	}
	half := len(panels) / 2
	first, second := layout(panels[:half]), layout(panels[half:])
	return layoutNode{
		Type: "split", Direction: "right", Ratio: float64(half) / float64(len(panels)),
		First: &first, Second: &second,
	}
}

// panes lists the pane ids of a layout herdr answered with, left to right.
func (n layoutNode) panes() []string {
	if n.Type == "pane" {
		return []string{n.PaneID}
	}
	var out []string
	for _, child := range []*layoutNode{n.First, n.Second} {
		if child != nil {
			out = append(out, child.panes()...)
		}
	}
	return out
}

// buildable refuses panels that one tab cannot hold, or whose program is not
// on the PATH of the server that starts them.
func (h *Host) buildable(ctx context.Context, panels []revier.PanelSpec) error {
	if len(panels) > maxPanes {
		return fmt.Errorf("herdr: a tab holds at most %d panels, and this one has %d", maxPanes, len(panels))
	}
	path, err := h.serverPATH(ctx)
	if err != nil {
		return err
	}
	return panelstart.StartableOn(panels, path)
}

// serverPATH is the PATH herdr starts a pane's program with: its server's,
// since a layout carries no environment. With no server running, or one whose
// environment cannot be read or holds no PATH, it is this process's, the one
// a server Open starts is given. A server that fails the ping is an error: a
// PATH guessed in its place would name a program as missing that the server
// may hold, instead of the server's own failure.
func (h *Host) serverPATH(ctx context.Context) (string, error) {
	pid, err := h.request(ctx, "ping", struct{}{}, nil)
	if errors.Is(err, errNoServer) {
		return os.Getenv("PATH"), nil
	}
	if err != nil {
		return "", err
	}
	if env, err := os.ReadFile(filepath.Join(h.root(), strconv.Itoa(pid), "environ")); err == nil {
		for _, kv := range strings.Split(string(env), "\x00") {
			if path, ok := strings.CutPrefix(kv, "PATH="); ok {
				return path, nil
			}
		}
	}
	return os.Getenv("PATH"), nil
}

// running makes sure a server answers, starting one when none does, and
// refuses one older than this host was tested against.
func (h *Host) running(ctx context.Context) error {
	version, err := h.ping(ctx)
	if errors.Is(err, errNoServer) {
		if err := h.start(); err != nil {
			return err
		}
		version, err = h.waitPing(ctx)
	}
	if err != nil {
		return err
	}
	return supported(version)
}

// supported refuses a herdr older than this host was tested against.
func supported(version string) error {
	if older(version, minVersion) {
		return fmt.Errorf("herdr %s is older than %s, the oldest revier works with", version, minVersion)
	}
	return nil
}

func (h *Host) ping(ctx context.Context) (string, error) {
	var pong struct {
		Version string `json:"version"`
	}
	_, err := h.request(ctx, "ping", struct{}{}, &pong)
	return pong.Version, err
}

// waitPing waits up to five seconds for a server just started to answer.
func (h *Host) waitPing(ctx context.Context) (string, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		version, err := h.ping(ctx)
		if !errors.Is(err, errNoServer) || time.Now().After(deadline) {
			return version, err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// start starts a herdr server in a session of its own, so it outlives this
// process, with no terminal to write to. It is waited for, so a server that
// ends while this process runs, as a second one started on the same socket
// does at once, leaves no zombie behind.
func (h *Host) start() error {
	cmd := exec.Command("herdr", "server")
	cmd.Env = os.Environ()
	if h.Socket != "" {
		cmd.Env = append(cmd.Env, "HERDR_SOCKET_PATH="+h.Socket)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("herdr: start the server: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// older reports whether version a is older than b, both as major.minor.patch.
// Each part counts by the digits it starts with, so 0.9.6-dev is 0.9.6; a part
// with none counts as zero.
func older(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = leadingNumber(pa[i])
		}
		if i < len(pb) {
			y = leadingNumber(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// leadingNumber is the number s starts with, and zero when s starts with no
// digit.
func leadingNumber(s string) int {
	end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(s)
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// Focus makes the workspace the one the server shows, and every terminal
// attached to the server moves to it: herdr's API has no way to move one
// client alone. With none attached, the next terminal that attaches lands on
// it.
func (h *Host) Focus(ctx context.Context, ref revier.TargetRef) error {
	workspace, err := h.current(ctx, ref)
	if err != nil {
		return err
	}
	_, err = h.request(ctx, "workspace.focus", map[string]string{"workspace_id": workspace}, nil)
	return err
}

// Close closes the workspace and every pane in it.
func (h *Host) Close(ctx context.Context, ref revier.TargetRef) error {
	workspace, err := h.current(ctx, ref)
	if err != nil {
		return err
	}
	_, err = h.request(ctx, "workspace.close", map[string]string{"workspace_id": workspace}, nil)
	return err
}

// current is the workspace ref names, once the server that assigned its id is
// the one answering now. herdr numbers workspaces per server, so after a
// restart the same id names an unrelated workspace, and neither a focus nor a
// close is to be made on the wrong one.
func (h *Host) current(ctx context.Context, ref revier.TargetRef) (string, error) {
	if err := h.own(ref); err != nil {
		return "", err
	}
	pid, err := h.request(ctx, "ping", struct{}{}, nil)
	if err != nil {
		return "", err
	}
	return workspaceAt(ref, pid)
}

// workspaceAt is the workspace ref names, once pid, the server that answered,
// is the server that assigned its id.
func workspaceAt(ref revier.TargetRef, pid int) (string, error) {
	server, workspace, _ := strings.Cut(ref.ID, "/")
	if server != strconv.Itoa(pid) {
		return "", fmt.Errorf("herdr: workspace %s is gone; the server that answers now is %d", ref.ID, pid)
	}
	return workspace, nil
}

// AttachCommand is the argv that puts the calling terminal onto the server.
// It lands on the workspace the server shows, which run-or-raise has focused
// already. A private socket is carried along. The caller execs it; nothing
// here takes over a terminal.
func (h *Host) AttachCommand(ref revier.TargetRef) ([]string, error) {
	if err := h.own(ref); err != nil {
		return nil, err
	}
	if h.Socket != "" {
		return []string{"env", "HERDR_SOCKET_PATH=" + h.Socket, "herdr"}, nil
	}
	return []string{"herdr"}, nil
}

// Encloses reports whether herdr refuses the attach as nested, and the
// focus has already moved the host's server (revier.Encloser). herdr starts
// each pane with HERDR_ENV and refuses to start wherever it is set, whatever
// the terminal, so a terminal started from a pane is refused too. A pane of
// another server still execs the attach, and herdr refuses it.
func (h *Host) Encloses(context.Context) (bool, error) {
	socket := os.Getenv("HERDR_SOCKET_PATH")
	return os.Getenv("HERDR_ENV") != "" && socket != "" && socket == h.socket(), nil
}

// own refuses a ref this host did not produce.
func (h *Host) own(ref revier.TargetRef) error {
	if ref.Host != h.Name() || !strings.Contains(ref.ID, "/") {
		return fmt.Errorf("herdr: %s/%s is not a herdr workspace", ref.Host, ref.ID)
	}
	return nil
}
