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
// same name (decisions.md D58). The merge is on the tables as TOML decoded
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
	}
	return errors.Join(errs...)
}

// decodeProject reads a project file with the shared targets merged in. The
// file is decoded as it is first, so an error in it is reported against its
// own lines; the merged tables are decoded again only once both halves are
// known to decode.
func decodeProject(path string, shared []map[string]any) (revier.Project, error) {
	var p revier.Project
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return p, err
	}
	// A link's home is the ssh pane onto the host, and the project file
	// there already has the host's shared targets.
	if len(shared) == 0 || p.Remote != nil {
		return p, nil
	}
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return p, err
	}
	raw["target"] = mergeTargets(shared, tablesOf(raw["target"]))
	var merged revier.Project
	if err := recode(raw, &merged); err != nil {
		return p, fmt.Errorf("with the shared targets of config.toml: %w", err)
	}
	return merged, nil
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
