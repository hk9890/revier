package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/hk9890/revier/pkg/revier"
)

// A shared target is a [[target]] table in config.toml. Every project file
// that is not a link gets it, merged with the project's own target of the
// same name (decisions.md D59). The merge is on the tables as TOML decoded
// them, not on revier.Target, because a decoded struct cannot tell a field
// the project left out from one it set to its zero value.
//
// A table merges key by key, at every depth: a project that writes
// `match = { title = "..." }` keeps the shared class. Any other value - a
// string, a bool, a list such as panels or launch - replaces the shared one
// whole.

// validateShared refuses a shared target that no project could use: one with
// no name, a name given twice, or a value of the wrong type. Whether it is
// complete - a realization, a match - is checked per project, after the
// merge, because a project may supply what it leaves out.
func validateShared(shared []map[string]any) error {
	var errs []error
	seen := map[string]bool{}
	for i, t := range shared {
		name, _ := t["name"].(string)
		if name == "" {
			errs = append(errs, fmt.Errorf("target %d has no name", i+1))
			continue
		}
		if seen[name] {
			errs = append(errs, fmt.Errorf("target %q declared twice", name))
		}
		seen[name] = true
		var typed revier.Target
		if err := recode(t, &typed); err != nil {
			errs = append(errs, fmt.Errorf("target %q: %w", name, err))
		}
		if remote, ok := t["remote"].(map[string]any); ok {
			var parts struct {
				Window  *revier.Realization `toml:"window"`
				Runtime *revier.Realization `toml:"runtime"`
			}
			if err := recode(remote, &parts); err != nil {
				errs = append(errs, fmt.Errorf("target %q remote: %w", name, err))
			}
			errs = append(errs, validateRemoteKeys(name, remote)...)
		}
	}
	return errors.Join(errs...)
}

// validateRemoteKeys refuses a key under [target.remote] that is not one of
// the two realizations. partsFor lifts the remote table onto the target for a
// link, so a key beside them would silently change that target's name, key or
// home for every link and for no local project.
func validateRemoteKeys(name string, remote map[string]any) []error {
	var errs []error
	for k := range remote {
		if k != "window" && k != "runtime" {
			errs = append(errs, fmt.Errorf("target %q: [target.remote] holds a window and a runtime realization; %q belongs on the target itself", name, k))
		}
	}
	return errs
}

// errNameKey refuses a project file that names itself. The file name is the
// project's name (decisions.md D80), and a key that could disagree with it
// would be ignored without a word.
var errNameKey = errors.New("name is not read: the file name is the project's name; delete the line")

// decodeProject is the text of a project file with the shared targets merged
// in. It is parsed once as tables; the typed decode of the file alone runs
// only where it is the answer - a local project with no shared targets - or
// where the merge failed, so an error in the file is still reported against
// its own lines.
func decodeProject(data []byte, shared []map[string]any) (revier.Project, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return revier.Project{}, err
	}
	if _, ok := raw["name"]; ok {
		return revier.Project{}, errNameKey
	}
	own := func() (revier.Project, error) {
		var p revier.Project
		_, err := toml.Decode(string(data), &p)
		return p, err
	}
	_, isLink := raw["remote"]
	ownTargets := tablesOf(raw["target"])
	if err := validateParts(ownTargets, isLink); err != nil {
		return revier.Project{}, err
	}
	if len(shared) == 0 && !isLink {
		return own()
	}
	declared := map[string]bool{}
	for _, t := range ownTargets {
		name, _ := t["name"].(string)
		declared[name] = true
	}
	raw["target"] = partsFor(mergeTargets(shared, ownTargets), declared, isLink)
	var merged revier.Project
	if err := recode(raw, &merged); err != nil {
		if p, ownErr := own(); ownErr != nil {
			return p, ownErr
		}
		return revier.Project{}, fmt.Errorf("with the shared targets of config.toml: %w", err)
	}
	return merged, nil
}

