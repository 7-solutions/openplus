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

	"github.com/7-solutions/openplus/internal/ports"
	"github.com/7-solutions/openplus/internal/runtime"
	"github.com/7-solutions/openplus/internal/tui"
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
		os.Exit(exitCode(err))
	}
}

// exitCode maps an error from run() to a process exit code (T-427):
//
//	0 = clean run
//	2 = configuration problem (missing credential, no model)
//	1 = everything else (provider error, mid-turn failure, policy rejection)
//
// The two sentinel errors wrap any assembly-time configuration failure;
// mapping them to 2 lets first-run scripts detect "I need an apiKey" vs
// "the provider is down" by exit code alone. Stderr text is unchanged
// from prior versions, so existing scripts that grep stderr keep working.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, runtime.ErrMissingCredential) || errors.Is(err, runtime.ErrNoModel) {
		return 2
	}
	return 1
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
		goal       string
	)
	flag.StringVar(&root, "C", ".", "project root")
	flag.StringVar(&model, "model", "", "model id as <provider>/<model> (overrides opencode.json)")
	flag.BoolVar(&skipPerms, "dangerously-skip-permissions", false,
		"allow all tool calls without prompting (explicit deny rules still apply)")
	flag.BoolVar(&fake, "fake", false, "use the scripted fake provider (no API key needed)")
	flag.StringVar(&prompt, "p", "", "run a single turn with this prompt and exit")
	flag.BoolVar(&showVer, "version", false, "print the build version and exit")
	flag.StringVar(&configPath, "config", "", "path to opencode.json (default <root>/opencode.json)")
	// --goal: long form only — no short form because the lowercase
	// letters collide with existing flags (-C is project root).
	// OPENPLUS_GOAL env override applies below, env > flag > precedence.
	flag.StringVar(&goal, "goal", "", "stop the agent when this goal is met (consults Session.Judge if set)")
	flag.Parse()

	if showVer {
		fmt.Println("openplus " + Version)
		return nil
	}

	// Env overrides apply on top of Options (T-425 pattern). Precedence:
	// env > flag > file. OPENPLUS_GOAL wins over --goal when set.
	if v := os.Getenv("OPENPLUS_GOAL"); v != "" {
		goal = v
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
		Goal:             goal,
	})
	if err != nil {
		return explain(err)
	}
	defer session.Close() //nolint:errcheck // best-effort teardown

	if skipPerms {
		fmt.Fprintln(os.Stderr, "warning: --dangerously-skip-permissions active (allow-all base)")
	}

	// A declared MCP server that could not be used is skipped, not fatal — but
	// its tools are missing, so the user has to be told which one and why.
	for _, w := range session.MCPWarnings {
		fmt.Fprintf(os.Stderr, "! %s\n", w)
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

	session.OnEvent = func(ev ports.Event) {
		if ev.Kind == ports.EventTextDelta {
			fmt.Print(ev.Text)
		}
	}
	session.OnToolResult = func(call ports.ToolCall, res ports.Block) {
		if res.ToolResultError {
			fmt.Fprintf(os.Stderr, "\n✗ %s: %s\n", call.Name, res.ToolResultText)
			return
		}
		fmt.Fprintf(os.Stderr, "\n· %s\n", call.Name)
	}
	// Compaction and a lost checkpoint both change what the session can
	// remember, so neither may happen silently.
	session.OnCompact = func(before, after int) {
		fmt.Fprintf(os.Stderr, "\n· context compacted: %d → %d messages (earlier turns are in checkpoint.md)\n",
			before, after)
	}
	session.OnCheckpointError = func(err error) {
		fmt.Fprintf(os.Stderr, "\n! checkpoint failed, session is not durable: %v\n", err)
	}

	// A slash command runs locally, with no model round-trip (change 0009).
	out, handled, err := session.Dispatch(context.Background(), prompt)
	if err != nil {
		return err
	}
	if handled {
		fmt.Println(strings.TrimRight(out, "\n"))
		return nil
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

	// The configured palette applies from the first frame (change 0017). An
	// unknown name warns and falls back: appearance never blocks a session.
	palette, warn := tui.ResolveTheme(session.Config.TUI.Theme)
	if warn != "" {
		fmt.Fprintf(os.Stderr, "! %s\n", warn)
	}

	m := tui.New(session, session.SystemPrompt).WithAnswer(answer).WithTheme(palette)
	p := tea.NewProgram(m, tea.WithAltScreen())
	session.Theme = tui.NewThemeControl(p.Send, palette.Name)

	session.OnEvent = func(ev ports.Event) { p.Send(tui.StreamMsg(ev)) }
	session.OnToolResult = func(call ports.ToolCall, res ports.Block) {
		p.Send(tui.ToolResultMsg{Call: call, Result: res})
	}
	session.OnCompact = func(before, after int) {
		p.Send(tui.NoticeMsg{Text: fmt.Sprintf(
			"context compacted: %d → %d messages (earlier turns are in checkpoint.md)", before, after)})
	}
	session.OnCheckpointError = func(err error) {
		p.Send(tui.NoticeMsg{Text: fmt.Sprintf("checkpoint failed, session is not durable: %v", err)})
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
