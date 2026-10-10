package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hk9890/revier/pkg/revier"
)

// tailBytes is how much of the end of a transcript Detail reads. The last
// thing an agent said is near the end; a tool's output after it can be long,
// and a line that starts before the window is skipped whole.
const tailBytes = 512 << 10

// errNoMessage is a transcript read whole with no turn in it: nothing the user
// asked, and nothing the agent said or called. A session that has not started
// is a normal outcome, not a read that broke.
var errNoMessage = fmt.Errorf("%w: no prompt, message or tool call in the transcript's tail", revier.ErrNoDetail)

// Detail is the last thing the agent in the panel said and the turn it is in,
// read from the end of its transcript: ~/.claude/projects/<dir>/<session
// id>.jsonl, the session named for the panel's pid by the listing Inspect
// keeps.
//
// The transcript's format is Claude Code's internal one, and it changes
// between releases. So this is display only, as revier.Detailed says
// (decisions.md D106): a line that does not parse is passed over, and a
// transcript with no line that does is an error, which the pane shows as
// nothing. A panel the listing does not hold, and a session with no
// transcript yet, are ordinary and answer revier.ErrNoDetail; only a read
// that failed is a failure.
//
// The pane asks every refresh, so a transcript is read again only when its
// size or modification time changed since the last read.
func (p *Probe) Detail(ctx context.Context, panel revier.Panel) (revier.AgentDetail, error) {
	listed, err := p.listing(ctx)
	if err != nil {
		return revier.AgentDetail{}, err
	}
	s, ok := listed[panel.PID]
	if !ok || panel.PID == 0 || s.SessionID == "" {
		return revier.AgentDetail{}, fmt.Errorf("%w: pid %d is in no listed session", revier.ErrNoDetail, panel.PID)
	}
	path, info, err := p.transcript(s)
	if err != nil {
		return revier.AgentDetail{}, err
	}
	return p.said(s.SessionID, path, info)
}

// read is one transcript's last message as it was read, and the file and the
// size and modification time it was read at.
type read struct {
	path   string
	size   int64
	mod    time.Time
	detail revier.AgentDetail
	err    error
}

// said is the last message of the session's transcript, from the last read of
// it while the file is the same one and unchanged. It is kept per session
// rather than per path, so the listing that drops a session drops what was
// read for it (forget): a message can be tens of kilobytes, and the surface
// outlives every agent in it.
func (p *Probe) said(id, path string, info os.FileInfo) (revier.AgentDetail, error) {
	p.readsMu.Lock()
	defer p.readsMu.Unlock()
	if r, ok := p.reads[id]; ok && r.path == path && r.size == info.Size() && r.mod.Equal(info.ModTime()) {
		return r.detail, r.err
	}
	r := read{path: path, size: info.Size(), mod: info.ModTime()}
	var tail []byte
	if tail, r.err = readTail(path, tailBytes); r.err == nil {
		r.detail, r.err = lastSaid(tail)
	}
	if p.reads == nil {
		p.reads = map[string]read{}
	}
	p.reads[id] = r
	return r.detail, r.err
}

// sweep is what a look through every project directory came to for one
// session: where its transcript was found, empty for nowhere, and when the
// look was made.
type sweep struct {
	path string
	at   time.Time
}

