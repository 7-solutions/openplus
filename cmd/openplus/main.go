// Command openplus is the scaffold entrypoint. It proves the loop end-to-end
// with the Fake provider and no network access — a smoke test you can run as
// `go run ./cmd/openplus`. Real provider wiring (T-012/T-013/T-014/T-002/
// T-003) and the TUI (T-030) land in later tasks — see
// openspec/changes/0001-foundation/tasks.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/7solutions/openplus/internal/agent"
	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/tool"
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

	// Gate: --dangerously-skip-permissions gives an allow-all base (no prompts);
	// otherwise the loop uses AllowAll for this smoke (real config-driven rules
	// land with config wiring).
	var gate policy.Gate
	switch {
	case skipPerms:
		skip, err := policy.NewSkip(nil, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		gate = skip
		fmt.Fprintln(os.Stderr, "warning: --dangerously-skip-permissions active (allow-all base)")
	default:
		gate = policy.AllowAll{}
	}
	_ = model // provider wiring lands in later tasks

	fake := &provider.Fake{
		Scripts: [][]provider.Event{
			{
				{Kind: provider.EventToolCallStart, Call: &provider.ToolCall{
					ID: "call_1", Name: "echo", Input: []byte(`{"text":"openplus scaffold is alive"}`),
				}},
				{Kind: provider.EventTurnEnd},
			},
			{
				{Kind: provider.EventTextDelta, Text: "done"},
				{Kind: provider.EventTurnEnd},
			},
		},
	}

	a := &agent.Agent{
		Provider: fake,
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
