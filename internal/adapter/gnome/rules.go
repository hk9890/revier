package gnome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/hk9890/revier/pkg/revier"
)

// Rules reads the placement rules of the window control extension, through
// `wctl rules`: the user's rules.json, and the drop-in files of rules.d,
// which the extension loads after it in byte order of their names. revier's
// own rules are the drop-in ownRules, which revier writes whole. The
// extension reads the files again on every change, so a rule written here
// holds for the next window, with no restart.
//
// wctl writes as well as reads, so every rules command this type runs goes
// through exec, which allows readRuleCommands alone, as Keys does. The
// writes are RuleWriter's.
type Rules struct {
	// run runs wctl with the arguments and returns its standard output.
	// Tests replace it; production leaves it nil and runs the wctl on PATH.
	run func(ctx context.Context, args ...string) ([]byte, error)
	// version is what Probe read, for CanAdd.
	version versionDoc
}

func (r *Rules) Name() string { return "gnome" }

// ownRules is revier's drop-in file in rules.d, and ownTemp the name it is
// written to first: a name that starts with a dot, which the extension does
// not load, so it never reads half the file.
const (
	ownRules = "revier.json"
	ownTemp  = ".revier.json.tmp"
)

// minRulesWctl is the oldest wctl that names rules.d and reads and checks
// one file of it.
var minRulesWctl = [3]int{0, 24, 0}

// ruleCapabilities are what the loaded extension must read for revier's
// rules: the drop-in files, and every key revier writes. A file with a key
// the extension does not read is refused whole, so revier writes none until
// the extension reads all of these.
var ruleCapabilities = []string{"rules", "rules-overlay", "rules-min-width", "rules-dropin"}

// Probe reports the rules usable when wctl is minRulesWctl or later.
// Listing the rules and removing revier's need no running extension, so a
// status and an uninstall work without one; CanAdd says whether an install
// may write.
func (r *Rules) Probe(ctx context.Context) error {
	if r.run == nil {
		if _, err := exec.LookPath("wctl"); err != nil {
			return fmt.Errorf("wctl not on PATH: %w", err)
		}
	}
	doc, err := wctlVersion(ctx, r.wctl)
	r.version = doc
	if err != nil {
		return err
	}
	return rulesWctl(doc)
}

func rulesWctl(doc versionDoc) error {
	if !atLeast(doc.Wctl, minRulesWctl) {
		return fmt.Errorf("wctl %s does not write rules.d, where revier keeps its rules: upgrade it to %d.%d.%d", doc.Wctl, minRulesWctl[0], minRulesWctl[1], minRulesWctl[2])
	}
	return nil
}

// CanAdd reports, from what Probe read, whether the extension the shell has
// loaded reads revier's file and every key in it.
func (r *Rules) CanAdd() error {
	doc := r.version
	if err := rulesWctl(doc); err != nil {
		return err
	}
	if doc.Extension == nil {
		return fmt.Errorf("no extension answers, so revier cannot tell whether it reads rules.d: %s", doc.Message)
	}
	for _, c := range ruleCapabilities {
		if !slices.Contains(doc.Capabilities, c) {
			return fmt.Errorf("extension %s does not read %q, so it would not apply revier's rules: upgrade it", *doc.Extension, c)
		}
	}
	return nil
}

// checkDoc is what `wctl rules check --json` prints: the user's file, and
// each drop-in file in the order the extension loads them.
type checkDoc struct {
	Path    string      `json:"path"`
	Valid   bool        `json:"valid"`
	Error   string      `json:"error"`
	Dropins []dropinDoc `json:"dropins"`
}

type dropinDoc struct {
	Path  string `json:"path"`
	Valid bool   `json:"valid"`
	Error string `json:"error"`
}

