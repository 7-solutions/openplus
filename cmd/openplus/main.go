// Command openplus is the entrypoint. With a TTY it runs the Bubble Tea UI;
// without one (pipes/CI) it falls back to a non-interactive smoke that proves
// the loop. Real provider/config wiring (T-002/T-003 selection) lands in later
// tasks; for now it uses the Fake provider so it runs with no API key.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/7solutions/openplus/internal/agent"
	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/tool"
	"github.com/7solutions/openplus/internal/tui"
)

func main() {
	var (
		skipPerms bool
		model     string
	)
	flag.BoolVar(&skipPerms, "dangerously-skip-permissions", false,
		"allow all tool calls without prompting (explicit deny rules still apply)")
	flag.StringVar(&model, "model", "neutral/fake", "model id as <provider>/<model>")
	flag.Parse()

	gate, err := buildGate(skipPerms)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	_ = model // provider selection wiring lands in later tasks

	registry := tool.NewRegistry(
		tool.Echo{},
		tool.Read{}, tool.Write{}, tool.Edit{}, tool.Bash{},
		tool.Glob{Root: "."}, tool.Grep{Root: "."},
	)

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		runSmoke(gate, registry)
		return
	}

	// Interactive: launch the TUI.
	agentInst := &agent.Agent{
		Provider: demoProvider(),
		Tools:    registry,
		Gate:     gate,
	}
	m := tui.New(agentInst, "You are OpenPlus.", schemas(registry))
	answer := make(chan bool, 1)
	m = m.WithAnswer(answer)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Bridge agent callbacks into the program, and wire the permission prompter.
	agentInst.OnEvent = func(ev provider.Event) { p.Send(tui.StreamMsg(ev)) }
	agentInst.OnToolResult = func(call provider.ToolCall, res provider.Block) {
		p.Send(tui.ToolResultMsg{Call: call, Result: res})
	}
	if prompting, ok := gate.(*policy.Prompting); ok {
		prompting.Prompter = tui.NewPrompter(p.Send, answer)
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// buildGate selects the permission gate from flags. The default asks before
// mutating tools (bash/write/edit); --dangerously-skip-permissions drops to an
// allow-all base. The returned Prompting gate's Prompter is wired by the caller
// for the TUI.
func buildGate(skipPerms bool) (policy.Gate, error) {
	if skipPerms {
		skip, err := policy.NewSkip(nil, nil)
		if err != nil {
			return nil, err
		}
		fmt.Fprintln(os.Stderr, "warning: --dangerously-skip-permissions active (allow-all base)")
		return skip, nil
	}
	rules, err := policy.NewRules(policy.Allow,
		map[string]string{"bash": "ask", "write": "ask", "edit": "ask"}, nil)
	if err != nil {
		return nil, err
	}
	return &policy.Prompting{Rules: rules}, nil
}

// schemas converts the tool registry into provider-neutral tool schemas.
func schemas(r *tool.Registry) []provider.ToolSchema {
	all := r.All()
	out := make([]provider.ToolSchema, 0, len(all))
	for _, t := range all {
		out = append(out, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	return out
}

// demoProvider returns a Fake that demonstrates streaming + a tool call on the
// first turn, so the TUI is exercisable with no API key. Real wiring later.
func demoProvider() *provider.Fake {
	return &provider.Fake{
		Scripts: [][]provider.Event{
			{
				{Kind: provider.EventTextDelta, Text: "sure — "},
				{Kind: provider.EventToolCallStart, Call: &provider.ToolCall{
					ID: "call_1", Name: "echo", Input: []byte(`{"text":"openplus is alive"}`),
				}},
				{Kind: provider.EventTurnEnd},
			},
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventTurnEnd},
			},
			{{Kind: provider.EventTurnEnd}},
		},
	}
}

// runSmoke is the non-interactive smoke: runs the loop once and prints history.
func runSmoke(gate policy.Gate, registry *tool.Registry) {
	a := &agent.Agent{
		Provider: demoProvider(),
		Tools:    tool.NewRegistry(tool.Echo{}),
		Gate:     gate,
		OnEvent: func(ev provider.Event) {
			if ev.Kind == provider.EventTextDelta {
				fmt.Print(ev.Text)
			}
		},
	}
	history, err := a.Run(context.Background(), "scaffold smoke test", nil, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println()
	fmt.Printf("turns: %d\n", len(history))
	for i, m := range history {
		fmt.Printf("  [%d] %s: %+v\n", i, m.Role, m.Blocks)
	}
}