// A target carries a realization for a local project under [target.window]
// and [target.runtime], and one for a link under [target.remote.window] and
// [target.remote.runtime] (decisions.md D82). Both parts together are only
// ever written in config.toml, where one target serves every project: a
// project file is one kind or the other, and writes the part of its kind.
//
// partsFor is the targets of one project with the part of its kind taken as
// the realization. A shared target with no part of that kind is not a target
// of that project at all - one with no remote part is local-only and no link
// has it, one with no local part is a link's and no local project has it.
// declared names the targets the project file writes itself: those stay
// whatever they hold, so a target of its own with no realization is refused
// by Validate against its own file rather than disappearing.
func partsFor(targets []map[string]any, declared map[string]bool, isLink bool) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		flat := maps.Clone(t)
		remote, _ := flat["remote"].(map[string]any)
		delete(flat, "remote")
		if isLink {
			delete(flat, "window")
			delete(flat, "runtime")
			maps.Copy(flat, remote)
		}
		name, _ := flat["name"].(string)
		_, window := flat["window"]
		_, runtime := flat["runtime"]
		if !window && !runtime && !declared[name] {
			continue
		}
		out = append(out, flat)
	}
	return out
}

// validateParts refuses a project file that writes the part of the other
// kind: a realization nothing would ever read is a target that silently does
// nothing, which is exactly what loading catches.
func validateParts(targets []map[string]any, isLink bool) error {
	var errs []error
	for i, t := range targets {
		name, _ := t["name"].(string)
		if name == "" {
			name = fmt.Sprintf("%d", i+1)
		}
		if isLink {
			for _, k := range []string{"window", "runtime"} {
				if _, ok := t[k]; ok {
					errs = append(errs, fmt.Errorf("target %q: a link declares its realization under [target.remote.%s], because it reaches a project on another machine", name, k))
				}
			}
			if remote, ok := t["remote"].(map[string]any); ok {
				errs = append(errs, validateRemoteKeys(name, remote)...)
			}
			continue
		}
		if _, ok := t["remote"]; ok {
			errs = append(errs, fmt.Errorf("target %q: [target.remote] is for a link, and this project is local; declare [target.window] or [target.runtime]", name))
		}
	}
	return errors.Join(errs...)
}

// mergeTargets is the shared targets, each merged with the project's own of
// the same name, then the project's targets the shared ones do not name, in
// the project's order.
func mergeTargets(shared, own []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(shared)+len(own))
	at := map[string]int{}
	for _, t := range shared {
		name, _ := t["name"].(string)
		at[name] = len(out)
		out = append(out, t)
	}
	for _, t := range own {
		name, _ := t["name"].(string)
		if i, ok := at[name]; ok && name != "" {
			out[i] = mergeTable(out[i], t)
			continue
		}
		out = append(out, t)
	}
	return out
}

// mergeTable is base with over written onto it. Neither is changed: the
// shared tables are read by every project.
func mergeTable(base, over map[string]any) map[string]any {
	out := maps.Clone(base)
	for k, v := range over {
		b, bok := out[k].(map[string]any)
		o, ook := v.(map[string]any)
		if bok && ook {
			out[k] = mergeTable(b, o)
			continue
		}
		out[k] = v
	}
	return out
}

// tablesOf is a decoded array of tables. An array of inline tables decodes as
// []any, and is taken the same way.
func tablesOf(v any) []map[string]any {
	switch v := v.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, e := range v {
			if t, ok := e.(map[string]any); ok {
				out = append(out, t)
			}
		}
		return out
	}
	return nil
}

// recode decodes tables TOML already decoded into a typed value, by writing
// them out as TOML and reading that back.
func recode(tables any, into any) error {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(tables); err != nil {
		return err
	}
	_, err := toml.Decode(b.String(), into)
	return err
}

// sharedTargets is the shared targets of the config.toml under root.
func sharedTargets(root string) ([]map[string]any, error) {
	path := File(root)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg.Targets, nil
}
