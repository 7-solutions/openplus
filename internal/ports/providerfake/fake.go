// Package portsfake provides a scripted Provider for tests and for proving the
// agent loop without any network access or API key (change 0018; formerly
// provider.Fake in internal/provider/fake.go). It plays back a fixed script of
// Events per call, in order — one script entry per Stream call. The call
// counter is atomic: a single Fake may be shared across concurrent goroutines
// (e.g. subagent fanout).
package portsfake

import (
	"context"
	"sync/atomic"

	"github.com/7-solutions/openplus/internal/ports"
)

// Fake is a deterministic, in-memory ports.Provider for tests.
type Fake struct {
	Scripts [][]ports.Event // Scripts[i] is played on the i-th call to Stream
	calls   atomic.Int32
}

func (f *Fake) Stream(ctx context.Context, _ ports.Request) (<-chan ports.Event, error) {
	i := int(f.calls.Add(1)) - 1
	var script []ports.Event
	if i < len(f.Scripts) {
		script = f.Scripts[i]
	} else {
		// Default: no tool calls, turn ends immediately.
		script = []ports.Event{{Kind: ports.EventTurnEnd}}
	}

	ch := make(chan ports.Event, len(script))
	go func() {
		defer close(ch)
		for _, ev := range script {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

// Compile-time assertion: Fake implements the canonical Provider port.
var _ ports.Provider = (*Fake)(nil)