// Read lists the user's file, revier's, and each drop-in file the extension
// loads before revier's: a constant number of commands, and one more for
// each such file.
func (r *Rules) Read(ctx context.Context) (revier.WindowRuleSet, error) {
	var set revier.WindowRuleSet
	dir, err := r.dropinDir(ctx)
	if err != nil {
		return set, err
	}
	set.Own.Path = filepath.Join(dir, ownRules)
	// check exits 1 when a file is broken, and still prints them all.
	raw, err := r.exec(ctx, "rules", "check", "--json")
	var check checkDoc
	if jerr := json.Unmarshal(bytes.TrimSpace(raw), &check); jerr != nil {
		if err != nil {
			return set, err
		}
		return set, fmt.Errorf("decode wctl rules check: %w", jerr)
	}
	if !check.Valid {
		return set, errors.New(check.Error)
	}
	set.User.Path = check.Path
	if set.User.Rules, err = r.list(ctx); err != nil {
		return set, err
	}
	for _, d := range check.Dropins {
		name := filepath.Base(d.Path)
		switch {
		case name == ownRules:
			set.OwnExists, set.OwnBroken = true, d.Error
			if d.Valid {
				set.OwnBroken = ""
				if set.Own.Rules, err = r.list(ctx, "--file", d.Path); err != nil {
					return set, err
				}
			}
		case name < ownRules && d.Valid:
			rules, err := r.list(ctx, "--file", d.Path)
			if err != nil {
				return set, err
			}
			set.Earlier = append(set.Earlier, revier.WindowRuleFile{Path: d.Path, Rules: rules})
		}
	}
	return set, nil
}

func (r *Rules) list(ctx context.Context, file ...string) ([]revier.WindowRule, error) {
	raw, err := r.exec(ctx, append([]string{"rules", "list", "--json"}, file...)...)
	if err != nil {
		return nil, err
	}
	return decodeRules(raw)
}

func (r *Rules) dropinDir(ctx context.Context) (string, error) {
	raw, err := r.exec(ctx, "rules", "path", "--dir")
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("wctl rules path --dir printed %q, not a path", dir)
	}
	return dir, nil
}

// RuleWriter is Rules with the writes: a rule out of the user's file, and
// revier's file written whole or removed.
type RuleWriter struct{ Rules }

// RemoveUser runs `wctl rules remove`, which writes the user's file through
// a link to a dotfiles repository too.
func (w *RuleWriter) RemoveUser(ctx context.Context, index int) error {
	_, err := w.write(ctx, "rules", "remove", strconv.Itoa(index))
	return err
}

// WriteOwn writes the rules to ownTemp, has wctl check it as the extension
// would load it, and renames it over ownRules. A file wctl refuses is never
// put in place.
func (w *RuleWriter) WriteOwn(ctx context.Context, rules []revier.WindowRule) error {
	dir, err := w.dropinDir(ctx)
	if err != nil {
		return err
	}
	data, err := encodeRules(rules)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ownTemp)
	defer func() { _ = os.Remove(tmp) }()
	if err := writeSynced(tmp, data); err != nil {
		return err
	}
	if _, err := w.exec(ctx, "rules", "check", "--file", tmp); err != nil {
		return fmt.Errorf("wctl refuses the rules revier writes: %w", err)
	}
	return os.Rename(tmp, filepath.Join(dir, ownRules))
}

// RemoveOwn deletes revier's file.
func (w *RuleWriter) RemoveOwn(ctx context.Context) error {
	dir, err := w.dropinDir(ctx)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, ownRules)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ruleDoc is one rule of revier's as the file holds it, in the layout
// `wctl rules add` writes.
type ruleDoc struct {
	Match   matchDoc `json:"match"`
	Place   []string `json:"place,omitempty"`
	Overlay bool     `json:"overlay,omitempty"`
}

type matchDoc struct {
	Class    string `json:"class"`
	MinWidth int    `json:"min_width,omitempty"`
}

