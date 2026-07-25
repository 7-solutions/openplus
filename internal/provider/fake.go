package provider

import "context"

// Fake is a deterministic, in-memory Provider for tests and for proving the
// agent loop without any network access or API key. It plays back a fixed
// script of Events per call, in order — one script entry per Stream call.
type Fake struct {
	Scripts [][]Event // Scripts[i] is played on the i-th call to Stream
	calls   int
}

func (f *Fake) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	i := f.calls
	f.calls++
	var script []Event
	if i < len(f.Scripts) {
		script = f.Scripts[i]
	} else {
		// Default: no tool calls, turn ends immediately.
		script = []Event{{Kind: EventTurnEnd}}
	}

	ch := make(chan Event, len(script))
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
