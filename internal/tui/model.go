// Package tui is the Bubble Tea front-end (ADR-0001, T-030). It renders the
// agent loop's streamed Events (text, thinking, tool calls) and takes user
// input. The agent runs in a goroutine; its OnEvent hook pushes Events onto a
// channel that a tea.Cmd drains into the UI, so streaming stays live without
// blocking the render loop.
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

// Model is the Bubble Tea model.
type Model struct {
	agent   *agent.Agent
	system  string
	tools   []provider.ToolSchema
	history []provider.Message

	// evCh carries streamed Events from the agent goroutine into the UI.
	evCh chan provider.Event

	input textarea.Model
	log   []string        // flushed, rendered lines
	cur   strings.Builder // assistant text accumulated since last flush
	busy  bool            // a turn is running
	err   error
	w, h  int
}

// New builds a Model wired to an agent. The agent's OnEvent is set to push
// Events onto the model's channel; call SetProgram (or rely on the channel) so
// the program drains them.
func New(a *agent.Agent, system string, tools []provider.ToolSchema) Model {
	ta := textarea.New()
	ta.Placeholder = "send a message… (enter to submit, ctrl+c to quit)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.Focus()

	m := Model{
		agent:  a,
		system: system,
		tools:  tools,
		evCh:   make(chan provider.Event, 256),
		input:  ta,
	}
	if a != nil {
		a.OnEvent = func(ev provider.Event) {
			// Non-blocking: drop if the UI falls behind rather than stall the model.
			select {
			case m.evCh <- ev:
			default:
			}
		}
	}
	return m
}

// Init starts the event-draining command.
func (m Model) Init() tea.Cmd {
	return waitForEvent(m.evCh)
}

// streamMsg wraps one streamed provider.Event as a tea.Msg.
type streamMsg provider.Event

// turnDoneMsg is sent when an agent.Run call returns.
type turnDoneMsg struct {
	history []provider.Message
	err     error
}

// waitForEvent blocks on the event channel, returning the next Event as a msg.
// Returns nil (a no-op) if the channel is closed.
func waitForEvent(ch chan provider.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return streamMsg(ev)
	}
}

// runTurn drives one agent.Run in a goroutine; streamed Events arrive via evCh.
func (m Model) runTurn() tea.Cmd {
	return func() tea.Msg {
		hist, err := m.agent.Run(context.Background(), m.system, m.tools, m.history)
		return turnDoneMsg{history: hist, err: err}
	}
}

// Update handles messages: streamed events, key input, window sizing, and
// turn completion.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.SetWidth(msg.Width)
		return m, nil

	case streamMsg:
		m.applyEvent(provider.Event(msg))
		return m, waitForEvent(m.evCh)

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

// applyEvent renders one streamed Event into the model. Pure with respect to
// the provider — this is the tested rendering seam.
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

func (m *Model) flushText() {
	if m.cur.Len() > 0 {
		m.log = append(m.log, m.cur.String())
		m.cur.Reset()
	}
}

// View renders the transcript + input. Kept thin; the real layout work is
// applyEvent above.
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
	b.WriteString("\n")
	b.WriteString(m.input.View())
	return b.String()
}
