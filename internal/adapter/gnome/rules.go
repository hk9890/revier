package gnome

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"

	"github.com/hk9890/revier/pkg/revier"
)

// Rules reads the placement rules of the window control extension, through
// `wctl rules`. wctl finds the file, and writes it in place, through a link
// to a dotfiles repository too. The extension reads the file again on every
// change, so a rule written here holds for the next window, with no restart.
//
// wctl writes as well as reads, so the commands this type may run are listed
// in readRuleCommands and checked before each one runs, as Keys does.
type Rules struct {
	// run runs wctl with the arguments and returns its standard output.
	// Tests replace it; production leaves it nil and runs the wctl on PATH.
	run func(ctx context.Context, args ...string) ([]byte, error)
}

func (r *Rules) Name() string { return "gnome" }

// ruleCapabilities are what the loaded extension must read for revier's
// rules. A rules file with a key the extension does not read is refused
// whole, with every rule of the user's in it, so revier writes none until
// the extension reads all of these.
var ruleCapabilities = []string{"rules", "rules-overlay", "rules-min-width"}

// Probe reports the rules usable when wctl is minWctl or later and the
// extension the shell has loaded reads every key revier writes.
func (r *Rules) Probe(ctx context.Context) error {
	if r.run == nil {
		if _, err := exec.LookPath("wctl"); err != nil {
			return fmt.Errorf("wctl not on PATH: %w", err)
		}
	}
	raw, err := r.wctl(ctx, "version", "--json")
	var doc versionDoc
	if json.Unmarshal(bytes.TrimSpace(raw), &doc) != nil || doc.Wctl == "" {
		if err != nil {
			return fmt.Errorf("wctl is present but not answering: %w", err)
		}
		return fmt.Errorf("wctl version --json names no version: revier needs wctl %s or later", minWctlText())
	}
	if !atLeast(doc.Wctl, minWctl) {
		return fmt.Errorf("wctl %s is older than %s, the oldest revier drives", doc.Wctl, minWctlText())
	}
	if doc.Extension == nil {
		return fmt.Errorf("no extension answers, so revier cannot tell which rules it reads: %s", doc.Message)
	}
	for _, c := range ruleCapabilities {
		if !slices.Contains(doc.Capabilities, c) {
			return fmt.Errorf("extension %s does not read %q in a rule: upgrade it", *doc.Extension, c)
		}
	}
	return nil
}

// Read lists the rules in one command.
func (r *Rules) Read(ctx context.Context) ([]revier.WindowRule, error) {
	raw, err := r.exec(ctx, readRuleCommands, "rules", "list", "--json")
	if err != nil {
		return nil, err
	}
	return decodeRules(raw)
}

// RuleWriter is Rules with the two commands that change the file.
type RuleWriter struct{ Rules }

// Add runs `wctl rules add`. The rule is always one of revier's: a class, a
// place, and an overlay.
func (w *RuleWriter) Add(ctx context.Context, at int, rule revier.WindowRule) error {
	args := []string{"rules", "add", "-c", rule.Class}
	if rule.MinWidth > 0 {
		args = append(args, "--min-width", strconv.Itoa(rule.MinWidth))
	}
	if rule.Overlay {
		args = append(args, "--overlay")
	}
	if len(rule.Place) > 0 {
		args = append(append(args, "place"), rule.Place...)
	}
	args = append(args, "--at", strconv.Itoa(at))
	_, err := w.exec(ctx, writeRuleCommands, args...)
	return err
}

// Remove runs `wctl rules remove`.
func (w *RuleWriter) Remove(ctx context.Context, index int) error {
	_, err := w.exec(ctx, writeRuleCommands, "rules", "remove", strconv.Itoa(index))
	return err
}

// readRuleCommands is every rules command Rules may run, and
// writeRuleCommands every one RuleWriter may run.
var (
	readRuleCommands  = []string{"list"}
	writeRuleCommands = []string{"list", "add", "remove"}
)

func (r *Rules) exec(ctx context.Context, allowed []string, args ...string) ([]byte, error) {
	if err := ruleCommand(allowed, args); err != nil {
		return nil, err
	}
	return r.wctl(ctx, args...)
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

// decodeRules reads `wctl rules list --json`: the file's array as it is.
// Anything in a rule that WindowRule does not carry marks it Other, so the
// core never takes a rule of the user's for one of revier's.
func decodeRules(raw []byte) ([]revier.WindowRule, error) {
	var docs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("decode wctl rules list: %w", err)
	}
	out := make([]revier.WindowRule, len(docs))
	for i, doc := range docs {
		out[i] = decodeRule(doc)
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
			ok = ok && json.Unmarshal(value, &rule.Class) == nil
		case "min_width":
			ok = ok && json.Unmarshal(value, &rule.MinWidth) == nil
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