// encodeRules is the file for rules, which are revier's: a class, a place,
// and an overlay. The same rules give the same bytes.
func encodeRules(rules []revier.WindowRule) ([]byte, error) {
	docs := make([]ruleDoc, len(rules))
	for i, r := range rules {
		docs[i] = ruleDoc{Match: matchDoc{Class: r.Class, MinWidth: r.MinWidth}, Place: r.Place, Overlay: r.Overlay}
	}
	data, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// readRuleCommands is every rules command Rules may run, and
// writeRuleCommands every one RuleWriter adds to them.
var (
	readRuleCommands  = []string{"list", "check", "path"}
	writeRuleCommands = []string{"remove"}
)

func (r *Rules) exec(ctx context.Context, args ...string) ([]byte, error) {
	if err := ruleCommand(readRuleCommands, args); err != nil {
		return nil, err
	}
	return r.wctl(ctx, args...)
}

func (w *RuleWriter) write(ctx context.Context, args ...string) ([]byte, error) {
	if err := ruleCommand(writeRuleCommands, args); err != nil {
		return nil, err
	}
	return w.wctl(ctx, args...)
}

// ruleCommand refuses a wctl command that is not a rules subcommand in
// allowed.
func ruleCommand(allowed []string, args []string) error {
	if len(args) < 2 || args[0] != "rules" || !slices.Contains(allowed, args[1]) {
		return fmt.Errorf("gnome rules: wctl %v is not allowed here", args)
	}
	return nil
}

func (r *Rules) wctl(ctx context.Context, args ...string) ([]byte, error) {
	if r.run != nil {
		return r.run(ctx, args...)
	}
	return execRun(ctx, "wctl", args...)
}

// decodeRules reads `wctl rules list --json`: one file's array as it is.
// Anything in a rule that WindowRule does not carry marks it Other, so the
// core never takes a rule of the user's for one of revier's.
func decodeRules(raw []byte) ([]revier.WindowRule, error) {
	var docs []json.RawMessage
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("decode wctl rules list: %w", err)
	}
	out := make([]revier.WindowRule, len(docs))
	for i, doc := range docs {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(doc, &fields); err != nil {
			return nil, fmt.Errorf("decode wctl rules list: rule %d: %w", i, err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, doc); err != nil {
			return nil, fmt.Errorf("decode wctl rules list: rule %d: %w", i, err)
		}
		out[i] = decodeRule(fields)
		out[i].Raw = compact.String()
	}
	return out, nil
}

func decodeRule(doc map[string]json.RawMessage) revier.WindowRule {
	var rule revier.WindowRule
	for key, value := range doc {
		var ok bool
		switch key {
		case "match":
			ok = decodeMatch(value, &rule)
		case "place":
			rule.Place, ok = decodePlace(value)
		case "overlay":
			ok = json.Unmarshal(value, &rule.Overlay) == nil
		}
		if !ok {
			rule.Other = true
		}
	}
	return rule
}

func decodeMatch(raw json.RawMessage, rule *revier.WindowRule) bool {
	var match map[string]json.RawMessage
	if json.Unmarshal(raw, &match) != nil {
		return false
	}
	ok := true
	for key, value := range match {
		switch key {
		case "class":
			ok = json.Unmarshal(value, &rule.Class) == nil && ok
		case "min_width":
			ok = json.Unmarshal(value, &rule.MinWidth) == nil && ok
		default:
			ok = false
		}
	}
	return ok
}

// decodePlace reads the four values of a place, each a string or a number,
// as the strings WindowPlacer.Place reads.
func decodePlace(raw json.RawMessage) ([]string, bool) {
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != 4 {
		return nil, false
	}
	out := make([]string, len(values))
	for i, v := range values {
		var s string
		if json.Unmarshal(v, &s) == nil {
			out[i] = s
			continue
		}
		var n json.Number
		if json.Unmarshal(v, &n) != nil {
			return nil, false
		}
		out[i] = n.String()
	}
	return out, true
}
