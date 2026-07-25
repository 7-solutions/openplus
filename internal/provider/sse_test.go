package provider

import (
	"context"
	"strings"
	"testing"
)

func TestReadSSE_MultiFrame(t *testing.T) {
	input := "event: content_block_delta\n" +
		"data: {\"partial\":\"hel\"}\n" +
		"\n" +
		"event: content_block_delta\n" +
		"data: {\"partial\":\"lo\"}\n" +
		"\n" +
		"event: message_stop\n" +
		"data: {}\n" +
		"\n"

	frames, errs := ReadSSE(context.Background(), strings.NewReader(input))

	var got []SSEFrame
	for f := range frames {
		got = append(got, f)
	}
	if err := <-errs; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 frames, got %d: %+v", len(got), got)
	}
	if got[0].Event != "content_block_delta" || got[0].Data != `{"partial":"hel"}` {
		t.Fatalf("frame 0 mismatch: %+v", got[0])
	}
	if got[2].Event != "message_stop" {
		t.Fatalf("frame 2 mismatch: %+v", got[2])
	}
}
