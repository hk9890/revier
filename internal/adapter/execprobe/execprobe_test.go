package execprobe_test

import (
	"testing"

	"github.com/hk9890/revier/internal/adapter/execprobe"
	"github.com/hk9890/revier/pkg/revier"
)

func TestMatchIsTheHarnessCommand(t *testing.T) {
	p := execprobe.New("aider", "/nonexistent")
	if !p.Match(revier.Panel{Command: []string{"/usr/bin/aider", "--model", "x"}}) {
		t.Error("a panel running aider should match")
	}
	if p.Match(revier.Panel{Command: []string{"claude"}}) {
		t.Error("a claude panel must not cost the aider probe a process")
	}
	if p.Match(revier.Panel{Command: []string{"less", "notes/aider"}}) {
		t.Error("a pager reading a file called aider is not aider")
	}
}