// transcript finds a session's transcript. Claude Code files it under a
// directory named from a working directory, with every character that is not a
// letter or a digit made a '-': mostly the one the session works in, which the
// listing gives, so that file is looked at first. A session that entered a
// worktree stays filed under the directory it started in, and a long path or
// CLAUDE_CODE_PROJECT_DIR_NAME spells the name otherwise; those are found by
// looking in every project directory, where the file written last is the
// session's own.
//
// The sweep's answer is kept either way. A hit stands while its file is there,
// so the look is paid once; a miss stands for MaxAge, so a session that has
// written no transcript yet costs one sweep in that time rather than a stat
// per project directory on every refresh, and still finds the file once it
// appears.
func (p *Probe) transcript(s listedSession) (string, os.FileInfo, error) {
	projects := filepath.Join(filepath.Dir(p.sessionsDir()), "projects")
	name := s.SessionID + ".jsonl"
	if s.Cwd != "" {
		path := filepath.Join(projects, projectDir(s.Cwd), name)
		if info, err := os.Stat(path); err == nil {
			return path, info, nil
		}
	}
	p.readsMu.Lock()
	known, swept := p.found[s.SessionID]
	p.readsMu.Unlock()
	if swept && known.path != "" {
		if info, err := os.Stat(known.path); err == nil {
			return known.path, info, nil
		}
	}
	if swept && known.path == "" && p.now().Sub(known.at) < MaxAge {
		return "", nil, noTranscript(s.SessionID)
	}
	// No projects directory at all is a Claude Code that has filed nothing
	// yet, not a read that broke.
	dirs, err := os.ReadDir(projects)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", nil, err
	}
	var found string
	var newest os.FileInfo
	for _, d := range dirs {
		path := filepath.Join(projects, d.Name(), name)
		if info, err := os.Stat(path); err == nil && (newest == nil || info.ModTime().After(newest.ModTime())) {
			found, newest = path, info
		}
	}
	p.readsMu.Lock()
	if p.found == nil {
		p.found = map[string]sweep{}
	}
	p.found[s.SessionID] = sweep{path: found, at: p.now()}
	p.readsMu.Unlock()
	if newest == nil {
		return "", nil, noTranscript(s.SessionID)
	}
	return found, newest, nil
}

// noTranscript is a session Claude Code lists with no transcript under it yet:
// the agent has started and said nothing, which is ordinary.
func noTranscript(id string) error {
	return fmt.Errorf("%w: session %s has written no transcript", revier.ErrNoDetail, id)
}

// forget drops what was read and swept for the sessions a listing no longer
// holds. The surface is one long-lived process, and a read keeps the whole of
// an agent's last message, so a session that ended must not be paid for until
// the process exits.
func (p *Probe) forget(listed map[int]listedSession) {
	live := make(map[string]bool, len(listed))
	for _, s := range listed {
		live[s.SessionID] = true
	}
	p.readsMu.Lock()
	defer p.readsMu.Unlock()
	maps.DeleteFunc(p.reads, func(id string, _ read) bool { return !live[id] })
	maps.DeleteFunc(p.found, func(id string, _ sweep) bool { return !live[id] })
}

