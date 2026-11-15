package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hk9890/revier/internal/adapter/hc"
	"github.com/hk9890/revier/pkg/revier"
)

// cmdHcAttach is what a kitty window over hc runs: hc's attach of one
// terminal, held once its program ends (decisions.md D187). The window sets
// the core's socket as HC_SOCKET and names hc by its path. It has no
// deadline: the attach lasts as long as the window.
func cmdHcAttach(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: revier %s <hc> <terminal>", hc.AttachCmd)
	}
	return (&hc.Host{}).Attach(context.Background(), args[0], revier.PanelID(args[1]), os.Stdin, os.Stdout)
}
