package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/7solutions/openplus/internal/agent"
	"github.com/7solutions/openplus/internal/orchestrate"
	"github.com/7solutions/openplus/internal/provider"
)

// PreviousPlaceholder is substituted in a phase prompt with the previous phase's
// output. It is the whole of the hand-off vocabulary: one explicit token, so a
// prompt without it is passed through verbatim and a reader can see at a glance
// which phases depend on their predecessor.
const PreviousPlaceholder = "{{previous}}"

// promptPhase runs a prompt as an agent turn. It is the first production
// implementation of orchestrate.Phase (ADR-0006): with it, a workflow is an
// ordered list of prompts with bounded retries and structured hand-off, which is
// exactly what the ADR describes.
type promptPhase struct {
	session *Session
	name    string
	prompt  string
}

func (p promptPhase) Name() string { return p.name }

// Run executes the phase's prompt as a single agent turn and returns the
// assistant's text, which becomes the hand-off value for the next phase.
//
// A phase deliberately does not carry conversation history: each is an
// independent step whose only inputs are its prompt and the previous phase's
// output. That is what makes a workflow deterministic rather than a conversation
// with extra structure.
func (p promptPhase) Run(ctx context.Context, st *orchestrate.State) (string, error) {
	prompt := p.prompt
	if st != nil && strings.Contains(prompt, PreviousPlaceholder) {
		prompt = strings.ReplaceAll(prompt, PreviousPlaceholder, st.Last)
	}

	a := &agent.Agent{
		Provider:     p.session.Provider,
		Tools:        p.session.Tools,
		Gate:         subagentGate(p.session.Rules),
		OnEvent:      p.session.OnEvent,
		OnToolResult: p.session.OnToolResult,
	}

	final, err := a.Run(ctx, p.session.SystemPrompt, p.session.ToolSchemas,
		[]provider.Message{userMessage(prompt)})
	if err != nil {
		return "", fmt.Errorf("phase %q: %w", p.name, err)
	}
	return lastAssistantText(final), nil
}

// registerWorkflows installs the built-in workflows.
//
// ADR-0006 names four (compose, deep-research, fact-check,
// research-experiment). Change 0011 ships one — enough to exercise the engine end
// to end. The other three are content rather than integration, and land when
// someone actually wants them rather than being invented here.
func (s *Session) registerWorkflows() {
	s.Workflows = map[string]orchestrate.Workflow{
		"review": {
			// A phase may fail transiently (a provider hiccup); one retry is
			// worth it, more would just multiply cost on a genuine failure.
			MaxRetries: 1,
			Phases: []orchestrate.Phase{
				promptPhase{
					session: s,
					name:    "survey",
					prompt: "Survey this project: what does it do, and what are its main " +
						"components? Be concise and specific to what you can actually see.",
				},
				promptPhase{
					session: s,
					name:    "critique",
					prompt: "Given this survey:\n\n" + PreviousPlaceholder +
						"\n\nIdentify the most significant risks or weaknesses. " +
						"Prefer a few real problems over a long list of minor ones.",
				},
			},
		},
	}
}

// workflowNames lists registered workflows, sorted for deterministic output.
func (s *Session) workflowNames() []string {
	names := make([]string, 0, len(s.Workflows))
	for name := range s.Workflows {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cmdWorkflows lists the registered workflows.
func (s *Session) cmdWorkflows(_ string) (string, error) {
	names := s.workflowNames()
	if len(names) == 0 {
		return "no workflows registered", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d workflow(s):\n", len(names))
	for _, name := range names {
		wf := s.Workflows[name]
		phases := make([]string, 0, len(wf.Phases))
		for _, p := range wf.Phases {
			phases = append(phases, p.Name())
		}
		fmt.Fprintf(&b, "  %-20s %s\n", name, strings.Join(phases, " → "))
	}
	return b.String(), nil
}

// cmdWorkflow runs a registered workflow and returns its report.
func (s *Session) cmdWorkflow(args string) (string, error) {
	name := strings.TrimSpace(args)
	if name == "" {
		return "", fmt.Errorf("runtime: /workflow needs a name; try /workflows to list them")
	}
	wf, ok := s.Workflows[name]
	if !ok {
		return "", fmt.Errorf("runtime: no workflow named %q; registered: %s",
			name, strings.Join(s.workflowNames(), ", "))
	}

	rep, err := wf.Run(context.Background(), &orchestrate.State{})
	if err != nil {
		// The report is still worth showing: it says how far the workflow got.
		return "", fmt.Errorf("%w\n\n%s", err, rep.String())
	}
	return rep.String(), nil
}
