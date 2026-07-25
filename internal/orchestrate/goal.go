package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7-solutions/openplus/internal/ports"
)

// errJudgeBoom is a sentinel used by tests to drive the provider-error path.
var errJudgeBoom = errors.New("judge provider failure")

// judgeSystemPrompt instructs the judge to answer in a machine-readable form.
// The judge is deliberately independent of the working agent: its own model, no
// tools, and no ability to act — it only renders a verdict.
const judgeSystemPrompt = `You are an independent goal judge. You are given a GOAL and a transcript of an agent's work.
Decide whether the goal has genuinely been achieved by the work shown.

Answer with exactly one line, starting with one of:
MET: <one-sentence justification>
UNMET: <what is still missing>

Be strict. Claims of success without evidence in the transcript are UNMET.`

// Verdict is the judge's decision about whether the agent may stop.
type Verdict struct {
	// Met reports whether the goal is satisfied. Only a clear approval sets
	// this true — anything ambiguous keeps the agent working.
	Met bool
	// Feedback is the judge's reasoning, fed back to the agent when Met is
	// false so the next turn knows what is missing.
	Feedback string
}

// Judge evaluates a goal / stop condition with an independent model
// (ADR-0006, orchestration spec "Goal / stop condition"). The agent must clear
// the judge before it is allowed to stop.
type Judge struct {
	// Provider is the judge's model backend — independent of the agent's.
	Provider ports.Provider
	// Model is the judge model id ("<provider>/<model>").
	Model string
}

// Evaluate asks the judge whether goal is satisfied by the work in history.
// An empty goal means no stop condition is configured, so stopping is always
// allowed and the judge is not consulted.
func (j Judge) Evaluate(ctx context.Context, goal string, history []ports.Message) (Verdict, error) {
	if strings.TrimSpace(goal) == "" {
		return Verdict{Met: true}, nil
	}
	if j.Provider == nil {
		return Verdict{}, fmt.Errorf("orchestrate: judge has no provider configured")
	}

	req := ports.Request{
		Model:  j.Model,
		System: judgeSystemPrompt,
		Messages: []ports.Message{{
			Role: ports.RoleUser,
			Blocks: []ports.Block{{
				Kind: ports.BlockText,
				Text: fmt.Sprintf("GOAL:\n%s\n\nTRANSCRIPT:\n%s", goal, flattenHistory(history)),
			}},
		}},
		// No tools: the judge decides, it does not act.
	}

	events, err := j.Provider.Stream(ctx, req)
	if err != nil {
		return Verdict{}, fmt.Errorf("orchestrate: judge stream: %w", err)
	}

	var reply strings.Builder
	for ev := range events {
		switch ev.Kind {
		case ports.EventTextDelta:
			reply.WriteString(ev.Text)
		case ports.EventError:
			return Verdict{}, fmt.Errorf("orchestrate: judge: %w", ev.Err)
		}
	}

	return parseVerdict(reply.String()), nil
}

// parseVerdict reads the judge's reply. Only an explicit MET authorizes
// stopping; an unparseable answer is treated as UNMET so the agent keeps working
// rather than stopping on a coin flip.
func parseVerdict(reply string) Verdict {
	trimmed := strings.TrimSpace(reply)
	lower := strings.ToLower(trimmed)

	switch {
	case strings.HasPrefix(lower, "unmet"):
		return Verdict{Met: false, Feedback: stripPrefix(trimmed, len("unmet"))}
	case strings.HasPrefix(lower, "met"):
		return Verdict{Met: true, Feedback: stripPrefix(trimmed, len("met"))}
	default:
		// Ambiguous — do not authorize a stop; hand the raw text back so the
		// agent (and the user) can see what the judge actually said.
		return Verdict{Met: false, Feedback: trimmed}
	}
}

// stripPrefix drops a verdict keyword and its separator, returning the reason.
func stripPrefix(s string, n int) string {
	rest := strings.TrimSpace(s[n:])
	return strings.TrimSpace(strings.TrimPrefix(rest, ":"))
}

// flattenHistory renders a message history as a plain transcript for the judge.
func flattenHistory(msgs []ports.Message) string {
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
