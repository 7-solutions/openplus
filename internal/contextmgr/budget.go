package contextmgr

import "github.com/7-solutions/openplus/internal/ports"

// Input is everything that could enter the context window for one turn, before
// budgeting. Sections are listed in ADR-0008 priority order.
type Input struct {
	// System is the system prompt. Never dropped — without it the agent loses
	// its identity and rules.
	System string
	// Task is the active task description (highest-priority working state).
	Task string
	// Progress is the active task's progress notes.
	Progress string
	// Checkpoint is the reconstructed checkpoint summary.
	Checkpoint string
	// Memory holds hybrid-retrieved memory chunks, most relevant first
	// (ADR-0003).
	Memory []string
	// Recent holds retained recent messages in chronological order.
	Recent []ports.Message
}

// Output is the budgeted context: the same sections, truncated to fit.
type Output struct {
	System     string
	Task       string
	Progress   string
	Checkpoint string
	Memory     []string
	Recent     []ports.Message

	// Used is the estimated token count of the retained content.
	Used int
	// Dropped counts the sections and items that did not fit.
	Dropped int
}

// Budgeter decides what enters the context window for a turn (ADR-0008).
// A zero Budget means no limit is configured and everything passes through.
type Budgeter struct {
	Tokenizer Tokenizer
	// Budget is the soft token ceiling for the assembled context.
	Budget int
}

// Fit assembles in into a budgeted Output, admitting sections in ADR-0008
// priority order: system → active task + progress → reconstructed checkpoint →
// retrieved memory → retained recent messages. The system prompt is always
// kept, even if it alone exceeds the budget: a context without it is useless,
// and ADR-0008 treats the budget as a soft ceiling.
func (b Budgeter) Fit(in Input) Output {
	tk := b.Tokenizer
	if tk == nil {
		tk = Heuristic{}
	}

	var out Output
	if b.Budget <= 0 {
		// No limit configured — pass everything through.
		out = Output{
			System:     in.System,
			Task:       in.Task,
			Progress:   in.Progress,
			Checkpoint: in.Checkpoint,
			Memory:     in.Memory,
			Recent:     in.Recent,
		}
		out.Used = tk.Count(in.System) + tk.Count(in.Task) + tk.Count(in.Progress) +
			tk.Count(in.Checkpoint) + countStrings(tk, in.Memory) + CountMessages(tk, in.Recent)
		return out
	}

	remaining := b.Budget

	// 1. System — unconditional.
	out.System = in.System
	sysCost := tk.Count(in.System)
	out.Used += sysCost
	remaining -= sysCost

	// 2. Active task, then its progress notes.
	if cost := tk.Count(in.Task); in.Task != "" {
		if cost <= remaining {
			out.Task = in.Task
			out.Used += cost
			remaining -= cost
		} else {
			out.Dropped++
		}
	}
	if cost := tk.Count(in.Progress); in.Progress != "" {
		// Progress only makes sense alongside its task.
		if out.Task != "" && cost <= remaining {
			out.Progress = in.Progress
			out.Used += cost
			remaining -= cost
		} else {
			out.Dropped++
		}
	}

	// 3. Reconstructed checkpoint.
	if cost := tk.Count(in.Checkpoint); in.Checkpoint != "" {
		if cost <= remaining {
			out.Checkpoint = in.Checkpoint
			out.Used += cost
			remaining -= cost
		} else {
			out.Dropped++
		}
	}

	// 4. Retrieved memory, most relevant first; stop at the first entry that
	// does not fit so retained entries keep their relevance order.
	for _, chunk := range in.Memory {
		cost := tk.Count(chunk)
		if cost > remaining {
			out.Dropped++
			continue
		}
		out.Memory = append(out.Memory, chunk)
		out.Used += cost
		remaining -= cost
	}

	// 5. Retained recent messages — newest first (recency wins), then restored
	// to chronological order so the transcript still reads correctly.
	kept := make([]ports.Message, 0, len(in.Recent))
	for i := len(in.Recent) - 1; i >= 0; i-- {
		cost := CountMessages(tk, in.Recent[i:i+1])
		if cost > remaining {
			out.Dropped++
			continue
		}
		kept = append(kept, in.Recent[i])
		out.Used += cost
		remaining -= cost
	}
	reverse(kept)
	out.Recent = kept

	return out
}

func countStrings(tk Tokenizer, ss []string) int {
	total := 0
	for _, s := range ss {
		total += tk.Count(s)
	}
	return total
}

func reverse(msgs []ports.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
