package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// tailBytes is how much of the end of a transcript Detail reads. The last
// thing an agent said is near the end; a tool's output after it can be long,
// and a line that starts before the window is skipped whole.
const tailBytes = 512 << 10

// errNoMessage is a transcript read whole with nothing the agent said in it.
var errNoMessage = errors.New("no assistant message in the transcript's tail")

// Detail is the last thing the agent in the panel said, read from the end of
// its transcript: ~/.claude/projects/<dir>/<session id>.jsonl, the session
// named for the panel's pid by the listing Inspect keeps.
//
// The transcript's format is Claude Code's internal one, and it changes
// between releases. So this is display only (revier.Detailed, decisions.md
// D105): a line that does not parse is passed over, and a transcript with no
// line that does is an error, which the pane shows as nothing.
func (p *Probe) Detail(ctx context.Context, panel revier.Panel) (revier.AgentDetail, error) {
	listed, err := p.listing(ctx)
	if err != nil {
		return revier.AgentDetail{}, err
	}
	s, ok := listed[panel.PID]
	if !ok || panel.PID == 0 || s.SessionID == "" {
		return revier.AgentDetail{}, fmt.Errorf("pid %d: no listed session", panel.PID)
	}
	path, err := p.transcript(s.SessionID)
	if err != nil {
		return revier.AgentDetail{}, err
	}
	tail, err := readTail(path, tailBytes)
	if err != nil {
		return revier.AgentDetail{}, err
	}
	return lastSaid(tail)
}

// transcript finds a session's transcript under any project directory, rather
// than spell the directory's name from the cwd as Claude Code does: that
// spelling is Claude Code's, and an agent that moved with /cd is filed under
// the directory it moved to.
func (p *Probe) transcript(session string) (string, error) {
	pattern := filepath.Join(filepath.Dir(p.sessionsDir()), "projects", "*", session+".jsonl")
	found, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no transcript for session %s", session)
	}
	return found[0], nil
}

// readTail is the last n bytes of a file, from the first whole line in them.
func readTail(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	from := max(info.Size()-n, 0)
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if from > 0 {
		if at := bytes.IndexByte(data, '\n'); at >= 0 {
			data = data[at+1:]
		}
	}
	return data, nil
}

// entry is the part of a transcript line Detail reads. Everything else a line
// carries is ignored, so a field Claude Code adds costs nothing.
type entry struct {
	IsSidechain bool      `json:"isSidechain"`
	Timestamp   time.Time `json:"timestamp"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// lastSaid walks the lines back from the end to the last text the agent wrote
// in its own conversation. A subagent's lines (isSidechain) are its work, not
// what the agent said to the user.
func lastSaid(tail []byte) (revier.AgentDetail, error) {
	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var e entry
		if json.Unmarshal(lines[i], &e) != nil || e.IsSidechain || e.Message.Role != "assistant" {
			continue
		}
		if text := textOf(e.Message.Content); text != "" {
			return revier.AgentDetail{Message: text, At: e.Timestamp}, nil
		}
	}
	return revier.AgentDetail{}, errNoMessage
}

// textOf is the text of a message's content: a plain string, or the text
// blocks of a block list, joined. A tool call or a thinking block is not
// something the agent said.
func textOf(content json.RawMessage) string {
	var plain string
	if json.Unmarshal(content, &plain) == nil {
		return strings.TrimSpace(plain)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			texts = append(texts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(texts, "\n\n")
}
