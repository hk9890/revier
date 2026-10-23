package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
)

// The shapes below hold only the fields this host reads. herdr asks a JSON
// client to ignore the fields it does not know, which is also what lets a
// newer herdr add one without breaking the decode.

// response is one line herdr answers a request with: a result or an error.
type response struct {
	Result json.RawMessage `json:"result"`
	Error  *apiError       `json:"error"`
}

// apiError is herdr's error object. Code is the stable part, such as
// pane_not_found; Message is for a person.
type apiError struct {
	Method  string `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("herdr %s: %s: %s", e.Method, e.Code, e.Message)
}

// isCode reports whether herdr answered err with the error code.
func isCode(err error, code string) bool {
	var e *apiError
	return errors.As(err, &e) && e.Code == code
}

type snapshotResult struct {
	Snapshot snapshot `json:"snapshot"`
}

type snapshot struct {
	FocusedWorkspaceID string      `json:"focused_workspace_id"`
	Workspaces         []workspace `json:"workspaces"`
	Panes              []pane      `json:"panes"`
	Layouts            []tabLayout `json:"layouts"`
}

type tabLayout struct {
	TabID         string `json:"tab_id"`
	FocusedPaneID string `json:"focused_pane_id"`
}

type workspace struct {
	ID          string            `json:"workspace_id"`
	Label       string            `json:"label"`
	ActiveTabID string            `json:"active_tab_id"`
	Tokens      map[string]string `json:"tokens"`
}

type processInfoResult struct {
	ProcessInfo struct {
		ShellPID int `json:"shell_pid"`
	} `json:"process_info"`
}

type pane struct {
	ID            string            `json:"pane_id"`
	WorkspaceID   string            `json:"workspace_id"`
	TabID         string            `json:"tab_id"`
	TerminalTitle string            `json:"terminal_title"`
	Tokens        map[string]string `json:"tokens"`
}
