package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/7-solutions/openplus/internal/coordinate"
	"github.com/7-solutions/openplus/internal/orchestrate"
)

// SubagentTask is one coordinated subagent: what to do, and which code symbols it
// will edit.
//
// Symbols are stated by the caller, never inferred from the prompt. A wrong guess
// would claim locks the subagent does not need — blocking other agents — or miss
// ones it does, which is the conflict coordination exists to prevent.
type SubagentTask struct {
	Prompt  string
	Symbols []string
}

// CoordinatedResult is one coordinated subagent's outcome. Blocked, failed, and
// merged are distinct states: a blocked subagent never ran, a failed one ran and
// errored, and only a merged one changed the codebase.
type CoordinatedResult struct {
	ID     string
	Prompt string
	Output string
	Err    error

	Merged        bool
	Blocked       bool
	BlockedBy     string
	BlockedSymbol string
}

// FanoutCoordinated claims each task's symbols, runs the granted subagents in
// their coordinated worktrees, and merges each on success (change 0012, via
// grit's claim→work→done model).
//
// Blocked tasks are reported and not run. Running a subagent whose claim was
// refused would produce work that must then be thrown away — the waste
// coordination is meant to eliminate.
func (s *Session) FanoutCoordinated(ctx context.Context, tasks []SubagentTask) ([]CoordinatedResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("runtime: coordinated fan-out needs at least one task")
	}
	for i, t := range tasks {
		if len(t.Symbols) == 0 {
			return nil, fmt.Errorf("runtime: task %d (%q) states no symbols; "+
				"coordinated mode requires the symbols each subagent will edit "+
				"(e.g. prompt#file.go::Func), since guessing them would lock the wrong code",
				i+1, t.Prompt)
		}
	}

	coord := s.Coordinator
	if coord == nil {
		coord = orchestrate.NoCoordinator{}
	}
	if !coord.Available() {
		return nil, fmt.Errorf("runtime: no symbol coordinator available; " +
			"the native coordinator needs a git repository, or install grit " +
			"(https://github.com/rtk-ai/grit); or run /subagents without --coordinated")
	}

	maxTasks := s.MaxSubagents
	if maxTasks <= 0 {
		maxTasks = DefaultMaxSubagents
	}
	if len(tasks) > maxTasks {
		return nil, fmt.Errorf("runtime: %d tasks exceeds the subagent limit of %d",
			len(tasks), maxTasks)
	}

	results := make([]CoordinatedResult, len(tasks))
	for i, task := range tasks {
		agent := fmt.Sprintf("sub-%d", i+1)
		results[i] = CoordinatedResult{ID: agent, Prompt: task.Prompt}

		claim, err := coord.Claim(ctx, agent, task.Prompt, task.Symbols)
		if err != nil {
			results[i].Err = fmt.Errorf("claim: %w", err)
			continue
		}
		if !claim.Granted {
			results[i].Blocked = true
			results[i].BlockedBy = claim.BlockedBy
			results[i].BlockedSymbol = claim.BlockedSymbol
			continue
		}

		out, runErr := s.runSubagent(ctx, task.Prompt, claim.Dir)
		if runErr != nil {
			results[i].Err = runErr
			// Release rather than Done: the work failed, so nothing should merge,
			// but the locks must not outlive the attempt.
			if relErr := coord.Release(ctx, agent); relErr != nil {
				results[i].Err = fmt.Errorf("%w (and releasing locks failed: %v)", runErr, relErr)
			}
			continue
		}

		results[i].Output = out
		if err := coord.Done(ctx, agent); err != nil {
			// The subagent succeeded but its work did not land. Report both facts:
			// the output is real, the merge is not.
			results[i].Err = fmt.Errorf("subagent succeeded but merge failed: %w", err)
			continue
		}
		results[i].Merged = true
	}

	return results, nil
}

// CoordinatedReport renders coordinated results. It leads with the backend and
// the fact that this mode commits, because unlike every other path in OpenPlus a
// coordinated fan-out writes history to the user's repository.
func CoordinatedReport(backend string, results []CoordinatedResult) string {
	var merged, blocked, failed int
	for _, r := range results {
		switch {
		case r.Blocked:
			blocked++
		case r.Err != nil:
			failed++
		case r.Merged:
			merged++
		}
	}

	var b strings.Builder
	if backend == "" {
		backend = "native"
	}
	fmt.Fprintf(&b, "coordinated fan-out (%s — this mode commits and merges into the repository): "+
		"%d merged, %d blocked, %d failed\n", backend, merged, blocked, failed)

	for _, r := range results {
		switch {
		case r.Blocked:
			fmt.Fprintf(&b, "\n⊘ [%s] %s\n  blocked: %s is held by %s\n",
				r.ID, r.Prompt, r.BlockedSymbol, holderOrUnknown(r.BlockedBy))
		case r.Err != nil:
			fmt.Fprintf(&b, "\n✗ [%s] %s\n  failed: %v\n", r.ID, r.Prompt, r.Err)
			if r.Output != "" {
				fmt.Fprintf(&b, "%s\n", strings.TrimRight(r.Output, "\n"))
			}
		default:
			fmt.Fprintf(&b, "\n· [%s] %s (merged)\n%s\n",
				r.ID, r.Prompt, strings.TrimRight(r.Output, "\n"))
		}
	}
	return b.String()
}

func holderOrUnknown(holder string) string {
	if holder == "" {
		return "another agent"
	}
	return holder
}

// coordinatorBackend reports which backend a Coordinator is, for the report.
func coordinatorBackend(c orchestrate.Coordinator) string {
	switch c.(type) {
	case *coordinate.NativeCoordinator:
		return "native"
	case *orchestrate.GritCoordinator:
		return "grit"
	case orchestrate.NoCoordinator:
		return "none"
	default:
		return "unknown"
	}
}

// parseCoordinatedTasks splits "prompt#sym,sym | prompt#sym" into tasks. The "#"
// separator keeps symbols attached to their prompt, and "|" separates tasks, so a
// prompt may contain spaces.
func parseCoordinatedTasks(args string) ([]SubagentTask, error) {
	var tasks []SubagentTask
	for seg := range strings.SplitSeq(args, "|") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		prompt, symList, found := strings.Cut(seg, "#")
		prompt = strings.TrimSpace(prompt)
		if !found || strings.TrimSpace(symList) == "" {
			return nil, fmt.Errorf("runtime: task %q states no symbols; "+
				"use prompt#file.go::Func (comma-separate several), since coordinated "+
				"mode must know what each subagent will edit", prompt)
		}
		var symbols []string
		for sym := range strings.SplitSeq(symList, ",") {
			if s := strings.TrimSpace(sym); s != "" {
				symbols = append(symbols, s)
			}
		}
		if len(symbols) == 0 {
			return nil, fmt.Errorf("runtime: task %q states no symbols", prompt)
		}
		tasks = append(tasks, SubagentTask{Prompt: prompt, Symbols: symbols})
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("runtime: --coordinated needs at least one prompt#symbol task")
	}
	return tasks, nil
}
