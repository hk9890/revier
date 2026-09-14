package sway

import (
	"bytes"
	"encoding/json"

	"github.com/hk9890/revier/pkg/revier"
)

// Decode exposes the tree parser to the L3 tests.
func Decode(raw []byte) ([]revier.Instance, error) { return (&Host{}).decode(raw) }

// DecodeEvents parses a recorded subscribe stream, for the L3 tests.
func DecodeEvents(raw []byte) ([]revier.WindowEvent, error) {
	h := &Host{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var out []revier.WindowEvent
	for dec.More() {
		var ev event
		if err := dec.Decode(&ev); err != nil {
			return nil, err
		}
		if kind, ok := kindOf(ev.Change); ok {
			out = append(out, revier.WindowEvent{Kind: kind, Instance: h.instance(ev.Container)})
		}
	}
	return out, nil
}
