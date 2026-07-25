// Package provider holds the concrete model adapters and adapter-only helpers.
//
// After change 0018, every provider-neutral type (Request, Event, Message,
// Block, BlockKind, Role, ToolSchema, ToolCall, Usage, EventKind) and the
// Provider interface live in internal/ports — that is the canonical surface
// the core depends on. This package retains only what an adapter author
// needs: the anthropic and openaicompat backends, the prefix-select registry,
// and the shared SSE frame reader in this file. Core packages must not import
// here; internal/ports/leak_guard_test.go enforces that.
package provider

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// SSEFrame is one decoded "text/event-stream" frame: an optional event name
// and its (possibly multi-line) data payload, joined with "\n" per spec.
type SSEFrame struct {
	Event string
	Data  string
}

// ReadSSE reads Server-Sent Events from r, sending each decoded frame on the
// returned channel until r is exhausted, ctx is cancelled, or an error
// occurs. The channel is always closed on return. A frame is emitted on the
// blank line that terminates it, per the SSE spec.
func ReadSSE(ctx context.Context, r io.Reader) (<-chan SSEFrame, <-chan error) {
	frames := make(chan SSEFrame)
	errs := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errs)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // allow large tool-arg lines

		var event string
		var data []string

		flush := func() {
			if len(data) == 0 && event == "" {
				return
			}
			select {
			case frames <- SSEFrame{Event: event, Data: strings.Join(data, "\n")}:
			case <-ctx.Done():
			}
			event = ""
			data = nil
		}

		for scanner.Scan() {
			if ctx.Err() != nil {
				errs <- ctx.Err()
				return
			}
			line := scanner.Text()
			switch {
			case line == "":
				flush()
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			case strings.HasPrefix(line, ":"):
				// comment / keep-alive line — ignore per SSE spec
			default:
				// ignore unrecognized fields (id:, retry:) for this scaffold
			}
		}
		flush() // final frame if stream ends without a trailing blank line

		if err := scanner.Err(); err != nil {
			errs <- fmt.Errorf("sse: scan: %w", err)
		}
	}()

	return frames, errs
}
