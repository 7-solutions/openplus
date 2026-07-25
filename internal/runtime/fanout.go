package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/7solutions/openplus/internal/agent"
	"github.com/7solutions/openplus/internal/orchestrate"
	"github.com/7solutions/openplus/internal/policy"
	"github.com/7solutions/openplus/internal/ports"
	"github.com/7solutions/openplus/internal/tool"
)

// Bounds on fan-out. Each subagent is a full agent turn against the provider, so
// these are cost controls, not just resource ones.
const (
	// DefaultMaxSubagents caps how many subagents one fan-out may launch.
	DefaultMaxSubagents = 8
	// DefaultMaxSubagentParallel caps how many run at once.
	DefaultMaxSubagentParallel = 4
)

// Fanout runs each prompt as a parallel subagent, isolated in its own git
// worktree when the project is a repo, and returns the results in input order
// (ADR-0002 #4).
//
// A subagent's failure is carried in its own Result and never aborts its
// siblings — losing three good answers because a fourth failed would be worse
// than reporting the one failure.
func (s *Session) Fanout(ctx context.Context, prompts []string) ([]orchestrate.Result, error) {
	if len(prompts) == 0 {
		return nil, fmt.Errorf("runtime: fan-out needs at least one prompt")
	}
	maxTasks := s.MaxSubagents
	if maxTasks <= 0 {
		maxTasks = DefaultMaxSubagents
	}
	if len(prompts) > maxTasks {
		return nil, fmt.Errorf("runtime: %d prompts exceeds the subagent limit of %d; "+
			"each subagent is a full model turn, so raise Session.MaxSubagents deliberately",
			len(prompts), maxTasks)
	}

	tasks := make([]orchestrate.Task, 0, len(prompts))
	for i, p := range prompts {
		prompt := p
		tasks = append(tasks, orchestrate.Task{
			ID: fmt.Sprintf("sub-%d", i+1),
			Run: func(ctx context.Context, dir string) (string, error) {
				if s.OnSubagentDir != nil {
					s.OnSubagentDir(dir)
				}
				return s.runSubagent(ctx, prompt, dir)
			},
		})
	}

	runner := orchestrate.Runner{
		Isolator:    s.subagentIsolator(),
		MaxParallel: s.subagentParallel(),
	}
	return runner.RunAll(ctx, tasks)
}

func (s *Session) subagentParallel() int {
	if s.MaxSubagentParallel > 0 {
		return s.MaxSubagentParallel
	}
	return DefaultMaxSubagentParallel
}

// subagentIsolator returns a worktree isolator when the project is a git repo,
// and nil (run in place) otherwise. A project need not be version controlled to
// use OpenPlus, so the absence of git degrades rather than fails.
func (s *Session) subagentIsolator() orchestrate.Isolator {
	if fi, err := os.Stat(filepath.Join(s.Root, ".git")); err != nil || (!fi.IsDir() && fi.Size() == 0) {
		return nil
	}
	return &orchestrate.WorktreeIsolator{RepoRoot: s.Root}
}

// runSubagent executes one prompt as an independent agent turn.
//
// The subagent gets its own tool registry rooted at its isolated directory, so a
// worktree actually isolates: a glob or grep inside a subagent sees its own
// checkout rather than the primary one.
func (s *Session) runSubagent(ctx context.Context, prompt, dir string) (string, error) {
	root := dir
	if root == "" {
		root = s.Root
	}

	a := &agent.Agent{
		Provider: s.Provider,
		Tools:    subagentTools(root),
		Gate:     subagentGate(s.Rules),
		// No OnEvent/OnToolResult: several subagents streaming into one
		// transcript at once would interleave into noise. Their output is
		// reported when the fan-out merges.
	}

	history := []ports.Message{userMessage(prompt)}
	final, err := a.Run(ctx, s.SystemPrompt, s.ToolSchemas, history)
	if err != nil {
		return "", err
	}
	return lastAssistantText(final), nil
}

// subagentTools builds a tool registry rooted at a subagent's directory, so a
// glob or grep inside a worktree searches that checkout rather than the primary
// one.
func subagentTools(root string) *tool.Registry {
	return tool.NewRegistry(
		tool.Read{}, tool.Write{}, tool.Edit{}, tool.Bash{},
		tool.Glob{Root: root}, tool.Grep{Root: root},
	)
}

// subagentGate wraps the session's rules in a gate that resolves Ask without a
// prompt. A subagent runs unattended, so a Prompting gate would block forever on
// approval nobody is watching. Explicit denials still deny — the rules are
// honored, only the interactive step is removed.
//
// Ask degrades to Deny rather than Allow: an unattended agent should not gain
// permissions a watched one would have to request.
func subagentGate(rules *policy.Rules) policy.Gate {
	if rules == nil {
		return policy.AllowAll{}
	}
	return askDeniesGate{rules: rules}
}

// askDeniesGate resolves an Ask decision to Deny.
type askDeniesGate struct {
	rules *policy.Rules
}

func (g askDeniesGate) Permit(ctx context.Context, call ports.ToolCall) (policy.Decision, error) {
	d, err := g.rules.Permit(ctx, call)
	if err != nil {
		return policy.Deny, err
	}
	if d == policy.Ask {
		return policy.Deny, nil
	}
	return d, nil
}

// FanoutReport renders fan-out results for a user, in input order, marking
// failures rather than hiding them.
func FanoutReport(prompts []string, results []orchestrate.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d subagent(s):\n", len(results))
	for i, r := range results {
		prompt := ""
		if i < len(prompts) {
			prompt = prompts[i]
		}
		if r.Err != nil {
			fmt.Fprintf(&b, "\n✗ [%s] %s\n  failed: %v\n", r.ID, prompt, r.Err)
			continue
		}
		fmt.Fprintf(&b, "\n· [%s] %s\n%s\n", r.ID, prompt, strings.TrimRight(r.Output, "\n"))
	}
	return b.String()
}
