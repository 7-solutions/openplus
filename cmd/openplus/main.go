// Command openplus is the entrypoint. It assembles a live session from the
// project's configuration (change 0002) and drives it: the Bubble Tea UI when
// stdin is a TTY, a single non-interactive turn otherwise.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/7solutions/openplus/internal/provider"
	"github.com/7solutions/openplus/internal/runtime"
	"github.com/7solutions/openplus/internal/tui"
)

const baseSystemPrompt = "You are OpenPlus, a pure-Go coding agent."

// Version is the build version, stamped at link time via
//
//	go build -ldflags '-X main.Version=v0.1.0'
//
// Defaults to "dev" so a developer build still reports a sensible string.
var Version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		root       string
		model      string
		skipPerms  bool
		fake       bool
		prompt     string
		showVer    bool
		configPath string
	)
	flag.StringVar(&root, "C", ".", "project root")
	flag.StringVar(&model, "model", "", "model id as <provider>/<model> (overrides opencode.json)")
	flag.BoolVar(&skipPerms, "dangerously-skip-permissions", false,
		"allow all tool calls without prompting (explicit deny rules still apply)")
	flag.BoolVar(&fake, "fake", false, "use the scripted fake provider (no API key needed)")
	flag.StringVar(&prompt, "p", "", "run a single turn with this prompt and exit")
	flag.BoolVar(&showVer, "version", false, "print the build version and exit")
	flag.StringVar(&configPath, "config", "", "path to opencode.json (default <root>/opencode.json)")
	flag.Parse()

	if showVer {
		fmt.Println("openplus " + Version)
		return nil
	}

	// A bare prompt can also be passed positionally: openplus "do the thing".
	if prompt == "" && flag.NArg() > 0 {
		prompt = strings.Join(flag.Args(), " ")
	}

	session, err := runtime.Assemble(root, runtime.Options{
		Model:            model,
		SkipPermissions:  skipPerms,
		Fake:             fake,
		BaseSystemPrompt: baseSystemPrompt,
		ConfigPath:       configPath,
	})
	if err != nil {
		return explain(err)
	}
	defer session.Close() //nolint:errcheck // best-effort teardown

	if skipPerms {
		fmt.Fprintln(os.Stderr, "warning: --dangerously-skip-permissions active (allow-all base)")
	}

	// A one-shot prompt, or no TTY, means non-interactive.
	if prompt != "" || !term.IsTerminal(int(os.Stdin.Fd())) {
		return runOnce(session, prompt)
	}
	return runTUI(session)
}

// runOnce drives a single turn and prints the assistant's reply.
func runOnce(session *runtime.Session, prompt string) error {
	if prompt == "" {
		prompt = "Say hello and describe what you can do."
	}

	session.OnEvent = func(ev provider.Event) {
		if ev.Kind == provider.EventTextDelta {
			fmt.Print(ev.Text)
		}
	}
	session.OnToolResult = func(call provider.ToolCall, res provider.Block) {
		if res.ToolResultError {
			fmt.Fprintf(os.Stderr, "\n✗ %s: %s\n", call.Name, res.ToolResultText)
			return
		}
		fmt.Fprintf(os.Stderr, "\n· %s\n", call.Name)
	}

	if _, err := session.Run(context.Background(), prompt, nil); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// runTUI launches the interactive front-end, bridging session callbacks into the
// program and wiring the permission prompter.
func runTUI(session *runtime.Session) error {
	answer := make(chan bool, 1)
	m := tui.New(session, session.SystemPrompt).WithAnswer(answer)
	p := tea.NewProgram(m, tea.WithAltScreen())

	session.OnEvent = func(ev provider.Event) { p.Send(tui.StreamMsg(ev)) }
	session.OnToolResult = func(call provider.ToolCall, res provider.Block) {
		p.Send(tui.ToolResultMsg{Call: call, Result: res})
	}
	session.SetPrompter(tui.NewPrompter(p.Send, answer))

	_, err := p.Run()
	return err
}

// explain turns an assembly failure into actionable guidance. A missing
// credential is the most common first-run problem, so it gets a concrete
// next step rather than just the error text.
func explain(err error) error {
	switch {
	case errors.Is(err, runtime.ErrMissingCredential):
		return fmt.Errorf("%w\n\nSet the provider's apiKey in opencode.json (it may reference "+
			"an environment variable, e.g. \"{env:ANTHROPIC_API_KEY}\"), point it at a local "+
			"endpoint with options.baseURL, or run with --fake to try OpenPlus offline", err)
	case errors.Is(err, runtime.ErrNoModel):
		return fmt.Errorf("%w\n\nSet \"model\": \"<provider>/<model>\" in opencode.json, "+
			"pass --model, or run with --fake", err)
	default:
		return err
	}
}
