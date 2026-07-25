// Package tui is the Bubble Tea front-end (ADR-0001, T-030/T-031). It renders
// streamed Events and tool results (including edit diffs) and drives the
// permission prompt when the policy gate returns Ask. The agent runs in a
// goroutine; its OnEvent/OnToolResult callbacks push messages into the program
// via program.Send, and the Prompter blocks its goroutine on an answer channel
// until the user responds.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/7solutions/openplus/internal/agent"
	"github.com/7solutions/openplus/internal/provider"
)

var errBoom = errors.New("boom")

// StreamMsg wraps one streamed provider.Event (sent via program.Send).
type StreamMsg provider.Event

// ToolResultMsg carries a tool call + its neutral result block (sent via
// program.Send from the agent's OnToolResult hook).
type ToolResultMsg struct {
	Call   provider.ToolCall
	Result provider.Block
}

// promptMsg asks the user to approve a tool call (sent by the Prompter).
type promptMsg struct {
	call provider.ToolCall
}

// Model is the Bubble Tea model.
type Model struct {
	agent   *agent.Agent
	system  string
	tools   []provider.ToolSchema
	history []provider.Message

	input   textarea.Model
	log     []string        // flushed, rendered lines
	cur     strings.Builder // assistant text accumulated since last flush
	busy    bool            // a turn is running
	err     error
	w, h    int
	pending *provider.ToolCall // non-nil while a permission prompt is shown
	answer  chan bool          // replies to the Prompter
}

// New builds a Model wired to an agent. The caller sets agent.OnEvent /
// OnToolResult to push StreamMsg/ToolResultMsg via program.Send, and calls
// WithAnswer to wire the permission-prompt reply channel.
func New(a *agent.Agent, system string, tools []provider.ToolSchema) Model {
	ta := textarea.New()
	ta.Placeholder = "send a message… (enter to submit, ctrl+c to quit)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.Focus()
	return Model{
		agent:  a,
		system: system,
		tools:  tools,
		input:  ta,
	}
}

// WithAnswer returns a copy of the model with the permission-prompt reply
// channel set (shared with the Prompter).
func (m Model) WithAnswer(ch chan bool) Model {
	m.answer = ch
	return m
}

// Init has nothing to start — the program.Send bridge drives all messages.
func (m Model) Init() tea.Cmd { return nil }

// Update handles streamed events, tool results, prompts, keys, and sizing.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.SetWidth(msg.Width)
		return m, nil

	case StreamMsg:
		m.applyEvent(provider.Event(msg))
		return m, nil

	case ToolResultMsg:
		m.applyToolResult(msg.Call, msg.Result)
		return m, nil

	case promptMsg:
		m.pending = &msg.call
		return m, nil

	case turnDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err
			m.log = append(m.log, "turn error: "+msg.err.Error())
		}
		m.history = msg.history
		m.log = append(m.log, "— turn done —")
		return m, nil

	case tea.KeyMsg:
		// A pending permission prompt captures y/n.
		if m.pending != nil {
			switch msg.String() {
			case "y", "Y", "enter":
				return m.answerPrompt(true), nil
			case "n", "N", "esc":
				return m.answerPrompt(false), nil
			}
			return m, nil // swallow other keys while prompting
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if !m.busy && strings.TrimSpace(m.input.Value()) != "" {
				m.submit()
				return m, m.runTurn()
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// answerPrompt records the user's decision, sends it to the Prompter, and
// clears the pending prompt.
func (m Model) answerPrompt(approved bool) Model {
	if m.answer != nil {
		select {
		case m.answer <- approved:
		default:
		}
	}
	m.pending = nil
	return m
}

// turnDoneMsg is sent when an agent.Run call returns.
type turnDoneMsg struct {
	history []provider.Message
	err     error
}

// runTurn drives one agent.Run in a goroutine; streamed Events and tool
// results arrive via the program.Send bridge set up by the caller.
func (m Model) runTurn() tea.Cmd {
	agent := m.agent
	system := m.system
	tools := m.tools
	history := m.history
	return func() tea.Msg {
		hist, err := agent.Run(context.Background(), system, tools, history)
		return turnDoneMsg{history: hist, err: err}
	}
}

// submit captures the current input as a user message, marks the model busy,
// and clears the input. The caller returns runTurn() as the resulting Cmd.
func (m *Model) submit() {
	text := m.input.Value()
	m.input.Reset()
	m.history = append(m.history, provider.Message{
		Role:   provider.RoleUser,
		Blocks: []provider.Block{{Kind: provider.BlockText, Text: text}},
	})
	m.busy = true
}

// applyEvent renders one streamed Event into the model (tested seam).
func (m *Model) applyEvent(ev provider.Event) {
	switch ev.Kind {
	case provider.EventTextDelta:
		m.cur.WriteString(ev.Text)
	case provider.EventThinkingDelta:
		m.flushText()
		m.log = append(m.log, "(thinking) "+ev.Text)
	case provider.EventToolCallStart:
		m.flushText()
		if ev.Call != nil {
			m.log = append(m.log, fmt.Sprintf("%s(%s)", ev.Call.Name, ev.Call.Input))
		}
	case provider.EventTurnEnd:
		m.flushText()
	case provider.EventError:
		m.err = ev.Err
		m.log = append(m.log, "error: "+ev.Err.Error())
	}
}

// applyToolResult renders a completed tool call's result (tested seam). Edit
// results are unified diffs.
func (m *Model) applyToolResult(call provider.ToolCall, res provider.Block) {
	m.flushText()
	if res.ToolResultError {
		m.log = append(m.log, fmt.Sprintf("✗ %s: %s", call.Name, res.ToolResultText))
		return
	}
	out := strings.TrimRight(res.ToolResultText, "\n")
	m.log = append(m.log, out)
}

func (m *Model) flushText() {
	if m.cur.Len() > 0 {
		m.log = append(m.log, m.cur.String())
		m.cur.Reset()
	}
}

// View renders the transcript, an optional permission prompt, and the input.
func (m Model) View() string {
	if m.h == 0 {
		return "starting…"
	}
	var b strings.Builder
	for _, line := range m.log {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if m.cur.Len() > 0 {
		b.WriteString(m.cur.String())
		b.WriteByte('\n')
	}
	if m.busy {
		b.WriteString(lipgloss.NewStyle().Faint(true).Render("…working…"))
		b.WriteByte('\n')
	}
	if m.pending != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(
			fmt.Sprintf("allow %s(%s)? [y/n]", m.pending.Name, m.pending.Input)))
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString(m.input.View())
	return b.String()
}
