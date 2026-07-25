// Package improve implements the self-improvement loop (M9): /dream extracts
// durable facts from session traces and prunes stale memory, and /distill mines
// repeated tool sequences into skill, subagent, and command scaffolds.
//
// Both halves are deliberately conservative. Dream summarizes with no tools
// attached, so it can never act on the trace it is reading. Distill only writes
// scaffolds — it never overwrites an existing file, so a generated artifact can
// always be edited by hand without a later run clobbering it.
package improve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7-solutions/openplus/internal/ports"
)

// errDreamBoom is a sentinel used by tests to drive the provider-error path.
var errDreamBoom = errors.New("dream provider failure")

// dreamSystemPrompt asks for durable, reusable facts in a parseable shape.
const dreamSystemPrompt = `You are reviewing a session transcript to extract durable memory.

Return only facts worth remembering for future sessions: decisions made, conventions
established, corrections the user gave, and non-obvious constraints discovered.

Format: one fact per line, each starting with "- ". No preamble, no summary.
Skip anything transient (build output, file listings, what a command printed) and
anything already obvious from reading the code.`

// Dreamer extracts durable facts from a session trace with a summarizing model.
type Dreamer struct {
	// Provider is the model backend used for extraction.
	Provider ports.Provider
	// Model is the extraction model id ("<provider>/<model>").
	Model string
}

// Extract reads a trace and returns the durable facts worth persisting. An empty
// trace yields nothing without consulting the model.
func (d Dreamer) Extract(ctx context.Context, trace []ports.Message) ([]string, error) {
	if len(trace) == 0 {
		return nil, nil
	}
	if d.Provider == nil {
		return nil, fmt.Errorf("improve: dreamer has no provider configured")
	}

	req := ports.Request{
		Model:  d.Model,
		System: dreamSystemPrompt,
		Messages: []ports.Message{{
			Role: ports.RoleUser,
			Blocks: []ports.Block{{
				Kind: ports.BlockText,
				Text: "TRANSCRIPT:\n" + flattenTrace(trace),
			}},
		}},
		// No tools: the dreamer reads and summarizes, it does not act.
	}

	events, err := d.Provider.Stream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("improve: dream stream: %w", err)
	}

	var reply strings.Builder
	for ev := range events {
		switch ev.Kind {
		case ports.EventTextDelta:
			reply.WriteString(ev.Text)
		case ports.EventError:
			return nil, fmt.Errorf("improve: dream: %w", ev.Err)
		}
	}

	return parseBullets(reply.String()), nil
}

// parseBullets pulls "- fact" lines out of a reply, dropping prose and blanks.
func parseBullets(reply string) []string {
	var out []string
	for line := range strings.SplitSeq(reply, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") && line != "-" {
			continue
		}
		fact := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if fact == "" {
			continue
		}
		out = append(out, fact)
	}
	return out
}

// flattenTrace renders a message history as a plain transcript.
func flattenTrace(msgs []ports.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		for _, blk := range m.Blocks {
			switch blk.Kind {
			case ports.BlockText, ports.BlockThinking:
				fmt.Fprintf(&b, "%s: %s\n", m.Role, blk.Text)
			case ports.BlockToolCall:
				fmt.Fprintf(&b, "%s: called %s(%s)\n", m.Role, blk.ToolName, blk.ToolInput)
			case ports.BlockToolResult:
				fmt.Fprintf(&b, "tool result: %s\n", blk.ToolResultText)
			}
		}
	}
	return b.String()
}

// --- stale-entry pruning ---

// Entry is one stored memory line with the metadata pruning needs.
type Entry struct {
	Text      string
	UpdatedAt time.Time
	// Pinned entries are never pruned, however old they get.
	Pinned bool
}

// PrunePolicy configures pruning. A zero MaxAge disables age-based pruning
// (deduplication still applies), so a misconfigured policy cannot silently wipe
// a project's memory.
type PrunePolicy struct {
	MaxAge time.Duration
}

// Prune splits entries into those to keep and those to drop. Entries are dropped
// when they exceed MaxAge, or when a newer entry says the same thing
// (case-insensitive). Pinned entries always survive. Input order is preserved
// among the kept entries.
func Prune(entries []Entry, now time.Time, policy PrunePolicy) (kept, pruned []Entry) {
	// Find, for each normalized fact, the newest timestamp claiming it — that is
	// the copy allowed to survive.
	newest := map[string]time.Time{}
	for _, e := range entries {
		key := normalizeFact(e.Text)
		if t, ok := newest[key]; !ok || e.UpdatedAt.After(t) {
			newest[key] = e.UpdatedAt
		}
	}

	seen := map[string]bool{}
	for _, e := range entries {
		if e.Pinned {
			kept = append(kept, e)
			continue
		}
		if policy.MaxAge > 0 && now.Sub(e.UpdatedAt) > policy.MaxAge {
			pruned = append(pruned, e)
			continue
		}
		key := normalizeFact(e.Text)
		// Keep only the newest copy of a duplicated fact, and only once.
		if seen[key] || e.UpdatedAt.Before(newest[key]) {
			pruned = append(pruned, e)
			continue
		}
		seen[key] = true
		kept = append(kept, e)
	}
	return kept, pruned
}

// normalizeFact collapses case and whitespace so restatements of the same fact
// deduplicate.
func normalizeFact(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
