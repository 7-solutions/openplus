package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
)

// Prompter implements policy.Prompter for the TUI. On Ask it pushes a promptMsg
// into the program (via send) and blocks on the answer channel until the user
// presses y/n, or until ctx is done (the forced-ask timeout).
type Prompter struct {
	send   func(tea.Msg) // usually program.Send
	answer <-chan bool   // replies from the model's answerPrompt
}

// NewPrompter builds a Prompter. send is typically the *tea.Program's Send;
// answer is the channel shared with the model (via WithAnswer).
func NewPrompter(send func(tea.Msg), answer <-chan bool) *Prompter {
	return &Prompter{send: send, answer: answer}
}

// Ask surfaces the call as a prompt and blocks for the user's decision.
func (p *Prompter) Ask(ctx context.Context, call provider.ToolCall) (bool, error) {
	p.send(promptMsg{call: call})
	select {
	case approved := <-p.answer:
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// compile-time: *Prompter satisfies policy.Prompter.
var _ policy.Prompter = (*Prompter)(nil)

// SendNoOp is a send function that discards messages — useful for tests that
// only exercise the answer/timeout path.
func SendNoOp() func(tea.Msg) {
	return func(tea.Msg) {}
}
