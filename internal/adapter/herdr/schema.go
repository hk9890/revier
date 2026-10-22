package herdr

import "encoding/json"

// The shapes below hold only the fields this host reads. herdr asks a JSON
// client to ignore the fields it does not know, which is also what lets a
// newer herdr add one without breaking the decode.

// response is one line herdr answers a request with: a result or an error.
type response struct {
	Result json.RawMessage `json:"result"`
	Error  *apiError       `json:"error"`
}

// apiError is herdr's error object. Code is the stable part, such as
// not_found; Message is for a person.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type snapshotResult struct {
	Snapshot snapshot `json:"snapshot"`
}

type snapshot struct {
	Version            string      `json:"version"`
	FocusedWorkspaceID string      `json:"focused_workspace_id"`
	Workspaces         []workspace `json:"workspaces"`
	Panes              []pane      `json:"panes"`
}

type workspace struct {
	ID     string            `json:"workspace_id"`
	Label  string            `json:"label"`
	Tokens map[string]string `json:"tokens"`
}

type pane struct {
	ID            string            `json:"pane_id"`
	WorkspaceID   string            `json:"workspace_id"`
	TabID         string            `json:"tab_id"`
	TerminalTitle string            `json:"terminal_title"`
	Tokens        map[string]string `json:"tokens"`
}
