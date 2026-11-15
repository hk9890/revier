package hosttest

import (
	"context"
	"fmt"
	"slices"

	"github.com/hk9890/revier/pkg/revier"
)

// FakeWindowRules is an in-memory WindowRules: a rules file a test sets up
// and reads back, so layer L2 drives `revier setup` with no wctl.
type FakeWindowRules struct {
	Rules []revier.WindowRule
	// ReadErr makes Read fail, for the error path.
	ReadErr error
}

// NewWindowRules returns a fake host holding rules.
func NewWindowRules(rules ...revier.WindowRule) *FakeWindowRules {
	return &FakeWindowRules{Rules: rules}
}

func (f *FakeWindowRules) Name() string { return "fake-rules" }

func (f *FakeWindowRules) Read(context.Context) ([]revier.WindowRule, error) {
	return slices.Clone(f.Rules), f.ReadErr
}

// FakeWindowRuleWriter is a FakeWindowRules that Add and Remove change.
type FakeWindowRuleWriter struct {
	*FakeWindowRules
	// FailAt makes the call with this number fail, from 1, before it changes
	// anything: a run that stopped half way. 0 fails none.
	FailAt int
	// Calls records every change, in order: "add 0" or "remove 2".
	Calls []string
}

// NewWindowRuleWriter returns a fake host holding rules that can be written.
func NewWindowRuleWriter(rules ...revier.WindowRule) *FakeWindowRuleWriter {
	return &FakeWindowRuleWriter{FakeWindowRules: NewWindowRules(rules...)}
}

func (f *FakeWindowRuleWriter) Add(_ context.Context, at int, rule revier.WindowRule) error {
	if err := f.call(fmt.Sprintf("add %d", at)); err != nil {
		return err
	}
	if at < 0 || at > len(f.Rules) {
		return fmt.Errorf("--at %d is past the end", at)
	}
	f.Rules = slices.Insert(f.Rules, at, rule)
	return nil
}

func (f *FakeWindowRuleWriter) Remove(_ context.Context, index int) error {
	if err := f.call(fmt.Sprintf("remove %d", index)); err != nil {
		return err
	}
	if index < 0 || index >= len(f.Rules) {
		return fmt.Errorf("no rule %d", index)
	}
	f.Rules = slices.Delete(f.Rules, index, index+1)
	return nil
}

func (f *FakeWindowRuleWriter) call(name string) error {
	f.Calls = append(f.Calls, name)
	if len(f.Calls) == f.FailAt {
		return fmt.Errorf("%s: refused", name)
	}
	return nil
}
