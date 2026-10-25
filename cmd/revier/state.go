package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hk9890/revier/internal/record"
	"github.com/hk9890/revier/internal/session"
	"github.com/hk9890/revier/internal/state"
	"github.com/hk9890/revier/pkg/revier"
)

const stateUsage = `revier state - what revier managed at a moment

usage:
  revier state [--at <time> | --session <id|name>] [--json]

  --at       the moment: RFC 3339, or YYYY-MM-DD HH:MM[:SS] in local time;
             now when not given
  --session  the moment a saved session was saved, and every difference
             between what was open then and what the session holds
  --json     the same as one JSON document

It reads the state record of this machine, $STATE/record, and asks no host.
It prints the current project, the project count, then for this machine and
each linked host the hosts, the targets, the instances and the agents. Then
it prints what revier did after the last survey of this machine, which no
survey has seen yet.

What a host held while it did not list, or while a linked host did not
answer, is printed as last seen and marked unknown: it is not known to be
gone. A machine with no survey line for six minutes is stale, since no
surface was there to see it: what it printed last is marked so. A moment
before the first record line is unknown.

Each difference --session names has one of these reasons:

  not asked by the save    a linked host a project close did not ask
  unreachable at the save  a host that did not list, or did not answer
  attached                 an instance attached by hand: a session has no
                           way to bring one back
  no conversation          an agent saved with no conversation, or the
                           agent of a tab, which a restore never resumes
  host unknown             a host whose state the record does not know then
  unexplained              none of these
`

// stateAt is the output of `revier state`: the state at a moment, and with
// --session how the session differs from it.
type stateAt struct {
	record.State
	Compare *comparedTo `json:"compare,omitempty"`
}

type comparedTo struct {
	Session     string              `json:"session"`
	Differences []record.Difference `json:"differences"`
}

// cmdState replays this machine's state record at a moment. It needs no host
// and no project file: the record is all it reads (decisions.md D151).
func cmdState(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("state", flag.ContinueOnError)
	at := fs.String("at", "", "the moment")
	ref := fs.String("session", "", "the saved session to compare")
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || (*at != "" && *ref != "") {
		return errors.New("usage: revier state [--at <time> | --session <id|name>] [--json]")
	}
	root, err := state.Root()
	if err != nil {
		return err
	}
	when := time.Now()
	if *at != "" {
		if when, err = parseMoment(*at); err != nil {
			return err
		}
	}
	var s session.Session
	if *ref != "" {
		if s, err = session.Load(root, *ref); err != nil {
			return err
		}
		when = s.At
	}
	lines, err := record.Read(root, when)
	if err != nil {
		return err
	}
	var st stateAt
	if *ref != "" {
		var diffs []record.Difference
		st.State, diffs = record.Compare(lines, s)
		st.Compare = &comparedTo{Session: s.ID, Differences: diffs}
	} else {
		st.State = record.Replay(lines, when)
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	printState(out, st)
	return nil
}

// parseMoment reads a moment as RFC 3339, or as a date and a time of day in
// local time.
func parseMoment(text string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t, nil
	}
	for _, layout := range []string{time.DateTime, "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--at %q: want RFC 3339 or YYYY-MM-DD HH:MM[:SS]", text)
}

func printState(out io.Writer, st stateAt) {
	w := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format+"\n", a...) }
	w("state at %s", st.At.Local().Format("2006-01-02 15:04:05 -07:00"))
	if st.Current != "" {
		w("current project: %s", st.Current)
	}
	w("projects: %d, %d open", len(st.Projects), openProjects(st.State))
	for _, m := range st.Machines {
		w("")
		printMachine(w, m)
	}
	if len(st.Since) > 0 {
		w("")
		w("done since the last survey, not yet seen by one:")
		for _, l := range st.Since {
			w("  %s  %s", l.Time.Local().Format(time.TimeOnly), describeLine(l))
		}
	}
	if st.Compare != nil {
		w("")
		w("session %s:", st.Compare.Session)
		if len(st.Compare.Differences) == 0 {
			w("  holds what was open")
		}
		for _, d := range st.Compare.Differences {
			w("  in %s, not in %s: %s: %s", d.In, other(d.In), describeDifference(d), d.Why)
		}
	}
}

