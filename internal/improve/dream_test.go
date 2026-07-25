package improve

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/7-solutions/openplus/internal/ports"
	portsfake "github.com/7-solutions/openplus/internal/ports/providerfake"
)

// extractorSays builds a Fake provider that streams one extraction reply.
func extractorSays(reply string) *portsfake.Fake {
	return &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventTextDelta, Text: reply},
		{Kind: ports.EventTurnEnd},
	}}}
}

func trace(texts ...string) []ports.Message {
	msgs := make([]ports.Message, 0, len(texts))
	for _, tx := range texts {
		msgs = append(msgs, ports.Message{
			Role:   ports.RoleAssistant,
			Blocks: []ports.Block{{Kind: ports.BlockText, Text: tx}},
		})
	}
	return msgs
}

func TestDreamExtractsFactsFromTrace(t *testing.T) {
	d := Dreamer{Provider: extractorSays(
		"- the build is cgo-free by policy\n- tests run with CGO_ENABLED=0\n")}

	got, err := d.Extract(context.Background(), trace("we fixed the build", "CGO must stay off"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("facts = %v, want 2", got)
	}
	if !strings.Contains(got[0], "cgo-free") {
		t.Errorf("facts[0] = %q", got[0])
	}
	// bullet markers must be stripped
	for _, f := range got {
		if strings.HasPrefix(f, "-") {
			t.Errorf("fact still has its bullet marker: %q", f)
		}
	}
}

func TestDreamIgnoresBlankAndNonBulletLines(t *testing.T) {
	d := Dreamer{Provider: extractorSays(
		"Here is what I learned:\n\n- a real fact\n\n   \n- another fact\nthanks!\n")}
	got, err := d.Extract(context.Background(), trace("work"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("facts = %v, want exactly the 2 bullets", got)
	}
}

func TestDreamEmptyTraceExtractsNothing(t *testing.T) {
	// No trace means nothing to learn; the model must not be consulted.
	d := Dreamer{Provider: extractorSays("- should never be asked")}
	got, err := d.Extract(context.Background(), nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("facts = %v, want none", got)
	}
}

func TestDreamNoProviderErrors(t *testing.T) {
	if _, err := (Dreamer{}).Extract(context.Background(), trace("x")); err == nil {
		t.Fatal("expected an error with no provider configured")
	}
}

func TestDreamProviderErrorPropagates(t *testing.T) {
	d := Dreamer{Provider: &portsfake.Fake{Scripts: [][]ports.Event{{
		{Kind: ports.EventError, Err: errDreamBoom},
	}}}}
	if _, err := d.Extract(context.Background(), trace("x")); err == nil {
		t.Fatal("expected the provider error to propagate")
	}
}

func TestDreamSendsNoTools(t *testing.T) {
	// The dreamer summarizes; it must not be able to act.
	rec := &recordingProvider{reply: "- fact"}
	d := Dreamer{Provider: rec, Model: "anthropic/dream-model"}
	if _, err := d.Extract(context.Background(), trace("work")); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if rec.gotTools != 0 {
		t.Errorf("dreamer was given %d tools, want 0", rec.gotTools)
	}
	if rec.gotModel != "anthropic/dream-model" {
		t.Errorf("model = %q", rec.gotModel)
	}
}

// --- stale-entry pruning ---

func TestPruneDropsEntriesPastMaxAge(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Text: "fresh", UpdatedAt: now.Add(-24 * time.Hour)},
		{Text: "stale", UpdatedAt: now.Add(-90 * 24 * time.Hour)},
	}
	kept, pruned := Prune(entries, now, PrunePolicy{MaxAge: 30 * 24 * time.Hour})
	if len(kept) != 1 || kept[0].Text != "fresh" {
		t.Fatalf("kept = %+v, want only fresh", kept)
	}
	if len(pruned) != 1 || pruned[0].Text != "stale" {
		t.Fatalf("pruned = %+v, want only stale", pruned)
	}
}

func TestPruneKeepsPinnedRegardlessOfAge(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{Text: "ancient but pinned", UpdatedAt: now.Add(-5 * 365 * 24 * time.Hour), Pinned: true},
	}
	kept, pruned := Prune(entries, now, PrunePolicy{MaxAge: time.Hour})
	if len(kept) != 1 {
		t.Fatalf("pinned entry was pruned: kept=%+v pruned=%+v", kept, pruned)
	}
	if len(pruned) != 0 {
		t.Fatalf("pruned = %+v, want none", pruned)
	}
}

func TestPruneDeduplicatesKeepingNewest(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{Text: "the build is cgo-free", UpdatedAt: now.Add(-10 * time.Hour)},
		{Text: "The Build Is Cgo-Free", UpdatedAt: now.Add(-1 * time.Hour)}, // same fact, newer
		{Text: "something else", UpdatedAt: now},
	}
	kept, pruned := Prune(entries, now, PrunePolicy{MaxAge: 365 * 24 * time.Hour})
	if len(kept) != 2 {
		t.Fatalf("kept = %+v, want 2 (duplicate collapsed)", kept)
	}
	if len(pruned) != 1 {
		t.Fatalf("pruned = %+v, want the older duplicate", pruned)
	}
	// the surviving duplicate must be the newer one
	for _, e := range kept {
		if strings.EqualFold(e.Text, "the build is cgo-free") && !e.UpdatedAt.Equal(now.Add(-1*time.Hour)) {
			t.Errorf("kept the older duplicate: %+v", e)
		}
	}
}

func TestPruneZeroMaxAgeKeepsEverything(t *testing.T) {
	now := time.Now()
	entries := []Entry{{Text: "very old", UpdatedAt: now.Add(-10 * 365 * 24 * time.Hour)}}
	kept, pruned := Prune(entries, now, PrunePolicy{})
	if len(kept) != 1 || len(pruned) != 0 {
		t.Fatalf("zero MaxAge must disable age pruning: kept=%+v pruned=%+v", kept, pruned)
	}
}

func TestPruneEmptyInput(t *testing.T) {
	kept, pruned := Prune(nil, time.Now(), PrunePolicy{MaxAge: time.Hour})
	if len(kept) != 0 || len(pruned) != 0 {
		t.Fatalf("kept=%+v pruned=%+v, want both empty", kept, pruned)
	}
}

func TestPrunePreservesOrder(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{Text: "first", UpdatedAt: now},
		{Text: "second", UpdatedAt: now},
		{Text: "third", UpdatedAt: now},
	}
	kept, _ := Prune(entries, now, PrunePolicy{MaxAge: time.Hour})
	for i, want := range []string{"first", "second", "third"} {
		if kept[i].Text != want {
			t.Fatalf("kept[%d] = %q, want %q (order must be stable)", i, kept[i].Text, want)
		}
	}
}

// recordingProvider captures what the improver sends.
type recordingProvider struct {
	reply     string
	gotModel  string
	gotSystem string
	gotUser   string
	gotTools  int
}

func (r *recordingProvider) Stream(ctx context.Context, req ports.Request) (<-chan ports.Event, error) {
	r.gotModel = req.Model
	r.gotSystem = req.System
	r.gotTools = len(req.Tools)
	var b strings.Builder
	for _, m := range req.Messages {
		for _, blk := range m.Blocks {
			b.WriteString(blk.Text)
			b.WriteByte(' ')
		}
	}
	r.gotUser = b.String()

	ch := make(chan ports.Event, 2)
	ch <- ports.Event{Kind: ports.EventTextDelta, Text: r.reply}
	ch <- ports.Event{Kind: ports.EventTurnEnd}
	close(ch)
	return ch, nil
}
