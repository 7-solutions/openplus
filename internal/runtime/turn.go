package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/7solutions/openplus/internal/agent"
	"github.com/7solutions/openplus/internal/contextmgr"
	"github.com/7solutions/openplus/internal/provider"
)

// MemoryTopK bounds how many memory chunks are retrieved per turn.
const MemoryTopK = 5

// Turn is the assembled context for one turn: the system prompt the model will
// see, and the message history to send with it.
type Turn struct {
	System  string
	History []provider.Message
}

// AssembleContext builds the context for one turn (ADR-0008): it retrieves
// relevant memory, auto-loads relevant skills, budgets the result in priority
// order, and returns the system prompt plus the history to send.
//
// Retrieval failures are not fatal. Memory and skills are enrichment — losing
// them degrades the answer, whereas refusing the turn loses it entirely.
func (s *Session) AssembleContext(ctx context.Context, userMsg string, history []provider.Message) (Turn, error) {
	in := contextmgr.Input{System: s.SystemPrompt}

	// Retrieved memory (ADR-0003 hybrid search).
	if s.Memory != nil {
		results, err := s.Memory.Search(ctx, userMsg, MemoryTopK)
		if err == nil {
			for _, r := range results {
				in.Memory = append(in.Memory, r.Text)
			}
		}
	}

	// Auto-loaded skills (ADR-0002 BM25 + threshold). Skills are instructions,
	// so they belong with the task context rather than the memory pool.
	if s.Skills != nil {
		for _, sk := range s.Skills.AutoLoad(userMsg, MaxAutoSkills) {
			in.Memory = append(in.Memory, fmt.Sprintf("# Skill: %s\n%s", sk.Name, sk.Body))
		}
	}

	// The new user message plus the prior history are the retained recent
	// messages the budgeter may trim.
	in.Recent = append(append([]provider.Message{}, history...), userMessage(userMsg))

	out := s.Budgeter.Fit(in)

	return Turn{
		System:  renderSystem(out),
		History: out.Recent,
	}, nil
}

// Run assembles context and drives one agent loop to completion, returning the
// resulting history. When memory is configured the exchange is persisted so a
// later session can retrieve it.
//
// When Session.Goal is non-empty AND Session.Judge is non-nil, Run consults
// the judge after the agent loop returns (Change 0007 / T-440..T-445).
// MET stops; UNMET appends the judge's feedback to history and re-runs the
// agent loop. The loop is bounded by Session.MaxJudgeIterations (default
// DefaultMaxJudgeIterations = 3) so an unsatisfiable goal can't run forever.
func (s *Session) Run(ctx context.Context, userMsg string, history []provider.Message) ([]provider.Message, error) {
	if strings.TrimSpace(userMsg) == "" {
		return nil, fmt.Errorf("runtime: empty user message")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("runtime: %w", err)
	}

	turn, err := s.AssembleContext(ctx, userMsg, history)
	if err != nil {
		return nil, err
	}

	a := &agent.Agent{
		Provider:     s.Provider,
		Tools:        s.Tools,
		Gate:         s.Gate,
		OnEvent:      s.OnEvent,
		OnToolResult: s.OnToolResult,
	}

	maxIter := s.maxJudgeIterations()
	currentHistory := turn.History

	var final []provider.Message
	for round := 0; round < maxIter; round++ {
		hist, err := a.Run(ctx, turn.System, s.ToolSchemas, currentHistory)
		if err != nil {
			return hist, err
		}
		final = hist

		// No goal or no judge → preserve the pre-0007 single-shot behavior.
		// The agent's own tool-call count is what ends the loop.
		if s.Goal == "" || s.Judge == nil {
			break
		}

		verdict, err := s.Judge.Evaluate(ctx, s.Goal, hist)
		if err != nil {
			return hist, fmt.Errorf("runtime: judge: %w", err)
		}
		if verdict.Met {
			break
		}
		// UNMET: append feedback as a user message and loop again,
		// unless this was the last allowed round.
		if round == maxIter-1 {
			return final, fmt.Errorf("runtime: judge never returned MET after %d rounds; last feedback: %s",
				maxIter, strings.TrimSpace(verdict.Feedback))
		}
		feedback := strings.TrimSpace(verdict.Feedback)
		if feedback == "" {
			feedback = "(judge said UNMET with no feedback)"
		}
		currentHistory = append(currentHistory, provider.Message{
			Role: provider.RoleUser,
			Blocks: []provider.Block{
				{Kind: provider.BlockText, Text: "The goal is not met yet. Judge feedback:\n\n" + feedback},
			},
		})
	}

	s.persist(ctx, userMsg, final)
	return final, nil
}

// persist writes the exchange to memory. A write failure is deliberately
// non-fatal: losing a memory entry must not fail a turn the user already got
// value from.
func (s *Session) persist(ctx context.Context, userMsg string, history []provider.Message) {
	if s.Memory == nil {
		return
	}
	text := userMsg
	if reply := lastAssistantText(history); reply != "" {
		text = userMsg + "\n" + reply
	}
	_, _ = s.Memory.Write(ctx, text, "session")
}

// renderSystem flattens the budgeted sections into the single system string the
// provider adapters take, keeping ADR-0008's priority order visible in the
// prompt itself.
func renderSystem(out contextmgr.Output) string {
	var b strings.Builder
	b.WriteString(out.System)

	if out.Task != "" {
		b.WriteString("\n\n# Active task\n")
		b.WriteString(out.Task)
	}
	if out.Progress != "" {
		b.WriteString("\n\n# Progress\n")
		b.WriteString(out.Progress)
	}
	if out.Checkpoint != "" {
		b.WriteString("\n\n# Checkpoint\n")
		b.WriteString(out.Checkpoint)
	}
	if len(out.Memory) > 0 {
		b.WriteString("\n\n# Retrieved context\n")
		for _, m := range out.Memory {
			b.WriteString(m)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func userMessage(text string) provider.Message {
	return provider.Message{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{{Kind: provider.BlockText, Text: text}},
	}
}

// lastAssistantText returns the final assistant text in a history, used to give
// a persisted memory entry both sides of the exchange.
func lastAssistantText(history []provider.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != provider.RoleAssistant {
			continue
		}
		var parts []string
		for _, b := range history[i].Blocks {
			if b.Kind == provider.BlockText && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "")
		}
	}
	return ""
}