func printMachine(w func(string, ...any), m record.Machine) {
	name := m.Machine
	if name == "" {
		name = "this machine"
	}
	switch {
	case m.Seen.IsZero():
		w("%s  unknown: %s", name, m.Unknown)
		return
	case !m.StaleSince.IsZero():
		w("%s  seen %s, no survey since: unknown", name, m.Seen.Local().Format(time.TimeOnly))
	case m.Unknown != "":
		w("%s  seen %s, unreachable: %s", name, m.Seen.Local().Format(time.TimeOnly), m.Unknown)
	default:
		w("%s  seen %s", name, m.Seen.Local().Format(time.TimeOnly))
	}
	var hosts []string
	for _, h := range m.Hosts {
		if h.Unknown != "" {
			hosts = append(hosts, fmt.Sprintf("%s unknown: %s", h.Host, h.Unknown))
		} else {
			hosts = append(hosts, h.Host+" listed")
		}
	}
	if len(hosts) > 0 {
		w("  hosts: %s", strings.Join(hosts, "; "))
	}

	titles := map[string]string{}
	for _, i := range m.Instances {
		titles[i.Host+"\x00"+i.Ref] = i.Title
	}
	instance := func(host, ref string) string {
		if title := titles[host+"\x00"+ref]; title != "" {
			return fmt.Sprintf("%s %s %q", host, ref, title)
		}
		return host + " " + ref
	}
	held := map[string]bool{}
	targets := map[revier.ProjectName][]record.Target{}
	agents := map[revier.ProjectName][]record.Agent{}
	for _, t := range m.Targets {
		targets[t.Project] = append(targets[t.Project], t)
		held[t.Host+"\x00"+t.Ref] = true
	}
	for _, a := range m.Agents {
		agents[a.Project] = append(agents[a.Project], a)
	}
	projects := slices.Sorted(maps.Keys(targets))
	for p := range agents {
		if !slices.Contains(projects, p) {
			projects = append(projects, p)
		}
	}
	for p := range m.UnknownLinks {
		if !slices.Contains(projects, p) {
			projects = append(projects, p)
		}
	}
	slices.Sort(projects)
	for _, p := range projects {
		if why := m.UnknownLinks[p]; why != "" {
			w("  %s  unknown: %s", p, why)
		} else {
			w("  %s", p)
		}
		for _, t := range targets[p] {
			name := string(t.Target)
			if t.Attached {
				name = "(attached)"
			}
			w("    %s  %s%s", name, instance(t.Host, t.Ref), unknownMark(m, t.Host, p))
		}
		for _, a := range agents[p] {
			w("    agent  %s%s", describeAgent(a), unknownMark(m, a.Host, p))
		}
	}
	var loose []string
	for _, i := range m.Instances {
		if !held[i.Host+"\x00"+i.Ref] {
			loose = append(loose, instance(i.Host, i.Ref))
		}
	}
	if len(loose) > 0 {
		w("  instances no project holds: %s", strings.Join(loose, ", "))
	}
}

// unknownMark says that what a line prints is last seen, of a host that did
// not list or a link with no answer.
func unknownMark(m record.Machine, host string, project revier.ProjectName) string {
	if m.Unknown != "" || !m.StaleSince.IsZero() || m.UnknownLinks[project] != "" {
		return ""
	}
	for _, h := range m.Hosts {
		if h.Host == host && h.Unknown != "" {
			return "  (last seen; " + host + " did not list)"
		}
	}
	return ""
}

func describeAgent(a record.Agent) string {
	parts := []string{}
	if a.Host != "" {
		parts = append(parts, fmt.Sprintf("%s %s panel %s", a.Host, a.Ref, a.Panel))
	}
	for _, s := range []string{a.Harness, a.Agent, string(a.Session), a.Dir} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "  ")
}

func describeLine(l record.Line) string {
	parts := []string{l.Kind}
	where := string(l.Project)
	if l.Target != "" {
		where += ":" + string(l.Target)
	}
	if where != "" {
		parts = append(parts, where)
	}
	if l.Machine != "" {
		parts = append(parts, "on "+l.Machine)
	}
	if l.Launched {
		parts = append(parts, "launched")
	}
	if l.Host != "" {
		parts = append(parts, l.Host+" "+l.Ref)
	}
	if l.Saved != "" {
		parts = append(parts, "session "+l.Saved)
	}
	if l.Err != "" {
		parts = append(parts, "err: "+l.Err)
	}
	return strings.Join(parts, " ")
}

func describeDifference(d record.Difference) string {
	name := d.Machine
	if name == "" {
		name = "this machine"
	}
	switch {
	case d.Target != nil && d.Target.Attached:
		return fmt.Sprintf("%s %s attached %s %s", name, d.Target.Project, d.Target.Host, d.Target.Ref)
	case d.Target != nil:
		return fmt.Sprintf("%s %s:%s", name, d.Target.Project, d.Target.Target)
	default:
		return fmt.Sprintf("%s %s agent %s", name, d.Agent.Project, describeAgent(*d.Agent))
	}
}

func other(in string) string {
	if in == "state" {
		return "session"
	}
	return "state"
}

// openProjects is how many projects have a target open on some machine.
func openProjects(st record.State) int {
	open := map[revier.ProjectName]bool{}
	for _, m := range st.Machines {
		for _, t := range m.Targets {
			if !t.Attached {
				open[t.Project] = true
			}
		}
	}
	return len(open)
}
