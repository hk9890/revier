package hosttest

import (
	"context"
	"fmt"
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// The paths the fake names its files by.
const (
	FakeUserRules = "rules.json"
	FakeOwnRules  = "rules.d/revier.json"
)

// FakeWindowRules is an in-memory WindowRules: the user's rules file, the
// files of other programs, and revier's own, which a test sets up and reads
// back, so layer L2 drives `revier setup` with no wctl.
type FakeWindowRules struct {
	User []revier.WindowRule
	// Earlier are the files of other programs the host tries before
	// revier's.
	Earlier []revier.WindowRuleFile
	// Own is revier's file, nil when there is none.
	Own []revier.WindowRule
	// OwnBroken makes revier's file one the host would not load.
	OwnBroken string
	// ReadErr makes Read fail, for the error path.
	ReadErr error
}

// NewWindowRules returns a fake host whose user's file holds rules, and
// with no file of revier's.
func NewWindowRules(user ...revier.WindowRule) *FakeWindowRules {
	return &FakeWindowRules{User: user}
}

func (f *FakeWindowRules) Name() string { return "fake-rules" }

func (f *FakeWindowRules) Read(context.Context) (revier.WindowRuleSet, error) {
	set := revier.WindowRuleSet{
		User:      revier.WindowRuleFile{Path: FakeUserRules, Rules: slices.Clone(f.User)},
		Earlier:   slices.Clone(f.Earlier),
		Own:       revier.WindowRuleFile{Path: FakeOwnRules},
		OwnExists: f.Own != nil || f.OwnBroken != "",
		OwnBroken: f.OwnBroken,
	}
	if f.OwnBroken == "" {
		set.Own.Rules = slices.Clone(f.Own)
	}
	return set, f.ReadErr
}

// FakeWindowRuleWriter is a FakeWindowRules that its three writes change.
type FakeWindowRuleWriter struct {
	*FakeWindowRules
	// FailAt makes the call with this number fail, from 1, before it changes
	// anything: a run that stopped half way. 0 fails none.
	FailAt int
	// Calls records every change, in order: "remove user 2", "write own",
	// "remove own".
	Calls []string
}

// NewWindowRuleWriter returns a fake host whose user's file holds rules,
// and which can be written.
func NewWindowRuleWriter(user ...revier.WindowRule) *FakeWindowRuleWriter {
	return &FakeWindowRuleWriter{FakeWindowRules: NewWindowRules(user...)}
}

func (f *FakeWindowRuleWriter) RemoveUser(_ context.Context, index int) error {
	if err := f.call(fmt.Sprintf("remove user %d", index)); err != nil {
		return err
	}
	if index < 0 || index >= len(f.User) {
		return fmt.Errorf("no rule %d", index)
	}
	f.User = slices.Delete(f.User, index, index+1)
	return nil
}

func (f *FakeWindowRuleWriter) WriteOwn(_ context.Context, rules []revier.WindowRule) error {
	if err := f.call("write own"); err != nil {
		return err
	}
	f.Own, f.OwnBroken = append([]revier.WindowRule{}, rules...), ""
	return nil
}

func (f *FakeWindowRuleWriter) RemoveOwn(context.Context) error {
	if err := f.call("remove own"); err != nil {
		return err
	}
	f.Own, f.OwnBroken = nil, ""
	return nil
}

func (f *FakeWindowRuleWriter) call(name string) error {
	f.Calls = append(f.Calls, name)
	if len(f.Calls) == f.FailAt {
		return fmt.Errorf("%s: refused", name)
	}
	return nil
}