// projectDir is the directory name Claude Code files a working directory's
// sessions under: the path with every character that is not an ASCII letter
// or digit made a '-'.
func projectDir(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
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
	IsSidechain      bool      `json:"isSidechain"`
	IsMeta           bool      `json:"isMeta"`
	IsCompactSummary bool      `json:"isCompactSummary"`
	Timestamp        time.Time `json:"timestamp"`
	Message          struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block is one part of a message's content: text the agent or the user wrote,
// a tool the agent called, or what a tool answered.
type block struct {
	Type      string                     `json:"type"`
	Text      string                     `json:"text"`
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	Input     map[string]json.RawMessage `json:"input"`
	ToolUseID string                     `json:"tool_use_id"`
	IsError   bool                       `json:"is_error"`
}

// briefRunes is how much of a prompt or of a tool's input Detail keeps: the
// pane draws a line of each, and a prompt can be a pasted file.
const briefRunes = 300

// lastSaid walks the lines back from the end: through the turn the agent is
// in, which is every tool it called since the user last wrote, and on to the
// last text the agent wrote in its own conversation, which can be older than
// the turn. A subagent's lines (isSidechain) are its
// work, not the agent's. A line Claude Code wrote into the conversation
// itself (isMeta), and the summary it put in a compacted conversation's place
// (isCompactSummary), are not the user's: the turn goes on through both.
//
// A call's result is on a line after the call, so walking back meets it
// first; a call met with no result is still open.
func lastSaid(tail []byte) (revier.AgentDetail, error) {
	var d revier.AgentDetail
	failed := map[string]bool{}
	inTurn := true
	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0 && (inTurn || d.Message == ""); i-- {
		var e entry
		if json.Unmarshal(lines[i], &e) != nil || e.IsSidechain || e.IsMeta || e.IsCompactSummary {
			continue
		}
		text, blocks := contentOf(e.Message.Content)
		switch e.Message.Role {
		case "assistant":
			if d.Message == "" && text != "" {
				d.Message, d.At = text, e.Timestamp
			}
			for at := len(blocks) - 1; at >= 0 && inTurn; at-- {
				if b := blocks[at]; b.Type == "tool_use" {
					isFailed, done := failed[b.ID]
					d.Tools = append(d.Tools, revier.ToolCall{Name: b.Name, Input: inputOf(b.Input), Pending: !done, Failed: isFailed})
				}
			}
		case "user":
			if !inTurn {
				continue
			}
			for _, b := range blocks {
				if b.Type == "tool_result" {
					failed[b.ToolUseID] = b.IsError
				}
			}
			if prompt := promptOf(text); prompt != "" {
				d.Prompt, inTurn = prompt, false
			}
		}
	}
	if d.IsZero() {
		return revier.AgentDetail{}, errNoMessage
	}
	slices.Reverse(d.Tools)
	return d, nil
}

// contentOf is a message's content: its text, which is a plain string or the
// text blocks of a block list joined, and the blocks. A tool call or a
// thinking block is not something the agent said.
func contentOf(content json.RawMessage) (string, []block) {
	var plain string
	if json.Unmarshal(content, &plain) == nil {
		return strings.TrimSpace(plain), nil
	}
	var blocks []block
	if json.Unmarshal(content, &blocks) != nil {
		return "", nil
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			texts = append(texts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(texts, "\n\n"), blocks
}

var (
	slashCommand = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	slashArgs    = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	pasted       = regexp.MustCompile(`(?s)<pasted_content[^>]*>.*?</pasted_content[^>]*>`)
)

// notPrompts open the user lines the user asked nothing in: what a local
// command printed for the user alone, a shell command of the `!` mode and
// what it printed, the report of a background task, and an interrupt.
var notPrompts = []string{"<local-command-", "<bash-", "<task-notification>", "[Request interrupted"}

// promptOf is a user line's text as the user typed it, or nothing for a line
// the user asked nothing in (notPrompts). Claude Code files a slash command
// as tags around its name and its arguments, and a paste under tags before
// the words typed with it.
func promptOf(text string) string {
	if slices.ContainsFunc(notPrompts, func(open string) bool { return strings.HasPrefix(text, open) }) {
		return ""
	}
	if name := slashCommand.FindStringSubmatch(text); name != nil && strings.HasPrefix(text, "<command-") {
		typed := name[1]
		if args := slashArgs.FindStringSubmatch(text); args != nil {
			typed += " " + args[1]
		}
		return brief(typed)
	}
	if typed := brief(pasted.ReplaceAllString(text, " ")); typed != "" {
		return typed
	}
	return brief(text)
}

// inputKeys are the fields of a tool's input that say what it was called on,
// the most telling first. A tool with none of them is named and nothing more.
var inputKeys = []string{"command", "file_path", "notebook_path", "pattern", "url", "query", "skill", "description", "prompt"}

// inputOf is what a tool was called on, as one short line.
func inputOf(input map[string]json.RawMessage) string {
	for _, key := range inputKeys {
		var value string
		if json.Unmarshal(input[key], &value) == nil && strings.TrimSpace(value) != "" {
			return brief(value)
		}
	}
	// A question to the user is the one input worth reading that is not a
	// string at the top.
	var questions []struct {
		Question string `json:"question"`
	}
	if json.Unmarshal(input["questions"], &questions) == nil && len(questions) > 0 {
		return brief(questions[0].Question)
	}
	return ""
}

// brief is a text on one line, cut to briefRunes.
func brief(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > briefRunes {
		return string(r[:briefRunes])
	}
	return s
}
